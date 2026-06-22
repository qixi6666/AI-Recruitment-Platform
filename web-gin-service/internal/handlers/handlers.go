package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"recruitment/shared/rpc"
	"recruitment/web-gin-service/internal/client"
	"recruitment/web-gin-service/internal/httputil"
	"recruitment/web-gin-service/internal/middleware"
)

type Handler struct {
	auth            rpc.AuthServiceClient
	jobs            rpc.JobServiceClient
	candidates      rpc.CandidateServiceClient
	applications    rpc.ApplicationServiceClient
	recommendations rpc.ResumeRecommendationServiceClient
	redis           *redis.Client
	jwtSecret       string
	jwtExpireHours  int64
	uploadDir       string
}

func New(logic client.LogicClients, redisClient *redis.Client, jwtSecret string, jwtExpireHours int64, uploadDir string) *Handler {
	return &Handler{
		auth:            logic.Auth,
		jobs:            logic.Jobs,
		candidates:      logic.Candidates,
		applications:    logic.Applications,
		recommendations: logic.Recommendations,
		redis:           redisClient,
		jwtSecret:       jwtSecret,
		jwtExpireHours:  jwtExpireHours,
		uploadDir:       uploadDir,
	}
}

func (h *Handler) RegisterRoutes(r *gin.Engine) {
	api := r.Group("/api/v1")
	api.POST("/auth/register", h.Register)
	api.POST("/auth/login", h.Login)
	api.GET("/jobs", h.ListPublicJobs)

	auth := api.Group("")
	auth.Use(middleware.JWT(h.jwtSecret))

	user := auth.Group("/me", middleware.RequireRole(rpc.RoleCandidate))
	user.GET("/profile", h.GetProfile)
	user.PUT("/profile", h.UpsertProfile)
	user.POST("/resume/upload", h.UploadResume)
	user.GET("/applications", h.ListMyApplications)

	auth.POST("/jobs/:id/apply", middleware.RequireRole(rpc.RoleCandidate), h.ApplyJob)

	hr := auth.Group("/hr", middleware.RequireRole(rpc.RoleHR))
	hr.GET("/jobs", h.ListHRJobs)
	hr.POST("/jobs", h.CreateJob)
	hr.PUT("/jobs/:id", h.UpdateJob)
	hr.PATCH("/jobs/:id/offline", h.OfflineJob)
	hr.GET("/applications", h.ListHRApplications)
	hr.GET("/resumes/:id/download", h.DownloadResume)
	hr.POST("/resume-recommendations", middleware.DuplicateSubmit(h.redis, 3*time.Second), h.RecommendResumes)
	hr.POST("/resume-recommendations/stream", h.RecommendResumesStream)
	hr.GET("/resume-recommendations/:task_id/stream", h.WatchResumeRecommendationTask)
}

func (h *Handler) Register(c *gin.Context) {
	var req rpc.RegisterRequest
	if !bind(c, &req) {
		return
	}
	resp, err := h.auth.Register(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.Created(c, resp)
}

func (h *Handler) Login(c *gin.Context) {
	var req rpc.LoginRequest
	if !bind(c, &req) {
		return
	}
	resp, err := h.auth.Login(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	token, err := middleware.Sign(h.jwtSecret, h.jwtExpireHours, resp.User)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	httputil.OK(c, gin.H{"token": token, "user": resp.User})
}

func (h *Handler) ListPublicJobs(c *gin.Context) {
	resp, err := h.jobs.ListJobs(c.Request.Context(), &rpc.ListJobsRequest{
		Page: page(c), PageSize: pageSize(c), Status: "open", Keyword: c.Query("keyword"),
	})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) ListHRJobs(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.jobs.ListJobs(c.Request.Context(), &rpc.ListJobsRequest{
		Page: page(c), PageSize: pageSize(c), Keyword: c.Query("keyword"), Status: c.Query("status"), Actor: actor, OnlyMine: true,
	})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) CreateJob(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	var req rpc.CreateJobRequest
	if !bind(c, &req) {
		return
	}
	req.Actor = actor
	resp, err := h.jobs.CreateJob(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.Created(c, resp)
}

func (h *Handler) UpdateJob(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	var req rpc.UpdateJobRequest
	if !bind(c, &req) {
		return
	}
	req.Actor = actor
	req.ID = uint64(pathID(c))
	resp, err := h.jobs.UpdateJob(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) OfflineJob(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.jobs.OfflineJob(c.Request.Context(), &rpc.OfflineJobRequest{Actor: actor, ID: uint64(pathID(c))})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) GetProfile(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.candidates.GetProfile(c.Request.Context(), &rpc.GetProfileRequest{Actor: actor})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) UpsertProfile(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	var req rpc.UpsertProfileRequest
	if !bind(c, &req) {
		return
	}
	req.Actor = actor
	resp, err := h.candidates.UpsertProfile(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) UploadResume(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	fileHeader, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resume file is required"})
		return
	}
	if fileHeader.Size <= 0 || fileHeader.Size > 20*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resume size must be within 20MB"})
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	defer file.Close()

	contentType, err := validateResumeUpload(fileHeader.Filename, file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	relativePath := filepath.Join("resumes", strconv.FormatUint(actor.UserID, 10), resumeStorageName(fileHeader.Filename))
	fullPath := filepath.Join(h.uploadDir, relativePath)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	dst, err := os.Create(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer dst.Close()
	if _, err := io.Copy(dst, file); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	resp, err := h.candidates.SaveResume(c.Request.Context(), &rpc.SaveResumeRequest{
		Actor: actor, FileName: filepath.Base(fileHeader.Filename), FilePath: fullPath,
		ContentType: contentType, Size: fileHeader.Size,
	})
	if err != nil {
		_ = os.Remove(fullPath)
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) DownloadResume(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.candidates.GetResume(c.Request.Context(), &rpc.GetResumeRequest{Actor: actor, ResumeID: pathID(c)})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	if resp.Resume == nil || resp.Resume.FilePath == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "resume not found"})
		return
	}
	c.FileAttachment(resp.Resume.FilePath, resp.Resume.FileName)
}

func (h *Handler) ApplyJob(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.applications.ApplyJob(c.Request.Context(), &rpc.ApplyJobRequest{Actor: actor, JobID: uint64(pathID(c))})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.Created(c, resp)
}

func (h *Handler) ListMyApplications(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.applications.ListApplications(c.Request.Context(), &rpc.ListApplicationsRequest{
		Actor: actor, Page: page(c), PageSize: pageSize(c),
	})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) ListHRApplications(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	resp, err := h.applications.ListApplications(c.Request.Context(), &rpc.ListApplicationsRequest{
		Actor: actor, Page: page(c), PageSize: pageSize(c), JobID: uint64(queryUint(c, "job_id")),
	})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	httputil.OK(c, resp)
}

func (h *Handler) RecommendResumes(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	var req rpc.ResumeRecommendationRequest
	if !bind(c, &req) {
		return
	}
	req.Actor = actor
	resp, err := h.recommendations.CreateResumeRecommendationTask(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	resp.StreamURL = fmt.Sprintf("/api/v1/hr/resume-recommendations/%s/stream", resp.TaskID)
	httputil.Created(c, resp)
}

func (h *Handler) RecommendResumesStream(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	var req rpc.ResumeRecommendationRequest
	if !bind(c, &req) {
		return
	}
	req.Actor = actor
	task, err := h.recommendations.CreateResumeRecommendationTask(c.Request.Context(), &req)
	if err != nil {
		httputil.Error(c, err)
		return
	}
	h.writeRecommendationTaskSSE(c, actor, task.TaskID)
}

func (h *Handler) WatchResumeRecommendationTask(c *gin.Context) {
	actor, _ := middleware.Actor(c)
	h.writeRecommendationTaskSSE(c, actor, c.Param("task_id"))
}

func (h *Handler) writeRecommendationTaskSSE(c *gin.Context, actor *rpc.Actor, taskID string) {
	if h.redis != nil {
		h.writeRecommendationTaskSSEFromRedis(c, actor, taskID)
		return
	}
	stream, err := h.recommendations.WatchResumeRecommendationTask(c.Request.Context(), &rpc.ResumeRecommendationTaskWatchRequest{
		Actor:  actor,
		TaskID: taskID,
	})
	if err != nil {
		httputil.Error(c, err)
		return
	}
	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	c.Status(http.StatusOK)
	for {
		chunk, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return
			}
			writeSSE(c.Writer, "error", gin.H{"error": err.Error()})
			flusher.Flush()
			return
		}
		event := "progress"
		if chunk.Done {
			event = "done"
		}
		writeSSE(c.Writer, event, chunk)
		flusher.Flush()
		if chunk.Done {
			return
		}
	}
}

func (h *Handler) writeRecommendationTaskSSEFromRedis(c *gin.Context, actor *rpc.Actor, taskID string) {
	if actor == nil || actor.UserID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing actor"})
		return
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_id is required"})
		return
	}
	state, err := h.redis.HGetAll(c.Request.Context(), resumeRecommendationTaskKey(taskID)).Result()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error()})
		return
	}
	if len(state) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "recommendation task not found"})
		return
	}
	if state["hr_id"] != strconv.FormatUint(actor.UserID, 10) {
		c.JSON(http.StatusForbidden, gin.H{"error": "cannot watch another hr's recommendation task"})
		return
	}

	c.Header("Content-Type", "text/event-stream; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}
	c.Status(http.StatusOK)

	lastID, done := h.replayRecommendationTaskEvents(c, taskID, flusher)
	if done {
		return
	}
	if lastID == "" {
		lastID = "0-0"
	}
	for {
		streams, err := h.redis.XRead(c.Request.Context(), &redis.XReadArgs{
			Streams: []string{resumeRecommendationTaskEventsKey(taskID), lastID},
			Count:   32,
			Block:   5 * time.Second,
		}).Result()
		if err != nil {
			if err == redis.Nil {
				continue
			}
			if c.Request.Context().Err() != nil {
				return
			}
			writeSSE(c.Writer, "error", gin.H{"error": err.Error()})
			flusher.Flush()
			return
		}
		for _, stream := range streams {
			for _, msg := range stream.Messages {
				lastID = msg.ID
				chunk, err := decodeRecommendationTaskEvent(msg)
				if err != nil {
					writeSSE(c.Writer, "error", gin.H{"error": err.Error()})
					flusher.Flush()
					return
				}
				event := "progress"
				if chunk.Done {
					event = "done"
				}
				writeSSE(c.Writer, event, chunk)
				flusher.Flush()
				if chunk.Done {
					h.destroyRecommendationTaskStream(c, taskID)
					return
				}
			}
		}
	}
}

func (h *Handler) replayRecommendationTaskEvents(c *gin.Context, taskID string, flusher http.Flusher) (string, bool) {
	items, err := h.redis.XRange(c.Request.Context(), resumeRecommendationTaskEventsKey(taskID), "-", "+").Result()
	if err != nil {
		writeSSE(c.Writer, "error", gin.H{"error": err.Error()})
		flusher.Flush()
		return "", true
	}
	lastID := ""
	for _, item := range items {
		lastID = item.ID
		chunk, err := decodeRecommendationTaskEvent(item)
		if err != nil {
			writeSSE(c.Writer, "error", gin.H{"error": err.Error()})
			flusher.Flush()
			return lastID, true
		}
		event := "progress"
		if chunk.Done {
			event = "done"
		}
		writeSSE(c.Writer, event, chunk)
		flusher.Flush()
		if chunk.Done {
			h.destroyRecommendationTaskStream(c, taskID)
			return lastID, true
		}
	}
	return lastID, false
}

func (h *Handler) destroyRecommendationTaskStream(c *gin.Context, taskID string) {
	_ = h.redis.Del(c.Request.Context(), resumeRecommendationTaskEventsKey(taskID)).Err()
}

func resumeRecommendationTaskKey(taskID string) string {
	return "resume_recommendation:task:" + taskID
}

func resumeRecommendationTaskEventsKey(taskID string) string {
	return resumeRecommendationTaskKey(taskID) + ":events"
}

func decodeRecommendationTaskEvent(msg redis.XMessage) (*rpc.ResumeRecommendationStreamChunk, error) {
	payload := fmt.Sprint(msg.Values["payload"])
	if payload == "" {
		return nil, fmt.Errorf("empty recommendation task event payload")
	}
	var chunk rpc.ResumeRecommendationStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return nil, err
	}
	return &chunk, nil
}

func writeSSE(w io.Writer, event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		data = []byte(`{"error":"marshal sse payload failed"}`)
	}
	fmt.Fprintf(w, "event: %s\n", event)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func validateResumeUpload(fileName string, file io.ReadSeeker) (string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != ".pdf" && ext != ".doc" && ext != ".docx" {
		return "", fmt.Errorf("resume only supports pdf, doc, docx")
	}
	head := make([]byte, 16)
	n, err := file.Read(head)
	if err != nil && err != io.EOF {
		return "", err
	}
	head = head[:n]
	if len(head) < 4 {
		return "", fmt.Errorf("invalid file header")
	}
	switch ext {
	case ".pdf":
		if string(head[:4]) != "%PDF" {
			return "", fmt.Errorf("pdf header mismatch")
		}
		return "application/pdf", nil
	case ".doc":
		if len(head) < 8 || head[0] != 0xD0 || head[1] != 0xCF || head[2] != 0x11 || head[3] != 0xE0 {
			return "", fmt.Errorf("doc header mismatch")
		}
		return "application/msword", nil
	case ".docx":
		if head[0] != 0x50 || head[1] != 0x4B || head[2] != 0x03 || head[3] != 0x04 {
			return "", fmt.Errorf("docx header mismatch")
		}
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document", nil
	default:
		return "", fmt.Errorf("unsupported resume file")
	}
}

func resumeStorageName(fileName string) string {
	base := filepath.Base(fileName)
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, base)
	return fmt.Sprintf("%d_%s", time.Now().UnixNano(), base)
}

func bind(c *gin.Context, req any) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return false
	}
	return true
}

func page(c *gin.Context) int32 {
	return int32(queryInt(c, "page", 1))
}

func pageSize(c *gin.Context) int32 {
	return int32(queryInt(c, "page_size", 20))
}

func pathID(c *gin.Context) uint64 {
	v, _ := strconv.ParseUint(c.Param("id"), 10, 64)
	return v
}

func queryUint(c *gin.Context, key string) uint64 {
	v, _ := strconv.ParseUint(c.Query(key), 10, 64)
	return v
}

func queryInt(c *gin.Context, key string, fallback int) int {
	v, err := strconv.Atoi(c.Query(key))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}
