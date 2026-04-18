package agent

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/task"
	"github.com/leeson1/agent-forge/internal/template"
)

// DefaultResolverPrompt Resolver Agent 的默认 prompt 模板
const DefaultResolverPrompt = `你是一个 Git 冲突解决专家。你的任务是解决合并冲突。

## 任务信息
- 项目名称：{{task_name}}
- 冲突分支：{{conflict_branch}}
- Feature ID：{{feature_id}}

## 冲突文件
{{conflict_files}}

## 冲突 Diff
{{conflict_diffs}}

## 要求
1. 分析每个冲突文件的内容
2. 理解两个分支各自修改的意图
3. 合并两者的改动，确保不丢失任何功能
4. 使用 git add 标记每个文件已解决
5. 执行 git commit 完成合并
6. 运行验证命令确认合并正确：{{validator_command}}

## 规则
- 不要简单选择某一方的版本，除非另一方的改动确实是多余的
- 保持代码风格一致
- 如果涉及配置文件（package.json, go.mod 等），确保合并后格式正确
- 完成后必须确保代码可以编译通过
`

// Resolver Level 2 冲突解决 Agent
type Resolver struct {
	executor     *session.Executor
	taskStore    *store.TaskStore
	sessionStore *store.SessionStore
	logStore     *store.LogStore
	merger       *session.Merger
	maxRetries   int
}

// NewResolver 创建 Resolver
func NewResolver(
	executor *session.Executor,
	taskStore *store.TaskStore,
	sessionStore *store.SessionStore,
	logStore *store.LogStore,
	merger *session.Merger,
	maxRetries int,
) *Resolver {
	if maxRetries <= 0 {
		maxRetries = 2
	}
	return &Resolver{
		executor:     executor,
		taskStore:    taskStore,
		sessionStore: sessionStore,
		logStore:     logStore,
		merger:       merger,
		maxRetries:   maxRetries,
	}
}

// ResolveConfig 解决冲突配置
type ResolveConfig struct {
	TaskID           string
	TaskName         string
	Feature          task.Feature
	Branch           string
	ConflictFiles    []string
	ConflictDiffs    map[string]string
	ValidatorCommand string
}

// ResolveResult 解决结果
type ResolveResult struct {
	Session *session.Session `json:"session"`
	Success bool             `json:"success"`
	Retries int              `json:"retries"`
	Error   string           `json:"error,omitempty"`
}

// Resolve 启动 Resolver Agent 解决冲突
func (r *Resolver) Resolve(config ResolveConfig) *ResolveResult {
	var lastErr string

	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		result := r.attemptResolve(config, attempt)
		if result.Success {
			result.Retries = attempt
			return result
		}
		lastErr = result.Error
	}

	return &ResolveResult{
		Success: false,
		Retries: r.maxRetries,
		Error:   fmt.Sprintf("resolver failed after %d retries: %s", r.maxRetries+1, lastErr),
	}
}

// attemptResolve 单次尝试解决冲突
func (r *Resolver) attemptResolve(config ResolveConfig, attempt int) *ResolveResult {
	if r.isTaskCancelled(config.TaskID) {
		_ = r.merger.AbortMerge()
		return &ResolveResult{Error: task.ErrTaskCancelled.Error()}
	}

	// 1. 构建 prompt
	prompt := r.buildPrompt(config)

	// 2. 创建 Session
	sessionID := fmt.Sprintf("resolver-%s-%s-%d-%d", config.TaskID, config.Feature.ID, time.Now().Unix(), attempt)
	sess := session.NewSession(sessionID, config.TaskID, session.TypeResolver, r.merger.RepoDir())
	sess.FeatureID = config.Feature.ID

	if err := r.sessionStore.Save(sess); err != nil {
		return &ResolveResult{
			Session: sess,
			Error:   fmt.Sprintf("failed to save session: %v", err),
		}
	}

	// 3. 先制造冲突状态：merge branch
	r.merger.AbortMerge() // 确保干净状态
	mergeResult := r.merger.MergeBranch(config.Branch, config.Feature.ID)
	if mergeResult.Success {
		// 没有冲突了，直接成功
		return &ResolveResult{Session: sess, Success: true}
	}
	if !mergeResult.HasConflict {
		return &ResolveResult{
			Session: sess,
			Error:   fmt.Sprintf("merge failed without conflict: %s", mergeResult.ErrorMessage),
		}
	}

	if r.isTaskCancelled(config.TaskID) {
		_ = r.merger.AbortMerge()
		return &ResolveResult{
			Session: sess,
			Error:   task.ErrTaskCancelled.Error(),
		}
	}

	// 4. 启动 Executor（让 Agent 在冲突状态下解决）
	var events []*session.SessionEvent
	var mu sync.Mutex

	handler := func(ev *session.SessionEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()

		if ev.Text != "" {
			_ = r.logStore.Append(config.TaskID, sessionID, ev.Text+"\n")
		}
		if ev.RawJSON != "" {
			_ = r.logStore.AppendEvent(config.TaskID, ev.RawJSON)
		}
	}

	if err := r.executor.Start(sess, prompt, handler); err != nil {
		r.merger.AbortMerge()
		return &ResolveResult{
			Session: sess,
			Error:   fmt.Sprintf("failed to start resolver session: %v", err),
		}
	}

	// 5. 等待完成
	r.executor.Wait(sessionID)
	_ = r.sessionStore.Save(sess)

	// 6. 检查结果
	if sess.Status == session.SessionCancelled {
		r.merger.AbortMerge()
		return &ResolveResult{
			Session: sess,
			Error:   task.ErrTaskCancelled.Error(),
		}
	}
	if sess.Status == session.SessionFailed || sess.Status == session.SessionTimeout {
		r.merger.AbortMerge()
		return &ResolveResult{
			Session: sess,
			Error:   sess.Result.ErrorMessage,
		}
	}

	// 7. 检查冲突是否已解决（没有冲突文件了）
	remaining := r.merger.GetRemainingConflicts()
	if len(remaining) > 0 {
		r.merger.AbortMerge()
		return &ResolveResult{
			Session: sess,
			Error:   fmt.Sprintf("resolver did not resolve all conflicts, remaining: %v", remaining),
		}
	}

	return &ResolveResult{
		Session: sess,
		Success: true,
	}
}

func (r *Resolver) isTaskCancelled(taskID string) bool {
	stored, err := r.taskStore.Get(taskID)
	return err == nil && stored.Status == task.StatusCancelled
}

// buildPrompt 构建 Resolver prompt
func (r *Resolver) buildPrompt(config ResolveConfig) string {
	conflictFilesStr := strings.Join(config.ConflictFiles, "\n")

	var diffParts []string
	for file, diff := range config.ConflictDiffs {
		diffParts = append(diffParts, fmt.Sprintf("### %s\n```\n%s\n```", file, diff))
	}
	conflictDiffsStr := strings.Join(diffParts, "\n\n")

	validatorCmd := config.ValidatorCommand
	if validatorCmd == "" {
		validatorCmd = "echo 'No validator configured'"
	}

	vars := map[string]string{
		"task_name":         config.TaskName,
		"conflict_branch":   config.Branch,
		"feature_id":        config.Feature.ID,
		"conflict_files":    conflictFilesStr,
		"conflict_diffs":    conflictDiffsStr,
		"validator_command": validatorCmd,
	}
	return template.RenderPrompt(DefaultResolverPrompt, vars)
}

// ThreeLevelResolve 三级冲突解决流程
// Level 1: 自动合并 → Level 2: Resolver Agent → Level 3: 人工介入
func (r *Resolver) ThreeLevelResolve(t *task.Task, featureID, branch string, conflictFiles []string, validatorCommand string) *ResolveResult {
	// Level 1: 自动解决
	r.merger.AbortMerge() // 确保干净状态
	autoResult := r.merger.AutoResolveConflict(branch, featureID)
	if autoResult.Success {
		return &ResolveResult{Success: true}
	}

	// Level 2: Resolver Agent
	// 获取冲突详情
	detail, err := r.merger.GetConflictDetail(branch, featureID)
	if err != nil {
		return &ResolveResult{Error: fmt.Sprintf("failed to get conflict detail: %v", err)}
	}

	feature := task.Feature{ID: featureID}

	resolveResult := r.Resolve(ResolveConfig{
		TaskID:           t.ID,
		TaskName:         t.Name,
		Feature:          feature,
		Branch:           branch,
		ConflictFiles:    detail.ConflictFiles,
		ConflictDiffs:    detail.FileDiffs,
		ValidatorCommand: validatorCommand,
	})

	if resolveResult.Success {
		return resolveResult
	}

	// Level 3: 返回失败，由上层决定是否进入 conflict_wait
	return &ResolveResult{
		Session: resolveResult.Session,
		Retries: resolveResult.Retries,
		Error:   fmt.Sprintf("all resolution levels failed: %s", resolveResult.Error),
	}
}
