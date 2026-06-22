package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"recruitment/logic-grpc-service/internal/ai"
	"recruitment/logic-grpc-service/internal/config"
	"recruitment/shared/rpc"
)

const (
	recommendationTaskStatusQueued      = "queued"
	recommendationTaskStatusRunning     = "running"
	recommendationTaskStatusSucceeded   = "succeeded"
	recommendationTaskStatusFailed      = "failed"
	recommendationAgentStatusRAGOnly    = "rag_only"
	recommendationEventStreamTTL        = 10 * time.Minute
	recommendationResultCacheTTL        = 30 * time.Minute
	recommendationResultCacheVersion    = "v1"
	recommendationTaskLeaseTTL          = 60 * time.Second
	recommendationTaskHeartbeatInterval = 10 * time.Second
	recommendationTaskMaxAttempts       = 3
)

type recommendationTaskPayload struct {
	TaskID     string                       `json:"task_id"`
	HRID       uint64                       `json:"hr_id"`
	Input      ai.ResumeRecommendationInput `json:"input"`
	RequestKey string                       `json:"request_key"`
	CacheKey   string                       `json:"cache_key"`
}

type recommendationCacheInput struct {
	Version        string   `json:"version"`
	HRID           uint64   `json:"hr_id"`
	JobID          uint64   `json:"job_id"`
	JobVersion     string   `json:"job_version,omitempty"`
	JobDescription string   `json:"job_description"`
	Queries        []string `json:"queries"`
	Limit          int      `json:"limit"`
	EvidenceLimit  int      `json:"evidence_limit"`
}

type recommendationTaskLease struct {
	acquired bool
	terminal bool
	status   string
}

type RecommendationQueue struct {
	rdb         *redis.Client
	ai          *ai.Client
	streamName  string
	groupName   string
	consumer    string
	workerCount int
	taskTTL     time.Duration
	taskTimeout time.Duration
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

func NewRecommendationQueue(cfg config.RecommendationQueueConfig, rdb *redis.Client, aiClient *ai.Client) *RecommendationQueue {
	workerCount := cfg.WorkerCount
	if workerCount <= 0 {
		workerCount = 1
	}
	taskTTL := time.Duration(cfg.TaskTTLSeconds) * time.Second
	if taskTTL <= 0 {
		taskTTL = 24 * time.Hour
	}
	taskTimeout := time.Duration(cfg.TaskTimeoutSeconds) * time.Second
	if taskTimeout <= 0 {
		taskTimeout = 5 * time.Minute
	}
	consumer := strings.TrimSpace(cfg.ConsumerName)
	if consumer == "" {
		consumer = "logic-1"
	}
	return &RecommendationQueue{
		rdb:         rdb,
		ai:          aiClient,
		streamName:  cfg.StreamName,
		groupName:   cfg.ConsumerGroup,
		consumer:    consumer,
		workerCount: workerCount,
		taskTTL:     taskTTL,
		taskTimeout: taskTimeout,
	}
}

func (q *RecommendationQueue) Start(ctx context.Context) error {
	if q == nil || q.rdb == nil {
		return errors.New("recommendation queue redis client is nil")
	}
	if strings.TrimSpace(q.streamName) == "" || strings.TrimSpace(q.groupName) == "" {
		return errors.New("recommendation queue stream and group are required")
	}
	if err := q.rdb.XGroupCreateMkStream(ctx, q.streamName, q.groupName, "0").Err(); err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return fmt.Errorf("create recommendation queue group: %w", err)
	}
	q.ctx, q.cancel = context.WithCancel(ctx)
	for i := 0; i < q.workerCount; i++ {
		consumer := fmt.Sprintf("%s-%d", q.consumer, i+1)
		q.wg.Add(1)
		go q.worker(consumer)
	}
	return nil
}

func (q *RecommendationQueue) Stop() {
	if q == nil || q.cancel == nil {
		return
	}
	q.cancel()
	q.wg.Wait()
}

func (q *RecommendationQueue) Enqueue(ctx context.Context, hrID uint64, input ai.ResumeRecommendationInput) (*rpc.ResumeRecommendationTaskResponse, error) {
	fingerprint := q.requestFingerprint(hrID, input)
	if fingerprint == "" {
		return nil, status.Error(codes.Internal, "build recommendation request fingerprint")
	}
	requestKey := q.requestKey(fingerprint)
	cacheKey := q.resultCacheKey(fingerprint)
	if existingTaskID, err := q.rdb.Get(ctx, requestKey).Result(); err == nil {
		if resp, ok, err := q.existingTaskResponse(ctx, existingTaskID, cacheKey); err != nil {
			return nil, err
		} else if ok {
			return resp, nil
		}
		_ = q.rdb.Del(ctx, requestKey).Err()
	} else if err != nil && !errors.Is(err, redis.Nil) {
		return nil, status.Errorf(codes.Unavailable, "load recommendation request key: %v", err)
	}

	taskID, err := newRecommendationTaskID()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := time.Now()
	payload := recommendationTaskPayload{TaskID: taskID, HRID: hrID, Input: input, RequestKey: requestKey, CacheKey: cacheKey}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	created, existingTaskID, err := q.createTaskIfAbsent(ctx, requestKey, q.taskKey(taskID), taskID, hrID, cacheKey, now, string(data))
	if err != nil {
		return nil, err
	}
	if !created {
		if resp, ok, err := q.existingTaskResponse(ctx, existingTaskID, cacheKey); err != nil {
			return nil, err
		} else if ok {
			return resp, nil
		}
		_ = q.rdb.Del(ctx, requestKey).Err()
		return nil, status.Error(codes.Unavailable, "recommendation task is being initialized")
	}
	return &rpc.ResumeRecommendationTaskResponse{
		TaskID:    taskID,
		Status:    recommendationTaskStatusQueued,
		CreatedAt: now.Format(time.RFC3339),
	}, nil
}

func (q *RecommendationQueue) Watch(ctx context.Context, actor *rpc.Actor, taskID string, send func(*rpc.ResumeRecommendationStreamChunk) error) error {
	if actor == nil || actor.UserID == 0 {
		return status.Error(codes.Unauthenticated, "missing actor")
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return status.Error(codes.InvalidArgument, "task_id is required")
	}
	state, err := q.taskState(ctx, taskID)
	if err != nil {
		return err
	}
	if state["hr_id"] != strconv.FormatUint(actor.UserID, 10) {
		return status.Error(codes.PermissionDenied, "cannot watch another hr's recommendation task")
	}

	lastID, replayedDone, err := q.replayEvents(ctx, taskID, send)
	if err != nil || replayedDone {
		return err
	}
	state, err = q.taskState(ctx, taskID)
	if err != nil {
		return err
	}
	if isTerminalRecommendationTaskStatus(state["status"]) {
		return q.sendTerminalState(state, taskID, send)
	}

	if lastID == "" {
		lastID = "0-0"
	}
	for {
		streams, err := q.rdb.XRead(ctx, &redis.XReadArgs{
			Streams: []string{q.eventsStreamKey(taskID), lastID},
			Count:   32,
			Block:   5 * time.Second,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return status.Errorf(codes.Unavailable, "read recommendation task event stream: %v", err)
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				lastID = msg.ID
				chunk, err := decodeRecommendationStreamChunk(msg)
				if err != nil {
					log.Printf("decode recommendation task event task_id=%s stream_id=%s: %v", taskID, msg.ID, err)
					continue
				}
				if err := send(chunk); err != nil {
					return err
				}
				if chunk.Done {
					return nil
				}
			}
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

func (q *RecommendationQueue) worker(consumer string) {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		default:
		}
		q.reclaimStaleMessages(consumer)
		streams, err := q.rdb.XReadGroup(q.ctx, &redis.XReadGroupArgs{
			Group:    q.groupName,
			Consumer: consumer,
			Streams:  []string{q.streamName, ">"},
			Count:    1,
			Block:    5 * time.Second,
		}).Result()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			if q.ctx.Err() != nil {
				return
			}
			log.Printf("read recommendation queue consumer=%s: %v", consumer, err)
			time.Sleep(time.Second)
			continue
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				q.processAndAckOnSuccess(consumer, msg)
			}
		}
	}
}

func (q *RecommendationQueue) reclaimStaleMessages(consumer string) {
	pending, err := q.rdb.XPendingExt(q.ctx, &redis.XPendingExtArgs{
		Stream: q.streamName,
		Group:  q.groupName,
		Idle:   q.taskTimeout,
		Start:  "-",
		End:    "+",
		Count:  int64(q.workerCount),
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		if q.ctx.Err() == nil {
			log.Printf("scan pending recommendation tasks consumer=%s: %v", consumer, err)
		}
		return
	}
	if len(pending) == 0 {
		return
	}
	ids := make([]string, 0, len(pending))
	for _, item := range pending {
		ids = append(ids, item.ID)
	}
	msgs, err := q.rdb.XClaim(q.ctx, &redis.XClaimArgs{
		Stream:   q.streamName,
		Group:    q.groupName,
		Consumer: consumer,
		MinIdle:  q.taskTimeout,
		Messages: ids,
	}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		if q.ctx.Err() == nil {
			log.Printf("claim pending recommendation tasks consumer=%s ids=%v: %v", consumer, ids, err)
		}
		return
	}
	for _, msg := range msgs {
		q.processAndAckOnSuccess(consumer, msg)
	}
}

func (q *RecommendationQueue) processAndAckOnSuccess(consumer string, msg redis.XMessage) {
	if err := q.processMessage(consumer, msg); err != nil {
		log.Printf("recommendation task failed consumer=%s message=%s: %v; leave message pending", consumer, msg.ID, err)
		return
	}
	if err := q.rdb.XAck(context.Background(), q.streamName, q.groupName, msg.ID).Err(); err != nil {
		log.Printf("ack recommendation task message=%s: %v", msg.ID, err)
	}
}

func (q *RecommendationQueue) processMessage(consumer string, msg redis.XMessage) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("panic while processing recommendation task message=%s: %v", msg.ID, recovered)
		}
	}()
	taskID := stringValue(msg.Values["task_id"])
	var payload recommendationTaskPayload
	if err := json.Unmarshal([]byte(stringValue(msg.Values["payload"])), &payload); err != nil {
		return fmt.Errorf("decode recommendation task message=%s task_id=%s: %w", msg.ID, taskID, err)
	}
	if taskID == "" {
		taskID = payload.TaskID
	}
	if payload.CacheKey == "" {
		fingerprint := q.requestFingerprint(payload.HRID, payload.Input)
		payload.CacheKey = q.resultCacheKey(fingerprint)
	}
	if payload.RequestKey == "" {
		fingerprint := q.requestFingerprint(payload.HRID, payload.Input)
		payload.RequestKey = q.requestKey(fingerprint)
	}
	ownerID := consumer + ":" + msg.ID
	lease, err := q.claimTaskLease(context.Background(), taskID, ownerID)
	if err != nil {
		return err
	}
	if lease.terminal {
		return nil
	}
	if !lease.acquired {
		log.Printf("skip duplicate recommendation task delivery task_id=%s owner=%s status=%s", taskID, ownerID, lease.status)
		return nil
	}
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	defer stopHeartbeat()
	go q.heartbeatTaskLease(heartbeatCtx, taskID, ownerID)

	ctx, cancel := context.WithTimeout(context.Background(), q.taskTimeout)
	defer cancel()
	if q.ai == nil {
		err := fmt.Errorf("简历推荐服务未初始化")
		q.failTask(ctx, taskID, ownerID, err.Error())
		return nil
	}
	out, err := q.runRecommendationWithRetries(ctx, taskID, ownerID, payload)
	if err != nil {
		if fallbackErr := q.completeTaskWithRAGFallback(context.Background(), taskID, ownerID, payload, err); fallbackErr != nil {
			q.failTask(context.Background(), taskID, ownerID, fmt.Sprintf("%v；RAG 兜底也失败：%v", err, fallbackErr))
		}
		return nil
	}
	if out.AgentStatus == recommendationAgentStatusRAGOnly {
		if err := q.rdb.Del(context.Background(), q.eventsStreamKey(taskID)).Err(); err != nil {
			return fmt.Errorf("clear partial recommendation stream task_id=%s: %w", taskID, err)
		}
	}
	active, err := q.taskOwnerActive(context.Background(), taskID, ownerID)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("stale recommendation task owner task_id=%s owner=%s", taskID, ownerID)
	}
	resp := resumeRecommendationResponse(out)
	if out.AgentStatus != recommendationAgentStatusRAGOnly {
		if err := q.storeCachedRecommendationResponse(context.Background(), payload.CacheKey, resp); err != nil {
			log.Printf("store recommendation cache task_id=%s cache_key=%s: %v", taskID, payload.CacheKey, err)
			_ = q.rdb.Del(context.Background(), payload.RequestKey).Err()
		} else {
			_ = q.rdb.Expire(context.Background(), payload.RequestKey, recommendationResultCacheTTL).Err()
		}
	} else {
		_ = q.rdb.Del(context.Background(), payload.RequestKey).Err()
	}
	if err := q.completeTask(context.Background(), taskID, ownerID, resp); err != nil {
		return err
	}
	return nil
}

func (q *RecommendationQueue) failTask(ctx context.Context, taskID string, ownerID string, reason string) {
	if err := q.failTaskIfOwner(ctx, taskID, ownerID, reason); err != nil {
		log.Printf("fail recommendation task task_id=%s owner=%s: %v", taskID, ownerID, err)
	}
}

func (q *RecommendationQueue) runRecommendationWithRetries(ctx context.Context, taskID string, ownerID string, payload recommendationTaskPayload) (ai.ResumeRecommendationOutput, error) {
	var lastErr error
	for attempt := 1; attempt <= recommendationTaskMaxAttempts; attempt++ {
		out, err := q.ai.RecommendResumesByJDWithProgress(ctx, payload.HRID, payload.Input, func(stage string, message string) error {
			if stage != "final_answer_streaming" {
				return nil
			}
			if err := q.emitChunkForOwner(context.Background(), taskID, ownerID, recommendationTaskStatusRunning, stage, message, false, nil); err != nil {
				return fmt.Errorf("append recommendation final answer task_id=%s stage=%s: %w", taskID, stage, err)
			}
			return nil
		})
		if err == nil {
			return out, nil
		}
		lastErr = err
		if attempt >= recommendationTaskMaxAttempts {
			break
		}
		log.Printf("retry recommendation task in worker task_id=%s attempt=%d/%d err=%v", taskID, attempt, recommendationTaskMaxAttempts, err)
		if err := q.clearTaskEventsIfOwner(context.Background(), taskID, ownerID); err != nil {
			return ai.ResumeRecommendationOutput{}, err
		}
	}
	return ai.ResumeRecommendationOutput{}, lastErr
}

func (q *RecommendationQueue) completeTaskWithRAGFallback(ctx context.Context, taskID string, ownerID string, payload recommendationTaskPayload, cause error) error {
	active, err := q.taskOwnerActive(ctx, taskID, ownerID)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("stale recommendation task owner task_id=%s owner=%s", taskID, ownerID)
	}
	reason := fmt.Sprintf("推荐任务重试 %d 次后仍失败：%v；已返回 RAG 检索候选人兜底结果", recommendationTaskMaxAttempts, cause)
	out, err := q.ai.RecommendResumesRAGOnly(ctx, payload.HRID, payload.Input, reason)
	if err != nil {
		return err
	}
	if err := q.rdb.Del(ctx, q.eventsStreamKey(taskID)).Err(); err != nil {
		return fmt.Errorf("clear partial recommendation stream task_id=%s: %w", taskID, err)
	}
	resp := resumeRecommendationResponse(out)
	_ = q.rdb.Del(ctx, payload.RequestKey).Err()
	if err := q.completeTask(ctx, taskID, ownerID, resp); err != nil {
		return err
	}
	return nil
}

func (q *RecommendationQueue) claimTaskLease(ctx context.Context, taskID string, ownerID string) (recommendationTaskLease, error) {
	now := time.Now()
	leaseUntil := now.Add(recommendationTaskLeaseTTL)
	const script = `
local status = redis.call("HGET", KEYS[1], "status")
if not status then
	return {"missing", "0"}
end
if status == ARGV[5] or status == ARGV[6] then
	return {status, "1"}
end
local lease_until = redis.call("HGET", KEYS[1], "lease_until")
if lease_until and lease_until ~= "" and tonumber(lease_until) > tonumber(ARGV[1]) then
	return {status, "0"}
end
redis.call("HSET", KEYS[1],
	"status", ARGV[4],
	"owner_id", ARGV[2],
	"lease_until", ARGV[7],
	"heartbeat_at", ARGV[3],
	"updated_at", ARGV[3])
redis.call("HDEL", KEYS[1], "error")
redis.call("EXPIRE", KEYS[1], ARGV[8])
redis.call("DEL", KEYS[2])
return {ARGV[4], "2"}
`
	result, err := q.rdb.Eval(ctx, script, []string{q.taskKey(taskID), q.eventsStreamKey(taskID)},
		strconv.FormatInt(now.UnixNano(), 10),
		ownerID,
		now.Format(time.RFC3339),
		recommendationTaskStatusRunning,
		recommendationTaskStatusSucceeded,
		recommendationTaskStatusFailed,
		strconv.FormatInt(leaseUntil.UnixNano(), 10),
		strconv.Itoa(int(q.taskTTL.Seconds())),
	).Result()
	if err != nil {
		return recommendationTaskLease{}, fmt.Errorf("claim recommendation task lease task_id=%s owner=%s: %w", taskID, ownerID, err)
	}
	values, ok := result.([]any)
	if !ok || len(values) != 2 {
		return recommendationTaskLease{}, fmt.Errorf("claim recommendation task lease returned invalid result task_id=%s", taskID)
	}
	statusValue := stringValue(values[0])
	mode := stringValue(values[1])
	if statusValue == "missing" {
		return recommendationTaskLease{}, fmt.Errorf("recommendation task not found task_id=%s", taskID)
	}
	return recommendationTaskLease{
		acquired: mode == "2",
		terminal: mode == "1",
		status:   statusValue,
	}, nil
}

func (q *RecommendationQueue) heartbeatTaskLease(ctx context.Context, taskID string, ownerID string) {
	ticker := time.NewTicker(recommendationTaskHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := q.refreshTaskLease(context.Background(), taskID, ownerID); err != nil {
				log.Printf("refresh recommendation task lease task_id=%s owner=%s: %v", taskID, ownerID, err)
			}
		}
	}
}

func (q *RecommendationQueue) refreshTaskLease(ctx context.Context, taskID string, ownerID string) error {
	now := time.Now()
	leaseUntil := now.Add(recommendationTaskLeaseTTL)
	const script = `
if redis.call("HGET", KEYS[1], "owner_id") ~= ARGV[1] then
	return 0
end
local status = redis.call("HGET", KEYS[1], "status")
if status ~= ARGV[4] then
	return 0
end
redis.call("HSET", KEYS[1],
	"lease_until", ARGV[2],
	"heartbeat_at", ARGV[3],
	"updated_at", ARGV[3])
return 1
`
	ok, err := q.rdb.Eval(ctx, script, []string{q.taskKey(taskID)},
		ownerID,
		strconv.FormatInt(leaseUntil.UnixNano(), 10),
		now.Format(time.RFC3339),
		recommendationTaskStatusRunning,
	).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return fmt.Errorf("task lease is no longer owned")
	}
	return nil
}

func (q *RecommendationQueue) taskOwnerActive(ctx context.Context, taskID string, ownerID string) (bool, error) {
	owner, err := q.rdb.HGet(ctx, q.taskKey(taskID), "owner_id").Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load recommendation task owner task_id=%s: %w", taskID, err)
	}
	return owner == ownerID, nil
}

func (q *RecommendationQueue) clearTaskEventsIfOwner(ctx context.Context, taskID string, ownerID string) error {
	const script = `
if redis.call("HGET", KEYS[1], "owner_id") ~= ARGV[1] then
	return 0
end
redis.call("DEL", KEYS[2])
return 1
`
	ok, err := q.rdb.Eval(ctx, script, []string{q.taskKey(taskID), q.eventsStreamKey(taskID)}, ownerID).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return fmt.Errorf("task lease is no longer owned")
	}
	return nil
}

func (q *RecommendationQueue) failTaskIfOwner(ctx context.Context, taskID string, ownerID string, reason string) error {
	chunk := &rpc.ResumeRecommendationStreamChunk{
		TaskID:  taskID,
		Status:  recommendationTaskStatusFailed,
		Stage:   "failed",
		Message: "简历推荐失败：" + reason,
		Done:    true,
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	const script = `
if redis.call("HGET", KEYS[1], "owner_id") ~= ARGV[1] then
	return 0
end
redis.call("HSET", KEYS[1],
	"status", ARGV[2],
	"error", ARGV[3],
	"updated_at", ARGV[4])
redis.call("HDEL", KEYS[1], "owner_id", "lease_until", "heartbeat_at")
redis.call("EXPIRE", KEYS[1], ARGV[7])
redis.call("XADD", KEYS[2], "*", "payload", ARGV[5])
redis.call("EXPIRE", KEYS[2], ARGV[6])
return 1
`
	ok, err := q.rdb.Eval(ctx, script, []string{q.taskKey(taskID), q.eventsStreamKey(taskID)},
		ownerID,
		recommendationTaskStatusFailed,
		reason,
		now,
		string(data),
		strconv.Itoa(int(recommendationEventStreamTTL.Seconds())),
		strconv.Itoa(int(q.taskTTL.Seconds())),
	).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return fmt.Errorf("task lease is no longer owned")
	}
	return nil
}

func (q *RecommendationQueue) completeTask(ctx context.Context, taskID string, ownerID string, resp *rpc.ResumeRecommendationResponse) error {
	chunk := &rpc.ResumeRecommendationStreamChunk{
		TaskID:   taskID,
		Status:   recommendationTaskStatusSucceeded,
		Stage:    "done",
		Message:  "简历推荐完成",
		Done:     true,
		Response: resp,
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	now := time.Now().Format(time.RFC3339)
	const script = `
if redis.call("HGET", KEYS[1], "owner_id") ~= ARGV[1] then
	return 0
end
redis.call("HSET", KEYS[1],
	"status", ARGV[2],
	"updated_at", ARGV[3])
redis.call("HDEL", KEYS[1], "owner_id", "lease_until", "heartbeat_at", "error")
redis.call("EXPIRE", KEYS[1], ARGV[4])
redis.call("XADD", KEYS[2], "*", "payload", ARGV[5])
redis.call("EXPIRE", KEYS[2], ARGV[6])
return 1
`
	ok, err := q.rdb.Eval(ctx, script, []string{q.taskKey(taskID), q.eventsStreamKey(taskID)},
		ownerID,
		recommendationTaskStatusSucceeded,
		now,
		strconv.Itoa(int(q.taskTTL.Seconds())),
		string(data),
		strconv.Itoa(int(recommendationEventStreamTTL.Seconds())),
	).Int()
	if err != nil {
		return fmt.Errorf("complete recommendation task task_id=%s: %w", taskID, err)
	}
	if ok != 1 {
		return fmt.Errorf("stale recommendation task owner task_id=%s owner=%s", taskID, ownerID)
	}
	return nil
}

func (q *RecommendationQueue) createTaskIfAbsent(ctx context.Context, requestKey string, taskKey string, taskID string, hrID uint64, cacheKey string, now time.Time, payload string) (bool, string, error) {
	const script = `
local existing = redis.call("GET", KEYS[1])
if existing then
	return {0, existing}
end
redis.call("SET", KEYS[1], ARGV[1], "EX", ARGV[2])
redis.call("HSET", KEYS[2],
	"task_id", ARGV[1],
	"hr_id", ARGV[3],
	"status", ARGV[4],
	"request_key", KEYS[1],
	"cache_key", ARGV[5],
	"created_at", ARGV[6],
	"updated_at", ARGV[6])
redis.call("EXPIRE", KEYS[2], ARGV[7])
redis.call("XADD", KEYS[3], "*", "task_id", ARGV[1], "payload", ARGV[8])
return {1, ARGV[1]}
`
	result, err := q.rdb.Eval(ctx, script, []string{requestKey, taskKey, q.streamName},
		taskID,
		strconv.Itoa(int(recommendationResultCacheTTL.Seconds())),
		strconv.FormatUint(hrID, 10),
		recommendationTaskStatusQueued,
		cacheKey,
		now.Format(time.RFC3339),
		strconv.Itoa(int(q.taskTTL.Seconds())),
		payload,
	).Result()
	if err != nil {
		return false, "", status.Errorf(codes.Unavailable, "create recommendation task: %v", err)
	}
	values, ok := result.([]any)
	if !ok || len(values) != 2 {
		return false, "", status.Error(codes.Unavailable, "create recommendation task returned invalid result")
	}
	return stringValue(values[0]) == "1", stringValue(values[1]), nil
}

func (q *RecommendationQueue) existingTaskResponse(ctx context.Context, taskID string, cacheKey string) (*rpc.ResumeRecommendationTaskResponse, bool, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, false, nil
	}
	state, err := q.rdb.HGetAll(ctx, q.taskKey(taskID)).Result()
	if err != nil {
		return nil, false, status.Errorf(codes.Unavailable, "load recommendation task: %v", err)
	}
	if len(state) == 0 {
		return nil, false, nil
	}
	resp := &rpc.ResumeRecommendationTaskResponse{
		TaskID:    taskID,
		Status:    state["status"],
		CreatedAt: state["created_at"],
	}
	if resp.Status == recommendationTaskStatusFailed {
		return nil, false, nil
	}
	if resp.Status == recommendationTaskStatusSucceeded {
		cached, ok, err := q.cachedRecommendationResponse(ctx, cacheKey)
		if err != nil {
			return nil, false, status.Errorf(codes.Unavailable, "load recommendation cache: %v", err)
		} else if ok {
			resp.Cached = true
			resp.Response = cached
		} else {
			return nil, false, nil
		}
	}
	return resp, true, nil
}

func (q *RecommendationQueue) cachedRecommendationResponse(ctx context.Context, cacheKey string) (*rpc.ResumeRecommendationResponse, bool, error) {
	if strings.TrimSpace(cacheKey) == "" {
		return nil, false, nil
	}
	payload, err := q.rdb.Get(ctx, cacheKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var resp rpc.ResumeRecommendationResponse
	if err := json.Unmarshal([]byte(payload), &resp); err != nil {
		return nil, false, err
	}
	return &resp, true, nil
}

func (q *RecommendationQueue) storeCachedRecommendationResponse(ctx context.Context, cacheKey string, resp *rpc.ResumeRecommendationResponse) error {
	if strings.TrimSpace(cacheKey) == "" || resp == nil {
		return nil
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	return q.rdb.Set(ctx, cacheKey, string(data), recommendationResultCacheTTL).Err()
}

func (q *RecommendationQueue) emitChunkForOwner(ctx context.Context, taskID string, ownerID string, taskStatus string, stage string, message string, done bool, resp *rpc.ResumeRecommendationResponse) error {
	chunk := &rpc.ResumeRecommendationStreamChunk{
		TaskID:   taskID,
		Status:   taskStatus,
		Stage:    stage,
		Message:  message,
		Done:     done,
		Response: resp,
	}
	data, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	const script = `
if redis.call("HGET", KEYS[1], "owner_id") ~= ARGV[1] then
	return 0
end
redis.call("XADD", KEYS[2], "*", "payload", ARGV[2])
redis.call("EXPIRE", KEYS[2], ARGV[3])
return 1
`
	ok, err := q.rdb.Eval(ctx, script, []string{q.taskKey(taskID), q.eventsStreamKey(taskID)},
		ownerID,
		string(data),
		strconv.Itoa(int(recommendationEventStreamTTL.Seconds())),
	).Int()
	if err != nil {
		return err
	}
	if ok != 1 {
		return fmt.Errorf("task lease is no longer owned")
	}
	return nil
}

func (q *RecommendationQueue) replayEvents(ctx context.Context, taskID string, send func(*rpc.ResumeRecommendationStreamChunk) error) (string, bool, error) {
	items, err := q.rdb.XRange(ctx, q.eventsStreamKey(taskID), "-", "+").Result()
	if err != nil {
		return "", false, status.Errorf(codes.Unavailable, "load recommendation task event stream: %v", err)
	}
	lastID := ""
	for _, item := range items {
		lastID = item.ID
		chunk, err := decodeRecommendationStreamChunk(item)
		if err != nil {
			log.Printf("decode recommendation task history task_id=%s stream_id=%s: %v", taskID, item.ID, err)
			continue
		}
		if err := send(chunk); err != nil {
			return lastID, false, err
		}
		if chunk.Done {
			return lastID, true, nil
		}
	}
	return lastID, false, nil
}

func (q *RecommendationQueue) taskState(ctx context.Context, taskID string) (map[string]string, error) {
	state, err := q.rdb.HGetAll(ctx, q.taskKey(taskID)).Result()
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "load recommendation task: %v", err)
	}
	if len(state) == 0 {
		return nil, status.Error(codes.NotFound, "recommendation task not found")
	}
	return state, nil
}

func (q *RecommendationQueue) sendTerminalState(state map[string]string, taskID string, send func(*rpc.ResumeRecommendationStreamChunk) error) error {
	taskStatus := state["status"]
	if taskStatus == recommendationTaskStatusFailed {
		message := state["error"]
		if message == "" {
			message = "简历推荐失败"
		}
		return send(&rpc.ResumeRecommendationStreamChunk{
			TaskID:  taskID,
			Status:  taskStatus,
			Stage:   "failed",
			Message: message,
			Done:    true,
		})
	}
	return nil
}

func (q *RecommendationQueue) taskKey(taskID string) string {
	return "resume_recommendation:task:" + taskID
}

func (q *RecommendationQueue) eventsStreamKey(taskID string) string {
	return q.taskKey(taskID) + ":events"
}

func (q *RecommendationQueue) requestKey(fingerprint string) string {
	if fingerprint == "" {
		return ""
	}
	return "resume_recommendation:request:" + fingerprint
}

func (q *RecommendationQueue) resultCacheKey(fingerprint string) string {
	if fingerprint == "" {
		return ""
	}
	return "resume_recommendation:cache:" + fingerprint
}

func (q *RecommendationQueue) requestFingerprint(hrID uint64, input ai.ResumeRecommendationInput) string {
	normalized := recommendationCacheInput{
		Version:        recommendationResultCacheVersion,
		HRID:           hrID,
		JobID:          input.JobID,
		JobVersion:     strings.TrimSpace(input.JobVersion),
		JobDescription: strings.TrimSpace(input.JobDescription),
		Queries:        normalizedRecommendationCacheQueries(input.Queries),
		Limit:          normalizedRecommendationCacheLimit(input.Limit),
		EvidenceLimit:  normalizedRecommendationCacheEvidenceLimit(input.EvidenceLimit),
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func normalizedRecommendationCacheQueries(queries []string) []string {
	normalized := make([]string, 0, len(queries))
	seen := make(map[string]struct{}, len(queries))
	for _, query := range queries {
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		if _, ok := seen[query]; ok {
			continue
		}
		seen[query] = struct{}{}
		normalized = append(normalized, query)
	}
	sort.Strings(normalized)
	return normalized
}

func normalizedRecommendationCacheLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 10 {
		return 10
	}
	return limit
}

func normalizedRecommendationCacheEvidenceLimit(limit int) int {
	if limit <= 0 {
		return 3
	}
	if limit > 5 {
		return 5
	}
	return limit
}

func isTerminalRecommendationTaskStatus(taskStatus string) bool {
	return taskStatus == recommendationTaskStatusSucceeded || taskStatus == recommendationTaskStatusFailed
}

func newRecommendationTaskID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("generate recommendation task id: %w", err)
	}
	return hex.EncodeToString(buf[:]), nil
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(value)
	}
}

func decodeRecommendationStreamChunk(msg redis.XMessage) (*rpc.ResumeRecommendationStreamChunk, error) {
	payload := stringValue(msg.Values["payload"])
	if payload == "" {
		return nil, fmt.Errorf("empty payload")
	}
	var chunk rpc.ResumeRecommendationStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return nil, err
	}
	return &chunk, nil
}
