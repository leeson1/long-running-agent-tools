package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/task"
	"github.com/leeson1/agent-forge/internal/template"
)

// Worker Worker Agent 流程控制器
// 负责在指定 worktree 上执行单个 feature
type Worker struct {
	executor     *session.Executor
	taskStore    *store.TaskStore
	sessionStore *store.SessionStore
	logStore     *store.LogStore
	OnEvent      SessionEventCallback // 可选：实时事件广播
}

// NewWorker 创建 Worker
func NewWorker(executor *session.Executor, taskStore *store.TaskStore, sessionStore *store.SessionStore, logStore *store.LogStore) *Worker {
	return &Worker{
		executor:     executor,
		taskStore:    taskStore,
		sessionStore: sessionStore,
		logStore:     logStore,
	}
}

// WorkerConfig Worker 执行配置
type WorkerConfig struct {
	TaskID           string
	TaskName         string
	Feature          task.Feature
	BatchNum         int
	SessionNumber    int
	WorkDir          string // worktree 路径
	ProgressContent  string // 当前 progress.txt 内容
	PendingFeatures  string // 待完成 features 摘要
	ValidatorCommand string // 验证命令
	Template         *template.Template
	Branch           string
	BaseCommit       string
}

// WorkerResult Worker 执行结果
type WorkerResult struct {
	Session          *session.Session `json:"session"`
	FeatureID        string           `json:"feature_id"`
	Branch           string           `json:"branch,omitempty"`
	WorkDir          string           `json:"work_dir,omitempty"`
	BaseCommit       string           `json:"base_commit,omitempty"`
	HeadCommit       string           `json:"head_commit,omitempty"`
	ChangedFiles     []string         `json:"changed_files,omitempty"`
	ValidationOutput string           `json:"validation_output,omitempty"`
	Success          bool             `json:"success"`
	Error            string           `json:"error,omitempty"`
}

// Run 执行 Worker 流程
func (w *Worker) Run(config WorkerConfig) *WorkerResult {
	if w.isTaskCancelled(config.TaskID) {
		return &WorkerResult{
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      task.ErrTaskCancelled.Error(),
		}
	}

	// 1. 构建 prompt
	prompt := w.buildPrompt(config)

	// 2. 保存 prompt
	w.savePrompt(config.TaskID, config.Feature.ID, prompt)

	// 3. 创建 Session
	sessionID := fmt.Sprintf("worker-%s-%s-%d", config.TaskID, config.Feature.ID, time.Now().Unix())
	sess := session.NewSession(sessionID, config.TaskID, session.TypeWorker, config.WorkDir)
	sess.FeatureID = config.Feature.ID
	sess.BatchNum = config.BatchNum
	sess.WorkerName = fmt.Sprintf("Worker-%s", config.Feature.ID)

	if err := w.sessionStore.Save(sess); err != nil {
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      fmt.Sprintf("failed to save session: %v", err),
		}
	}

	if w.isTaskCancelled(config.TaskID) {
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      task.ErrTaskCancelled.Error(),
		}
	}

	hookEnv := template.HookEnv{
		TaskID:       config.TaskID,
		SessionID:    sessionID,
		WorkspaceDir: config.WorkDir,
		FeatureID:    config.Feature.ID,
		BatchNum:     config.BatchNum,
	}
	if config.Template != nil {
		hookEnv.Extra = config.Template.Config.Variables
	}
	if config.Template != nil {
		hookResult := template.RunSessionStartHook(config.Template, hookEnv)
		if !hookResult.Success {
			errMsg := fmt.Sprintf("session start hook failed: %v", hookResult.Error)
			sess.Fail(errMsg)
			_ = w.sessionStore.Save(sess)
			if hookResult.Output != "" {
				_ = w.logStore.Append(config.TaskID, sessionID, "[hook:start] "+hookResult.Output+"\n")
			}
			return &WorkerResult{
				Session:          sess,
				FeatureID:        config.Feature.ID,
				Branch:           config.Branch,
				WorkDir:          config.WorkDir,
				BaseCommit:       config.BaseCommit,
				ValidationOutput: hookResult.Output,
				Error:            errMsg,
			}
		}
		if hookResult.Output != "" {
			_ = w.logStore.Append(config.TaskID, sessionID, "[hook:start] "+hookResult.Output+"\n")
		}
	}

	// 4. 启动 Executor
	var events []*session.SessionEvent
	var mu sync.Mutex

	handler := func(ev *session.SessionEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()

		if ev.Text != "" {
			_ = w.logStore.Append(config.TaskID, sessionID, ev.Text+"\n")
		}
		if ev.RawJSON != "" {
			_ = w.logStore.AppendEvent(config.TaskID, ev.RawJSON)
		}

		// 广播实时事件
		if w.OnEvent != nil {
			w.OnEvent(sessionID, ev)
		}
	}

	if err := w.executor.Start(sess, prompt, handler); err != nil {
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      fmt.Sprintf("failed to start worker session: %v", err),
		}
	}

	// 5. 等待完成
	w.executor.Wait(sessionID)

	// 6. 保存 session 最终状态
	_ = w.sessionStore.Save(sess)

	// 7. 判断 Claude 进程结果
	if sess.Status == session.SessionCancelled {
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      task.ErrTaskCancelled.Error(),
		}
	}
	if sess.Status == session.SessionFailed || sess.Status == session.SessionTimeout {
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      sess.Result.ErrorMessage,
		}
	}

	if config.Template != nil {
		hookResult := template.RunSessionEndHook(config.Template, hookEnv)
		if !hookResult.Success {
			errMsg := fmt.Sprintf("session end hook failed: %v", hookResult.Error)
			sess.Fail(errMsg)
			_ = w.sessionStore.Save(sess)
			if hookResult.Output != "" {
				_ = w.logStore.Append(config.TaskID, sessionID, "[hook:end] "+hookResult.Output+"\n")
			}
			return &WorkerResult{
				Session:          sess,
				FeatureID:        config.Feature.ID,
				Branch:           config.Branch,
				WorkDir:          config.WorkDir,
				BaseCommit:       config.BaseCommit,
				ValidationOutput: hookResult.Output,
				Error:            errMsg,
			}
		}
		if hookResult.Output != "" {
			_ = w.logStore.Append(config.TaskID, sessionID, "[hook:end] "+hookResult.Output+"\n")
		}
	}

	headAdvanced, headCommit, err := session.HasCommitAdvanced(config.WorkDir, config.BaseCommit)
	if err != nil {
		errMsg := fmt.Sprintf("failed to validate git history: %v", err)
		sess.Fail(errMsg)
		_ = w.sessionStore.Save(sess)
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			Error:      errMsg,
		}
	}
	if !headAdvanced {
		errMsg := "worker completed without creating a commit"
		sess.Fail(errMsg)
		_ = w.sessionStore.Save(sess)
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			HeadCommit: headCommit,
			Error:      errMsg,
		}
	}

	clean, err := session.IsWorktreeClean(config.WorkDir)
	if err != nil {
		errMsg := fmt.Sprintf("failed to inspect worktree state: %v", err)
		sess.Fail(errMsg)
		_ = w.sessionStore.Save(sess)
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			HeadCommit: headCommit,
			Error:      errMsg,
		}
	}
	if !clean {
		errMsg := "worker left the worktree dirty"
		sess.Fail(errMsg)
		_ = w.sessionStore.Save(sess)
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			HeadCommit: headCommit,
			Error:      errMsg,
		}
	}

	changedFiles, err := session.ChangedFilesSince(config.WorkDir, config.BaseCommit)
	if err != nil {
		errMsg := fmt.Sprintf("failed to inspect changed files: %v", err)
		sess.Fail(errMsg)
		_ = w.sessionStore.Save(sess)
		return &WorkerResult{
			Session:    sess,
			FeatureID:  config.Feature.ID,
			Branch:     config.Branch,
			WorkDir:    config.WorkDir,
			BaseCommit: config.BaseCommit,
			HeadCommit: headCommit,
			Error:      errMsg,
		}
	}
	for _, path := range changedFiles {
		if path == "feature_list.json" || path == "progress.txt" {
			errMsg := fmt.Sprintf("worker modified coordinator-owned file: %s", path)
			sess.Fail(errMsg)
			_ = w.sessionStore.Save(sess)
			return &WorkerResult{
				Session:      sess,
				FeatureID:    config.Feature.ID,
				Branch:       config.Branch,
				WorkDir:      config.WorkDir,
				BaseCommit:   config.BaseCommit,
				HeadCommit:   headCommit,
				ChangedFiles: changedFiles,
				Error:        errMsg,
			}
		}
	}

	var validationOutput string
	if config.Template != nil {
		validatorResult := template.RunValidator(config.Template, hookEnv)
		validationOutput = validatorResult.Output
		if validationOutput != "" {
			_ = w.logStore.Append(config.TaskID, sessionID, "[validator] "+validationOutput+"\n")
		}
		if !validatorResult.Success {
			errMsg := fmt.Sprintf("validator failed: %v", validatorResult.Error)
			sess.Fail(errMsg)
			_ = w.sessionStore.Save(sess)
			return &WorkerResult{
				Session:          sess,
				FeatureID:        config.Feature.ID,
				Branch:           config.Branch,
				WorkDir:          config.WorkDir,
				BaseCommit:       config.BaseCommit,
				HeadCommit:       headCommit,
				ChangedFiles:     changedFiles,
				ValidationOutput: validationOutput,
				Error:            errMsg,
			}
		}
	}

	return &WorkerResult{
		Session:          sess,
		FeatureID:        config.Feature.ID,
		Branch:           config.Branch,
		WorkDir:          config.WorkDir,
		BaseCommit:       config.BaseCommit,
		HeadCommit:       headCommit,
		ChangedFiles:     changedFiles,
		ValidationOutput: validationOutput,
		Success:          true,
	}
}

func (w *Worker) isTaskCancelled(taskID string) bool {
	stored, err := w.taskStore.Get(taskID)
	return err == nil && stored.Status == task.StatusCancelled
}

// buildPrompt 构建 Worker Agent 的 prompt
func (w *Worker) buildPrompt(config WorkerConfig) string {
	// 构建 steps 描述
	stepsStr := ""
	for i, step := range config.Feature.Steps {
		stepsStr += fmt.Sprintf("%d. %s\n", i+1, step)
	}

	description := config.Feature.Description
	if stepsStr != "" {
		description += "\n\n### Steps:\n" + stepsStr
	}

	validatorCmd := config.ValidatorCommand
	if validatorCmd == "" {
		validatorCmd = "echo 'No validator configured'"
	}

	vars := map[string]string{
		"task_name":           config.TaskName,
		"session_number":      fmt.Sprintf("%d", config.SessionNumber),
		"feature_id":          config.Feature.ID,
		"feature_category":    config.Feature.Category,
		"feature_description": description,
		"progress_content":    config.ProgressContent,
		"pending_features":    config.PendingFeatures,
		"validator_command":   validatorCmd,
	}
	promptTemplate := template.DefaultWorkerPrompt
	if config.Template != nil && config.Template.WorkerPrompt != "" {
		promptTemplate = config.Template.WorkerPrompt
	}
	return template.RenderPrompt(promptTemplate, vars)
}

// savePrompt 保存 prompt 到文件
func (w *Worker) savePrompt(taskID, featureID, prompt string) {
	dir := w.taskStore.PromptsDir(taskID)
	filename := fmt.Sprintf("worker-%s.txt", featureID)
	path := dir + "/" + filename
	_ = writeFile(path, prompt)
}

// FormatPendingFeatures 格式化待完成 features 列表
func FormatPendingFeatures(features []task.Feature) string {
	if len(features) == 0 {
		return "No pending features."
	}
	var lines []string
	for _, f := range features {
		deps := "none"
		if len(f.DependsOn) > 0 {
			deps = strings.Join(f.DependsOn, ", ")
		}
		lines = append(lines, fmt.Sprintf("- %s: %s (depends on: %s)", f.ID, f.Description, deps))
	}
	return strings.Join(lines, "\n")
}

// writeFile 辅助函数：写入文件（自动创建目录）
func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}
