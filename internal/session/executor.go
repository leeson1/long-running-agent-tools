package session

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// EventHandler 处理 SessionEvent 的回调函数
type EventHandler func(event *SessionEvent)

const (
	ProviderClaude = "claude"
	ProviderCodex  = "codex"
)

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	Provider     string        // agent CLI provider: claude or codex
	ClaudePath   string        // claude CLI 路径，默认 "claude"
	CodexPath    string        // codex CLI 路径，默认 "codex"
	Model        string        // 可选模型名
	MaxTurns     int           // 最大对话轮数
	Timeout      time.Duration // Session 超时
	AllowedTools []string      // 允许的工具列表
	MaxRetries   int           // 最大重试次数
}

// DefaultExecutorConfig 默认配置
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		Provider:     ProviderClaude,
		ClaudePath:   "claude",
		CodexPath:    "codex",
		MaxTurns:     50,
		Timeout:      30 * time.Minute,
		AllowedTools: nil, // nil 表示不限制
		MaxRetries:   3,
	}
}

// Executor agent CLI 进程执行器
type Executor struct {
	config  ExecutorConfig
	mu      sync.RWMutex
	procs   map[string]*runningProcess // sessionID -> process
	baseDir string                     // 任务存储根目录
}

// runningProcess 运行中的进程信息
type runningProcess struct {
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	pid       int
	sessionID string
	taskID    string
	startedAt time.Time
	done      chan struct{} // 进程结束信号
	stopped   atomic.Bool
}

// NewExecutor 创建执行器
func NewExecutor(baseDir string, config ExecutorConfig) *Executor {
	return &Executor{
		config:  normalizeExecutorConfig(config),
		procs:   make(map[string]*runningProcess),
		baseDir: baseDir,
	}
}

// Start 启动一个 agent CLI 会话
// 返回 session 对象和事件 channel
func (e *Executor) Start(sess *Session, prompt string, handler EventHandler) error {
	e.mu.Lock()
	if _, exists := e.procs[sess.ID]; exists {
		e.mu.Unlock()
		return fmt.Errorf("session %s is already running", sess.ID)
	}
	e.mu.Unlock()

	// 构建命令
	config := e.Config()
	ctx, cancel := context.WithTimeout(context.Background(), config.Timeout)
	path, args := buildCommand(config, sess)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = sess.WorkDir

	// 设置 stdin（通过管道写入 prompt）
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// 设置 stdout（实时读取 provider 事件流）
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// 设置 stderr
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// 使用进程组，确保子进程也能被正确终止
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// 自定义 cancel 行为：kill 整个进程组
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// 负 PID 表示 kill 进程组
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}

	// 启动进程
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start %s: %w", providerFromConfig(config), err)
	}

	proc := &runningProcess{
		cmd:       cmd,
		cancel:    cancel,
		pid:       cmd.Process.Pid,
		sessionID: sess.ID,
		taskID:    sess.TaskID,
		startedAt: time.Now(),
		done:      make(chan struct{}),
	}

	// 注册进程
	e.mu.Lock()
	e.procs[sess.ID] = proc
	e.mu.Unlock()

	// 更新 session 状态
	sess.PID = proc.pid
	sess.Status = SessionRunning

	// 写入 PID 文件
	e.writePIDFile(sess.TaskID, sess.ID, proc.pid)

	// 写入 prompt
	go func() {
		defer stdin.Close()
		_, _ = stdin.Write([]byte(prompt))
	}()

	// 捕获 stderr
	go func() {
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
		for scanner.Scan() {
			// stderr 内容记录到日志但不解析
			if handler != nil {
				handler(&SessionEvent{
					Timestamp: time.Now(),
					Type:      SEventSystem,
					SessionID: sess.ID,
					Text:      "[stderr] " + scanner.Text(),
				})
			}
		}
	}()

	// 实时解析 stdout event stream
	go func() {
		defer close(proc.done)
		defer func() {
			e.mu.Lock()
			delete(e.procs, sess.ID)
			e.mu.Unlock()
			e.removePIDFile(sess.TaskID, sess.ID)
		}()

		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB buffer for large outputs

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			events, err := parseProviderLine(config, line)
			if err != nil {
				// 解析失败也转发为系统事件
				if handler != nil {
					handler(&SessionEvent{
						Timestamp: time.Now(),
						Type:      SEventSystem,
						SessionID: sess.ID,
						Text:      "[unparsed] " + string(line),
					})
				}
				continue
			}

			for _, event := range events {
				if event == nil {
					continue
				}
				if event.SessionID == "" {
					event.SessionID = sess.ID
				}
				event.RawJSON = string(line)
				if handler != nil {
					handler(event)
				}

				// 如果收到 result 事件，提取 token 信息到 session
				if event.Type == SEventResult || event.Type == SEventError {
					sess.Result.TokensInput += event.InputTokens
					sess.Result.TokensOutput += event.OutputTokens
				}
			}
		}

		// 等待进程结束
		err := cmd.Wait()
		now := time.Now()
		sess.EndedAt = &now

		if proc.stopped.Load() {
			sess.Status = SessionCancelled
			if sess.Result.ErrorMessage == "" {
				sess.Result.ErrorMessage = "session cancelled"
			}
		} else if ctx.Err() == context.DeadlineExceeded {
			sess.Status = SessionTimeout
			sess.Result.ErrorMessage = "session timed out"
		} else if err != nil {
			sess.Status = SessionFailed
			sess.Result.ErrorMessage = err.Error()
		} else {
			if sess.Status == SessionRunning {
				sess.Status = SessionCompleted
			}
		}
	}()

	return nil
}

// Stop 停止一个运行中的会话
func (e *Executor) Stop(sessionID string) error {
	e.mu.RLock()
	proc, exists := e.procs[sessionID]
	e.mu.RUnlock()

	if !exists {
		return fmt.Errorf("session %s is not running", sessionID)
	}

	proc.stopped.Store(true)

	// 先尝试优雅终止（发送 SIGINT 到进程组）
	if proc.cmd.Process != nil {
		_ = syscall.Kill(-proc.cmd.Process.Pid, syscall.SIGINT)
	}

	// 等待 5 秒，如果还没结束就强制 kill
	select {
	case <-proc.done:
		return nil
	case <-time.After(5 * time.Second):
		proc.cancel()
		<-proc.done
		return nil
	}
}

// StopTask stops all running sessions that belong to the specified task.
func (e *Executor) StopTask(taskID string) error {
	e.mu.RLock()
	sessionIDs := make([]string, 0, len(e.procs))
	for sessionID, proc := range e.procs {
		if proc.taskID == taskID {
			sessionIDs = append(sessionIDs, sessionID)
		}
	}
	e.mu.RUnlock()

	var errs []error
	for _, sessionID := range sessionIDs {
		if err := e.Stop(sessionID); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// IsRunning 检查会话是否正在运行
func (e *Executor) IsRunning(sessionID string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	_, exists := e.procs[sessionID]
	return exists
}

// Wait 等待会话结束
func (e *Executor) Wait(sessionID string) {
	e.mu.RLock()
	proc, exists := e.procs[sessionID]
	e.mu.RUnlock()

	if exists {
		<-proc.done
	}
}

// RunningCount 返回正在运行的会话数量
func (e *Executor) RunningCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.procs)
}

// Config returns a snapshot of the current executor config.
func (e *Executor) Config() ExecutorConfig {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.config
}

// UpdateConfig updates the executor config for future sessions.
func (e *Executor) UpdateConfig(config ExecutorConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = normalizeExecutorConfig(config)
}

func normalizeExecutorConfig(config ExecutorConfig) ExecutorConfig {
	if config.ClaudePath == "" {
		config.ClaudePath = "claude"
	}
	if config.CodexPath == "" {
		config.CodexPath = "codex"
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Minute
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 3
	}
	config.Provider = providerFromConfig(config)
	return config
}

func providerFromConfig(config ExecutorConfig) string {
	provider := strings.ToLower(strings.TrimSpace(config.Provider))
	switch provider {
	case ProviderCodex:
		return ProviderCodex
	default:
		return ProviderClaude
	}
}

func buildCommand(config ExecutorConfig, sess *Session) (string, []string) {
	switch providerFromConfig(config) {
	case ProviderCodex:
		path := config.CodexPath
		if path == "" {
			path = "codex"
		}
		return path, buildCodexArgs(config, sess)
	default:
		path := config.ClaudePath
		if path == "" {
			path = "claude"
		}
		return path, buildClaudeArgs(config, sess)
	}
}

func parseProviderLine(config ExecutorConfig, line []byte) ([]*SessionEvent, error) {
	switch providerFromConfig(config) {
	case ProviderCodex:
		return ParseCodexJSONLine(line)
	default:
		return ParseStreamLine(line)
	}
}

// buildClaudeArgs 构建 claude CLI 命令行参数
func buildClaudeArgs(config ExecutorConfig, sess *Session) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
	}

	if config.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(config.MaxTurns))
	}

	if len(config.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(config.AllowedTools, ","))
	}

	return args
}

// buildCodexArgs 构建 Codex CLI 命令行参数。
func buildCodexArgs(config ExecutorConfig, sess *Session) []string {
	args := []string{
		"exec",
		"--json",
		"--dangerously-bypass-approvals-and-sandbox",
		"--skip-git-repo-check",
		"-C", sess.WorkDir,
	}

	if config.Model != "" {
		args = append(args, "--model", config.Model)
	}

	return append(args, "-")
}

// PID 文件管理

func (e *Executor) pidFilePath(taskID, sessionID string) string {
	return filepath.Join(e.baseDir, "tasks", taskID, "sessions", sessionID+".pid")
}

func (e *Executor) writePIDFile(taskID, sessionID string, pid int) {
	path := e.pidFilePath(taskID, sessionID)
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func (e *Executor) removePIDFile(taskID, sessionID string) {
	path := e.pidFilePath(taskID, sessionID)
	_ = os.Remove(path)
}

// ReadPIDFile 读取 PID 文件（用于崩溃恢复）
func (e *Executor) ReadPIDFile(taskID, sessionID string) (int, error) {
	path := e.pidFilePath(taskID, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(data)))
}

// IsProcessAlive 检查进程是否还存活
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
