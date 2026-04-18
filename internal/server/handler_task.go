package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leeson1/agent-forge/internal/stream"
	"github.com/leeson1/agent-forge/internal/task"
)

// --- Request/Response 类型 ---

// CreateTaskRequest 创建任务请求
type CreateTaskRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Template    string `json:"template"`
	Config      struct {
		MaxParallelWorkers int    `json:"max_parallel_workers"`
		SessionTimeout     string `json:"session_timeout"`
		WorkspaceDir       string `json:"workspace_dir"`
	} `json:"config"`
}

// TaskResponse 任务响应
type TaskResponse struct {
	*task.Task
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

// --- Handlers ---

// HealthCheck 健康检查
func (s *Server) HealthCheck(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "agent-forge",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// CreateTask 创建任务
func (s *Server) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.Config.WorkspaceDir == "" {
		writeError(w, http.StatusBadRequest, "workspace_dir is required")
		return
	}

	taskID := fmt.Sprintf("task-%d", time.Now().UnixNano())
	config := task.TaskConfig{
		MaxParallelWorkers: req.Config.MaxParallelWorkers,
		SessionTimeout:     req.Config.SessionTimeout,
		WorkspaceDir:       req.Config.WorkspaceDir,
	}
	if config.MaxParallelWorkers <= 0 {
		config.MaxParallelWorkers = 2
	}
	if config.SessionTimeout == "" {
		config.SessionTimeout = "30m"
	}

	t := task.NewTask(taskID, req.Name, req.Description, req.Template, config)
	if err := s.taskStore.Create(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

// ListTasks 任务列表
func (s *Server) ListTasks(w http.ResponseWriter, r *http.Request) {
	statusStr := r.URL.Query().Get("status")
	var statusFilter *task.TaskStatus
	if statusStr != "" {
		st := task.TaskStatus(statusStr)
		statusFilter = &st
	}

	tasks, err := s.taskStore.List(statusFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if tasks == nil {
		tasks = []*task.Task{}
	}

	writeJSON(w, http.StatusOK, tasks)
}

// GetTask 获取任务详情
func (s *Server) GetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	t, err := s.taskStore.Get(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}

	writeJSON(w, http.StatusOK, t)
}

// UpdateTask 更新任务配置
func (s *Server) UpdateTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	t, err := s.taskStore.Get(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}

	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if name, ok := updates["name"].(string); ok {
		t.Name = name
	}
	if desc, ok := updates["description"].(string); ok {
		t.Description = desc
	}

	t.UpdatedAt = time.Now()
	if err := s.taskStore.Update(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, t)
}

// DeleteTask 删除任务
func (s *Server) DeleteTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	if err := s.taskStore.Delete(taskID); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "task deleted"})
}

// StartTask 启动任务（在后台 goroutine 中执行完整管线）
func (s *Server) StartTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	s.taskLifecycleMu.Lock()
	defer s.taskLifecycleMu.Unlock()

	t, err := s.taskStore.Get(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}

	if t.Status != task.StatusPending && t.Status != task.StatusFailed {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("task cannot be started from status: %s", t.Status))
		return
	}

	// Reserve the task before launching the background pipeline so duplicate
	// start requests cannot race against the initializer status transition.
	if err := t.TransitionTo(task.StatusInitializing); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.taskStore.Update(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.eventBus.Publish(stream.NewEvent(stream.EventTaskStatus, taskID, map[string]string{
		"status": string(t.Status),
	}))

	// 立即返回响应，管线在后台执行
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "task pipeline starting",
		"task_id": taskID,
	})

	// 后台启动执行管线
	runPipeline := s.runPipeline
	if runPipeline == nil {
		runPipeline = s.pipeline.Run
	}
	go runPipeline(t)
}

// StopTask 停止任务
func (s *Server) StopTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	t, err := s.taskStore.Get(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}

	if !t.Status.IsActive() {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("task is not active: %s", t.Status))
		return
	}

	if err := t.TransitionTo(task.StatusCancelled); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	t.UpdatedAt = time.Now()
	if err := s.taskStore.Update(t); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.eventBus.Publish(stream.NewEvent(stream.EventTaskStatus, taskID, map[string]string{
		"status": string(t.Status),
	}))

	go func() {
		_ = s.executor.StopTask(taskID)
	}()

	writeJSON(w, http.StatusOK, t)
}

// --- Session Handlers ---

// ListSessions 获取会话列表
func (s *Server) ListSessions(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	sessions, err := s.sessionStore.List(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if sessions == nil {
		writeJSON(w, http.StatusOK, []interface{}{})
		return
	}

	writeJSON(w, http.StatusOK, sessions)
}

// GetSession 获取会话详情
func (s *Server) GetSession(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	sessionID := chi.URLParam(r, "sessionID")

	sess, err := s.sessionStore.Get(taskID, sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("session not found: %s", sessionID))
		return
	}

	writeJSON(w, http.StatusOK, sess)
}

// --- Feature Handlers ---

// GetFeatures 获取 feature list
func (s *Server) GetFeatures(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	fl, err := s.taskStore.GetFeatureList(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "feature list not found")
		return
	}

	writeJSON(w, http.StatusOK, fl)
}

// --- Log/Event Handlers ---

// GetLogs 获取 session 日志
func (s *Server) GetLogs(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	sessionID := chi.URLParam(r, "sessionID")

	lines := r.URL.Query().Get("tail")
	if lines != "" {
		n := 100 // 默认
		fmt.Sscanf(lines, "%d", &n)
		tailLines, err := s.logStore.Tail(taskID, sessionID, n)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, tailLines)
		return
	}

	content, err := s.logStore.Read(taskID, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

// GetEvents 获取任务事件
func (s *Server) GetEvents(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	events, err := s.logStore.ReadEvents(taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if events == nil {
		events = []string{}
	}

	writeJSON(w, http.StatusOK, events)
}

// --- Intervention Handler ---

// InterventionRequest 干预请求
type InterventionRequest struct {
	Content      string `json:"content"`
	TargetWorker string `json:"target_worker,omitempty"`
}

// Intervene 发送人工干预消息
func (s *Server) Intervene(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")

	// 验证任务存在
	if _, err := s.taskStore.Get(taskID); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}

	var req InterventionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}

	// 发布干预事件到 EventBus，广播给所有订阅者
	event := stream.NewEvent(stream.EventIntervention, taskID, map[string]string{
		"content":       req.Content,
		"target_worker": req.TargetWorker,
		"sender":        "human",
	})
	s.eventBus.Publish(event)

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "intervention sent",
		"task_id": taskID,
	})
}

// --- 辅助函数 ---

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, ErrorResponse{
		Error:   http.StatusText(status),
		Code:    status,
		Message: message,
	})
}
