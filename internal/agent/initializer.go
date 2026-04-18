package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/task"
	"github.com/leeson1/agent-forge/internal/template"
)

// Initializer Initializer Agent 流程控制器
// 负责启动 Initializer 会话，监控执行，验证产出物
// SessionEventCallback 实时事件回调（用于广播到 EventBus 等）
type SessionEventCallback func(sessionID string, ev *session.SessionEvent)

type Initializer struct {
	executor     *session.Executor
	taskStore    *store.TaskStore
	sessionStore *store.SessionStore
	logStore     *store.LogStore
	OnEvent      SessionEventCallback // 可选：实时事件广播
}

// NewInitializer 创建 Initializer 实例
func NewInitializer(executor *session.Executor, taskStore *store.TaskStore, sessionStore *store.SessionStore, logStore *store.LogStore) *Initializer {
	return &Initializer{
		executor:     executor,
		taskStore:    taskStore,
		sessionStore: sessionStore,
		logStore:     logStore,
	}
}

// InitResult Initializer 执行结果
type InitResult struct {
	Session         *session.Session  `json:"session"`
	FeatureList     *task.FeatureList `json:"feature_list,omitempty"`
	ProgressContent string            `json:"progress_content,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// Run 执行 Initializer 流程
// 1. 状态转换：pending → initializing
// 2. 构建 prompt 并启动 Executor
// 3. 等待完成
// 4. 验证产出物（feature_list.json, init.sh, progress.txt）
// 5. 保存结果，状态转换：initializing → planning
func (init *Initializer) Run(t *task.Task, tmpl *template.Template) (*InitResult, error) {
	if init.isTaskCancelled(t.ID) {
		return &InitResult{Error: task.ErrTaskCancelled.Error()}, task.ErrTaskCancelled
	}

	// 1. 状态转换：pending → initializing
	if t.Status != task.StatusInitializing {
		if err := t.TransitionTo(task.StatusInitializing); err != nil {
			return nil, fmt.Errorf("failed to transition task to initializing: %w", err)
		}
		if err := init.taskStore.Update(t); err != nil {
			return nil, fmt.Errorf("failed to update task status: %w", err)
		}
	}

	// 2. 构建 prompt
	prompt := init.buildPrompt(t, tmpl)

	// 3. 保存 prompt 到文件（供调试和审计）
	init.savePrompt(t.ID, prompt)

	// 4. 创建 Session
	sessionID := fmt.Sprintf("init-%s-%d", t.ID, time.Now().Unix())
	sess := session.NewSession(sessionID, t.ID, session.TypeInitializer, t.Config.WorkspaceDir)

	if err := init.sessionStore.Save(sess); err != nil {
		return nil, fmt.Errorf("failed to save session: %w", err)
	}

	if init.isTaskCancelled(t.ID) {
		return &InitResult{
			Session: sess,
			Error:   task.ErrTaskCancelled.Error(),
		}, task.ErrTaskCancelled
	}

	// 5. 启动 Executor，收集事件
	var events []*session.SessionEvent
	var mu sync.Mutex

	handler := func(ev *session.SessionEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()

		// 实时写入日志
		if ev.Text != "" {
			_ = init.logStore.Append(t.ID, sessionID, ev.Text+"\n")
		}

		// 写入事件流
		if ev.RawJSON != "" {
			_ = init.logStore.AppendEvent(t.ID, ev.RawJSON)
		}

		// 广播实时事件
		if init.OnEvent != nil {
			init.OnEvent(sessionID, ev)
		}
	}

	if err := init.executor.Start(sess, prompt, handler); err != nil {
		return nil, fmt.Errorf("failed to start initializer session: %w", err)
	}

	// 6. 等待完成
	init.executor.Wait(sessionID)

	// 7. 保存 session 最终状态
	_ = init.sessionStore.Save(sess)

	// 8. 检查执行结果
	if sess.Status == session.SessionCancelled {
		return &InitResult{
			Session: sess,
			Error:   task.ErrTaskCancelled.Error(),
		}, task.ErrTaskCancelled
	}
	if sess.Status == session.SessionFailed || sess.Status == session.SessionTimeout {
		result := &InitResult{
			Session: sess,
			Error:   sess.Result.ErrorMessage,
		}
		_ = t.TransitionTo(task.StatusFailed)
		_ = init.taskStore.Update(t)
		return result, fmt.Errorf("initializer session failed: %s", sess.Result.ErrorMessage)
	}

	// 9. 验证产出物
	featureList, progressContent, err := init.validateOutputs(t.Config.WorkspaceDir)
	if err != nil {
		result := &InitResult{
			Session: sess,
			Error:   err.Error(),
		}
		_ = t.TransitionTo(task.StatusFailed)
		_ = init.taskStore.Update(t)
		return result, fmt.Errorf("initializer output validation failed: %w", err)
	}

	// 10. 保存 feature_list 到 store
	if err := init.taskStore.SaveFeatureList(t.ID, featureList); err != nil {
		return nil, fmt.Errorf("failed to save feature list: %w", err)
	}
	if err := init.taskStore.SaveProgress(t.ID, progressContent); err != nil {
		return nil, fmt.Errorf("failed to save progress: %w", err)
	}

	// 11. 更新任务进度
	t.Progress.FeaturesTotal = len(featureList.Features)
	t.Progress.TotalSessions++

	// 12. 状态转换：initializing → planning
	if err := t.TransitionTo(task.StatusPlanning); err != nil {
		return nil, fmt.Errorf("failed to transition task to planning: %w", err)
	}
	if err := init.taskStore.Update(t); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return &InitResult{
		Session:         sess,
		FeatureList:     featureList,
		ProgressContent: progressContent,
	}, nil
}

func (init *Initializer) isTaskCancelled(taskID string) bool {
	stored, err := init.taskStore.Get(taskID)
	return err == nil && stored.Status == task.StatusCancelled
}

// buildPrompt 构建 Initializer Agent 的 prompt
func (init *Initializer) buildPrompt(t *task.Task, tmpl *template.Template) string {
	vars := map[string]string{
		"task_name":        t.Name,
		"task_description": t.Description,
	}
	promptTemplate := template.DefaultInitializerPrompt
	if tmpl != nil && tmpl.InitializerPrompt != "" {
		promptTemplate = tmpl.InitializerPrompt
	}
	return template.RenderPrompt(promptTemplate, vars)
}

// savePrompt 保存 prompt 到文件
func (init *Initializer) savePrompt(taskID, prompt string) {
	dir := init.taskStore.PromptsDir(taskID)
	_ = os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "initializer.txt")
	_ = os.WriteFile(path, []byte(prompt), 0644)
}

// validateOutputs 验证 Initializer 产出物
func (init *Initializer) validateOutputs(workDir string) (*task.FeatureList, string, error) {
	featureList, err := init.validateFeatureList(workDir)
	if err != nil {
		return nil, "", fmt.Errorf("feature_list.json validation failed: %w", err)
	}

	if err := init.validateInitScript(workDir); err != nil {
		return nil, "", fmt.Errorf("init.sh validation failed: %w", err)
	}

	progressContent, err := init.validateProgressFile(workDir)
	if err != nil {
		return nil, "", fmt.Errorf("progress.txt validation failed: %w", err)
	}

	return featureList, progressContent, nil
}

// validateFeatureList 验证 feature_list.json
func (init *Initializer) validateFeatureList(workDir string) (*task.FeatureList, error) {
	path := filepath.Join(workDir, "feature_list.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("feature_list.json not found in workspace")
		}
		return nil, fmt.Errorf("failed to read feature_list.json: %w", err)
	}

	var fl task.FeatureList
	if err := json.Unmarshal(data, &fl); err != nil {
		return nil, fmt.Errorf("failed to parse feature_list.json: %w", err)
	}

	if len(fl.Features) == 0 {
		return nil, fmt.Errorf("feature_list.json contains no features")
	}

	if err := fl.Validate(); err != nil {
		return nil, err
	}

	return &fl, nil
}

// validateInitScript 验证 init.sh 存在且可执行
func (init *Initializer) validateInitScript(workDir string) error {
	path := filepath.Join(workDir, "init.sh")

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("init.sh not found in workspace")
		}
		return fmt.Errorf("failed to stat init.sh: %w", err)
	}

	if info.Mode()&0111 == 0 {
		return fmt.Errorf("init.sh is not executable")
	}

	return nil
}

// validateProgressFile 验证 progress.txt 存在且非空
func (init *Initializer) validateProgressFile(workDir string) (string, error) {
	path := filepath.Join(workDir, "progress.txt")

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("progress.txt not found in workspace")
		}
		return "", fmt.Errorf("failed to stat progress.txt: %w", err)
	}

	if info.Size() == 0 {
		return "", fmt.Errorf("progress.txt is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read progress.txt: %w", err)
	}

	return string(data), nil
}
