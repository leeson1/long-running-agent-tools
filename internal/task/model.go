package task

import "time"

// TaskStatus 表示任务的当前状态
type TaskStatus string

const (
	StatusPending       TaskStatus = "pending"        // 已创建，等待启动
	StatusInitializing  TaskStatus = "initializing"   // Initializer Agent 运行中
	StatusPlanning      TaskStatus = "planning"       // Coordinator 拓扑排序中
	StatusRunning       TaskStatus = "running"        // Workers 执行中
	StatusMerging       TaskStatus = "merging"        // 合并分支中
	StatusAutoResolving TaskStatus = "auto_resolving" // Level 1 自动解决冲突中
	StatusAgentResolving TaskStatus = "agent_resolving" // Level 2 Resolver Agent 解决冲突中
	StatusValidating    TaskStatus = "validating"     // 集成测试验证中
	StatusConflictWait  TaskStatus = "conflict_wait"  // 等待用户手动解决冲突
	StatusPaused        TaskStatus = "paused"         // 用户暂停
	StatusCompleted     TaskStatus = "completed"      // 全部完成
	StatusFailed        TaskStatus = "failed"         // 失败（可重试）
	StatusCancelled     TaskStatus = "cancelled"      // 用户取消
)

// AllStatuses 返回所有有效的任务状态
func AllStatuses() []TaskStatus {
	return []TaskStatus{
		StatusPending, StatusInitializing, StatusPlanning, StatusRunning,
		StatusMerging, StatusAutoResolving, StatusAgentResolving, StatusValidating,
		StatusConflictWait, StatusPaused, StatusCompleted, StatusFailed, StatusCancelled,
	}
}

// IsTerminal 判断是否为终态
func (s TaskStatus) IsTerminal() bool {
	return s == StatusCompleted || s == StatusFailed || s == StatusCancelled
}

// IsActive 判断任务是否处于活跃状态
func (s TaskStatus) IsActive() bool {
	return s == StatusInitializing || s == StatusPlanning || s == StatusRunning ||
		s == StatusMerging || s == StatusAutoResolving || s == StatusAgentResolving ||
		s == StatusValidating
}

// ValidTransitions 定义合法的状态转换
var ValidTransitions = map[TaskStatus][]TaskStatus{
	StatusPending:        {StatusInitializing, StatusCancelled},
	StatusInitializing:   {StatusPlanning, StatusFailed, StatusCancelled},
	StatusPlanning:       {StatusRunning, StatusFailed, StatusCancelled},
	StatusRunning:        {StatusMerging, StatusPaused, StatusFailed, StatusCancelled, StatusCompleted},
	StatusMerging:        {StatusAutoResolving, StatusValidating, StatusFailed, StatusCancelled},
	StatusAutoResolving:  {StatusAgentResolving, StatusValidating, StatusFailed},
	StatusAgentResolving: {StatusValidating, StatusConflictWait, StatusFailed},
	StatusValidating:     {StatusRunning, StatusCompleted, StatusFailed},
	StatusConflictWait:   {StatusRunning, StatusCancelled},
	StatusPaused:         {StatusRunning, StatusCancelled},
	StatusFailed:         {StatusInitializing, StatusRunning, StatusCancelled},
	StatusCompleted:      {},
	StatusCancelled:      {},
}

// CanTransitionTo 检查是否可以从当前状态转换到目标状态
func (s TaskStatus) CanTransitionTo(target TaskStatus) bool {
	targets, ok := ValidTransitions[s]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == target {
			return true
		}
	}
	return false
}

// TaskConfig 任务配置
type TaskConfig struct {
	MaxParallelWorkers int    `json:"max_parallel_workers"` // 最大并行 Worker 数
	SessionTimeout     string `json:"session_timeout"`      // Session 超时时间 (如 "30m")
	WorkspaceDir       string `json:"workspace_dir"`        // 工作目录路径
}

// TaskProgress 任务进度
type TaskProgress struct {
	CurrentBatch      int     `json:"current_batch"`      // 当前 Batch 编号
	TotalBatches      int     `json:"total_batches"`      // 总 Batch 数
	FeaturesCompleted int     `json:"features_completed"` // 已完成 feature 数
	FeaturesTotal     int     `json:"features_total"`     // 总 feature 数
	TotalSessions     int     `json:"total_sessions"`     // 总 Session 数
	TotalTokens       int64   `json:"total_tokens"`       // 总 token 消耗
	EstimatedCost     float64 `json:"estimated_cost"`     // 预估费用 (USD)
}

// Task 任务主体结构
type Task struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Template    string       `json:"template"`
	Status      TaskStatus   `json:"status"`
	Config      TaskConfig   `json:"config"`
	Progress    TaskProgress `json:"progress"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// NewTask 创建一个新任务
func NewTask(id, name, description, template string, config TaskConfig) *Task {
	now := time.Now()
	return &Task{
		ID:          id,
		Name:        name,
		Description: description,
		Template:    template,
		Status:      StatusPending,
		Config:      config,
		Progress:    TaskProgress{},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// TransitionTo 将任务状态转换到目标状态
func (t *Task) TransitionTo(target TaskStatus) error {
	if !t.Status.CanTransitionTo(target) {
		return &InvalidTransitionError{From: t.Status, To: target}
	}
	t.Status = target
	t.UpdatedAt = time.Now()
	return nil
}

// InvalidTransitionError 非法状态转换错误
type InvalidTransitionError struct {
	From TaskStatus
	To   TaskStatus
}

func (e *InvalidTransitionError) Error() string {
	return "invalid state transition from " + string(e.From) + " to " + string(e.To)
}
