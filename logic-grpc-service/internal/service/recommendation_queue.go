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
	recommendationTaskStatusQueued     = "queued"
	recommendationTaskStatusProcessing = "processing"
	recommendationTaskStatusDone       = "done"
	recommendationTaskStatusFailed     = "failed"
	recommendationAgentStatusRAGOnly   = "rag_only"
	recommendationEventStreamTTL       = 10 * time.Minute
	recommendationResultCacheTTL       = 30 * time.Minute
	recommendationResultCacheVersion   = "v1"
)

type recommendationTaskPayload struct {
	TaskID   string                       `json:"task_id"`
	HRID     uint64                       `json:"hr_id"`
	Input    ai.ResumeRecommendationInput `json:"input"`
	CacheKey string                       `json:"cache_key"`
}

type recommendationCacheInput struct {
	Version        string   `json:"version"`
	HRID           uint64   `json:"hr_id"`
	JobID          uint64   `json:"job_id"`
	JobDescription string   `json:"job_description"`
	Queries        []string `json:"queries"`
	Limit          int      `json:"limit"`
	EvidenceLimit  int      `json:"evidence_limit"`
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
	taskID, err := newRecommendationTaskID()
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	now := time.Now()
	cacheKey := q.resultCacheKey(hrID, input)
	if cached, ok, err := q.cachedRecommendationResponse(ctx, cacheKey); err != nil {
		log.Printf("load recommendation cache task_id=%s cache_key=%s: %v", taskID, cacheKey, err)
	} else if ok {
		if err := q.createCachedTask(ctx, taskID, hrID, cacheKey, now, cached); err != nil {
			return nil, err
		}
		return &rpc.ResumeRecommendationTaskResponse{
			TaskID:    taskID,
			Status:    recommendationTaskStatusDone,
			CreatedAt: now.Format(time.RFC3339),
			Cached:    true,
			Response:  cached,
		}, nil
	}
	payload := recommendationTaskPayload{TaskID: taskID, HRID: hrID, Input: input, CacheKey: cacheKey}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	taskKey := q.taskKey(taskID)
	pipe := q.rdb.Pipeline()
	pipe.HSet(ctx, taskKey, map[string]any{
		"task_id":    taskID,
		"hr_id":      strconv.FormatUint(hrID, 10),
		"status":     recommendationTaskStatusQueued,
		"cache_key":  cacheKey,
		"cache_hit":  "false",
		"created_at": now.Format(time.RFC3339),
		"updated_at": now.Format(time.RFC3339),
	})
	pipe.Expire(ctx, taskKey, q.taskTTL)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "store recommendation task: %v", err)
	}
	if err := q.rdb.XAdd(ctx, &redis.XAddArgs{
		Stream: q.streamName,
		Values: map[string]any{
			"task_id": taskID,
			"payload": string(data),
		},
	}).Err(); err != nil {
		return nil, status.Errorf(codes.Unavailable, "enqueue recommendation task: %v", err)
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
	if err := q.processMessage(msg); err != nil {
		log.Printf("recommendation task failed consumer=%s message=%s: %v; leave message pending", consumer, msg.ID, err)
		return
	}
	if err := q.rdb.XAck(context.Background(), q.streamName, q.groupName, msg.ID).Err(); err != nil {
		log.Printf("ack recommendation task message=%s: %v", msg.ID, err)
	}
}

func (q *RecommendationQueue) processMessage(msg redis.XMessage) (err error) {
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
		payload.CacheKey = q.resultCacheKey(payload.HRID, payload.Input)
	}
	done, err := q.taskDone(context.Background(), taskID)
	if err != nil {
		return err
	}
	if done {
		return nil
	}
	attempt, err := q.beginTaskAttempt(context.Background(), taskID, msg.ID)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), q.taskTimeout)
	defer cancel()
	if q.ai == nil {
		err := fmt.Errorf("简历推荐服务未初始化")
		q.failTask(ctx, taskID, msg.ID, attempt, err.Error())
		return err
	}
	out, err := q.ai.RecommendResumesByJDWithProgress(ctx, payload.HRID, payload.Input, func(stage string, message string) error {
		if stage != "final_answer_streaming" {
			return nil
		}
		active, err := q.taskAttemptActive(context.Background(), taskID, msg.ID, attempt)
		if err != nil {
			return err
		}
		if !active {
			return fmt.Errorf("stale recommendation task attempt task_id=%s message=%s attempt=%d", taskID, msg.ID, attempt)
		}
		if err := q.emitChunk(context.Background(), taskID, recommendationTaskStatusProcessing, stage, message, false, nil); err != nil {
			return fmt.Errorf("append recommendation final answer task_id=%s stage=%s: %w", taskID, stage, err)
		}
		return nil
	})
	if err != nil {
		q.failTask(context.Background(), taskID, msg.ID, attempt, err.Error())
		return err
	}
	if out.AgentStatus == recommendationAgentStatusRAGOnly {
		if err := q.rdb.Del(context.Background(), q.eventsStreamKey(taskID)).Err(); err != nil {
			return fmt.Errorf("clear partial recommendation stream task_id=%s: %w", taskID, err)
		}
	}
	active, err := q.taskAttemptActive(context.Background(), taskID, msg.ID, attempt)
	if err != nil {
		return err
	}
	if !active {
		return fmt.Errorf("stale recommendation task attempt task_id=%s message=%s attempt=%d", taskID, msg.ID, attempt)
	}
	resp := resumeRecommendationResponse(out)
	if out.AgentStatus != recommendationAgentStatusRAGOnly {
		if err := q.storeCachedRecommendationResponse(context.Background(), payload.CacheKey, resp); err != nil {
			log.Printf("store recommendation cache task_id=%s cache_key=%s: %v", taskID, payload.CacheKey, err)
		}
	}
	if err := q.emitChunk(context.Background(), taskID, recommendationTaskStatusDone, "done", "简历推荐完成", true, resp); err != nil {
		return fmt.Errorf("append recommendation result task_id=%s: %w", taskID, err)
	}
	if err := q.completeTask(context.Background(), taskID); err != nil {
		return err
	}
	return nil
}

func (q *RecommendationQueue) failTask(ctx context.Context, taskID string, messageID string, attempt int64, reason string) {
	active, err := q.taskAttemptActive(ctx, taskID, messageID, attempt)
	if err != nil || !active {
		return
	}
	q.markTaskStatus(ctx, taskID, recommendationTaskStatusFailed, reason)
	_ = q.emitChunk(ctx, taskID, recommendationTaskStatusFailed, "failed", "简历推荐失败："+reason, true, nil)
}

func (q *RecommendationQueue) markTaskStatus(ctx context.Context, taskID string, taskStatus string, reason string) {
	values := map[string]any{
		"status":     taskStatus,
		"updated_at": time.Now().Format(time.RFC3339),
	}
	if reason != "" {
		values["error"] = reason
	}
	if err := q.rdb.HSet(ctx, q.taskKey(taskID), values).Err(); err != nil {
		log.Printf("mark recommendation task task_id=%s status=%s: %v", taskID, taskStatus, err)
	}
	_ = q.rdb.Expire(ctx, q.taskKey(taskID), q.taskTTL).Err()
}

func (q *RecommendationQueue) beginTaskAttempt(ctx context.Context, taskID string, messageID string) (int64, error) {
	attempt, err := q.rdb.HIncrBy(ctx, q.taskKey(taskID), "attempt_count", 1).Result()
	if err != nil {
		return 0, fmt.Errorf("increment recommendation task attempt task_id=%s: %w", taskID, err)
	}
	pipe := q.rdb.Pipeline()
	pipe.HSet(ctx, q.taskKey(taskID), map[string]any{
		"status":            recommendationTaskStatusProcessing,
		"active_message_id": messageID,
		"active_attempt":    strconv.FormatInt(attempt, 10),
		"updated_at":        time.Now().Format(time.RFC3339),
	})
	pipe.HDel(ctx, q.taskKey(taskID), "error")
	pipe.Expire(ctx, q.taskKey(taskID), q.taskTTL)
	pipe.Del(ctx, q.eventsStreamKey(taskID))
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("begin recommendation task attempt task_id=%s attempt=%d: %w", taskID, attempt, err)
	}
	return attempt, nil
}

func (q *RecommendationQueue) taskAttemptActive(ctx context.Context, taskID string, messageID string, attempt int64) (bool, error) {
	values, err := q.rdb.HMGet(ctx, q.taskKey(taskID), "active_message_id", "active_attempt").Result()
	if err != nil {
		return false, fmt.Errorf("load recommendation task active attempt task_id=%s: %w", taskID, err)
	}
	if len(values) != 2 {
		return false, nil
	}
	return stringValue(values[0]) == messageID && stringValue(values[1]) == strconv.FormatInt(attempt, 10), nil
}

func (q *RecommendationQueue) taskDone(ctx context.Context, taskID string) (bool, error) {
	statusValue, err := q.rdb.HGet(ctx, q.taskKey(taskID), "status").Result()
	if errors.Is(err, redis.Nil) {
		return false, fmt.Errorf("recommendation task not found task_id=%s", taskID)
	}
	if err != nil {
		return false, fmt.Errorf("load recommendation task status task_id=%s: %w", taskID, err)
	}
	return statusValue == recommendationTaskStatusDone, nil
}

func (q *RecommendationQueue) completeTask(ctx context.Context, taskID string) error {
	pipe := q.rdb.Pipeline()
	pipe.HSet(ctx, q.taskKey(taskID), map[string]any{
		"status":     recommendationTaskStatusDone,
		"updated_at": time.Now().Format(time.RFC3339),
	})
	pipe.HDel(ctx, q.taskKey(taskID), "active_message_id", "active_attempt", "error")
	pipe.Expire(ctx, q.taskKey(taskID), q.taskTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("complete recommendation task task_id=%s: %w", taskID, err)
	}
	return nil
}

func (q *RecommendationQueue) createCachedTask(ctx context.Context, taskID string, hrID uint64, cacheKey string, now time.Time, resp *rpc.ResumeRecommendationResponse) error {
	taskKey := q.taskKey(taskID)
	pipe := q.rdb.Pipeline()
	pipe.HSet(ctx, taskKey, map[string]any{
		"task_id":    taskID,
		"hr_id":      strconv.FormatUint(hrID, 10),
		"status":     recommendationTaskStatusDone,
		"cache_key":  cacheKey,
		"cache_hit":  "true",
		"created_at": now.Format(time.RFC3339),
		"updated_at": now.Format(time.RFC3339),
	})
	pipe.Expire(ctx, taskKey, q.taskTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return status.Errorf(codes.Unavailable, "store cached recommendation task: %v", err)
	}
	if err := q.emitChunk(ctx, taskID, recommendationTaskStatusDone, "done", "命中缓存，已返回上次推荐结果", true, resp); err != nil {
		return status.Errorf(codes.Unavailable, "append cached recommendation result: %v", err)
	}
	return nil
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

func (q *RecommendationQueue) emitChunk(ctx context.Context, taskID string, taskStatus string, stage string, message string, done bool, resp *rpc.ResumeRecommendationResponse) error {
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
	eventsKey := q.eventsStreamKey(taskID)
	pipe := q.rdb.Pipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{
		Stream: eventsKey,
		Values: map[string]any{
			"payload": string(data),
		},
	})
	pipe.Expire(ctx, eventsKey, recommendationEventStreamTTL)
	_, err = pipe.Exec(ctx)
	return err
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

func (q *RecommendationQueue) resultCacheKey(hrID uint64, input ai.ResumeRecommendationInput) string {
	normalized := recommendationCacheInput{
		Version:        recommendationResultCacheVersion,
		HRID:           hrID,
		JobID:          input.JobID,
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
	return "resume_recommendation:cache:" + hex.EncodeToString(sum[:])
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
		return 5
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
	return taskStatus == recommendationTaskStatusDone || taskStatus == recommendationTaskStatusFailed
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
