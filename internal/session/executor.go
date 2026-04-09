package session

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// EventHandler 处理 SessionEvent 的回调函数
type EventHandler func(event *SessionEvent)

// ExecutorConfig 执行器配置
type ExecutorConfig struct {
	ClaudePath   string        // claude CLI 路径，默认 "claude"
	MaxTurns     int           // 最大对话轮数
	Timeout      time.Duration // Session 超时
	AllowedTools []string      // 允许的工具列表
	MaxRetries   int           // 最大重试次数
}

// DefaultExecutorConfig 默认配置
func DefaultExecutorConfig() ExecutorConfig {
	return ExecutorConfig{
		ClaudePath:   "claude",
		MaxTurns:     50,
		Timeout:      30 * time.Minute,
		AllowedTools: nil, // nil 表示不限制
		MaxRetries:   3,
	}
}

// Executor Claude Code CLI 进程执行器
type Executor struct {
	config   ExecutorConfig
	mu       sync.RWMutex
	procs    map[string]*runningProcess // sessionID -> process
	baseDir  string                     // 任务存储根目录
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
}

// NewExecutor 创建执行器
func NewExecutor(baseDir string, config ExecutorConfig) *Executor {
	return &Executor{
		config:  config,
		procs:   make(map[string]*runningProcess),
		baseDir: baseDir,
	}
}

// Start 启动一个 Claude Code CLI 会话
// 返回 session 对象和事件 channel
func (e *Executor) Start(sess *Session, prompt string, handler EventHandler) error {
	e.mu.Lock()
	if _, exists := e.procs[sess.ID]; exists {
		e.mu.Unlock()
		return fmt.Errorf("session %s is already running", sess.ID)
	}
	e.mu.Unlock()

	// 构建命令
	ctx, cancel := context.WithTimeout(context.Background(), e.config.Timeout)
	args := e.buildArgs(sess)
	cmd := exec.CommandContext(ctx, e.config.ClaudePath, args...)
	cmd.Dir = sess.WorkDir

	// 设置 stdin（通过管道写入 prompt）
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	// 设置 stdout（实时读取 stream-json）
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
		return fmt.Errorf("failed to start claude: %w", err)
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

	// 实时解析 stdout stream-json
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

			event, err := ParseStreamLine(line)
			if err != nil {
				// 解析失败也转发为系统事件
				if handler != nil {
					handler(&SessionEvent{
						Timestamp: time.Now(),
						Type:      SEventSystem,
						SessionID: sess.ID,
						Text:      "[parse error] " + string(line),
					})
				}
				continue
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

		// 等待进程结束
		err := cmd.Wait()
		now := time.Now()
		sess.EndedAt = &now

		if ctx.Err() == context.DeadlineExceeded {
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

// buildArgs 构建 claude CLI 命令行参数
func (e *Executor) buildArgs(sess *Session) []string {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
	}

	if e.config.MaxTurns > 0 {
		args = append(args, "--max-turns", strconv.Itoa(e.config.MaxTurns))
	}

	if len(e.config.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(e.config.AllowedTools, ","))
	}

	return args
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
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// 发送信号 0 检查进程是否存在
	err = proc.Signal(os.Signal(nil))
	return err == nil
}
