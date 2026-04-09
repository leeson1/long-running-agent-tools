package session

import "time"

// SessionType 会话类型
type SessionType string

const (
	TypeInitializer SessionType = "initializer" // 初始化 Agent
	TypeWorker      SessionType = "worker"      // Worker Agent（编码）
	TypeResolver    SessionType = "resolver"    // Resolver Agent（冲突解决）
)

// SessionStatus 会话状态
type SessionStatus string

const (
	SessionPending   SessionStatus = "pending"   // 等待启动
	SessionRunning   SessionStatus = "running"   // 运行中
	SessionCompleted SessionStatus = "completed" // 完成
	SessionFailed    SessionStatus = "failed"    // 失败
	SessionTimeout   SessionStatus = "timeout"   // 超时
	SessionCancelled SessionStatus = "cancelled" // 取消
)

// SessionResult 会话执行结果
type SessionResult struct {
	FeaturesCompleted []string `json:"features_completed,omitempty"` // 本次完成的 feature IDs
	TokensInput       int64    `json:"tokens_input"`                 // 输入 token 数
	TokensOutput      int64    `json:"tokens_output"`                // 输出 token 数
	GitCommits        []string `json:"git_commits,omitempty"`        // 本次产生的 git commit hashes
	ErrorMessage      string   `json:"error_message,omitempty"`      // 错误信息（如果失败）
}

// Session 会话记录
type Session struct {
	ID         string        `json:"id"`
	TaskID     string        `json:"task_id"`
	Type       SessionType   `json:"type"`
	Status     SessionStatus `json:"status"`
	FeatureID  string        `json:"feature_id,omitempty"`  // Worker 关联的 feature ID
	BatchNum   int           `json:"batch_num,omitempty"`   // 所属 Batch 编号
	WorkerName string        `json:"worker_name,omitempty"` // Worker 名称 (如 "Worker A")
	WorkDir    string        `json:"work_dir"`              // 工作目录（worktree 路径）
	PID        int           `json:"pid,omitempty"`         // Claude Code 进程 PID
	RetryCount int           `json:"retry_count"`           // 重试次数
	Result     SessionResult `json:"result"`
	StartedAt  time.Time     `json:"started_at"`
	EndedAt    *time.Time    `json:"ended_at,omitempty"`
}

// NewSession 创建新会话
func NewSession(id, taskID string, sessionType SessionType, workDir string) *Session {
	return &Session{
		ID:        id,
		TaskID:    taskID,
		Type:      sessionType,
		Status:    SessionPending,
		WorkDir:   workDir,
		StartedAt: time.Now(),
	}
}

// Duration 返回会话运行时长
func (s *Session) Duration() time.Duration {
	if s.EndedAt != nil {
		return s.EndedAt.Sub(s.StartedAt)
	}
	return time.Since(s.StartedAt)
}

// TotalTokens 返回总 token 数
func (s *Session) TotalTokens() int64 {
	return s.Result.TokensInput + s.Result.TokensOutput
}

// Complete 标记会话完成
func (s *Session) Complete(result SessionResult) {
	s.Status = SessionCompleted
	s.Result = result
	now := time.Now()
	s.EndedAt = &now
}

// Fail 标记会话失败
func (s *Session) Fail(errMsg string) {
	s.Status = SessionFailed
	s.Result.ErrorMessage = errMsg
	now := time.Now()
	s.EndedAt = &now
}
