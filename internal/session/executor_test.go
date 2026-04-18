package session

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestExecutor_StartAndWait(t *testing.T) {
	// 使用 echo 模拟 claude CLI 输出 stream-json
	tmpDir := t.TempDir()
	taskDir := filepath.Join(tmpDir, "tasks", "test-task", "sessions")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	workDir := t.TempDir()

	// 创建一个 mock 脚本，输出 stream-json 格式
	mockScript := filepath.Join(tmpDir, "mock-claude.sh")
	scriptContent := `#!/bin/bash
# Read stdin (prompt) and discard
cat > /dev/null
# Output stream-json events
echo '{"type":"system","subtype":"init","session_id":"test-session","cwd":"'$PWD'"}'
echo '{"type":"assistant","message":{"id":"msg_01","role":"assistant","content":[{"type":"text","text":"Hello from mock"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},"session_id":"test-session"}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":100,"num_turns":1,"result":"done","stop_reason":"end_turn","total_cost_usd":0.001,"session_id":"test-session","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	config := ExecutorConfig{
		ClaudePath: mockScript,
		MaxTurns:   10,
		Timeout:    10 * time.Second,
		MaxRetries: 1,
	}

	exec := NewExecutor(tmpDir, config)

	sess := NewSession("test-session", "test-task", TypeWorker, workDir)

	// 收集事件
	var events []*SessionEvent
	var mu sync.Mutex
	handler := func(ev *SessionEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	// 启动
	if err := exec.Start(sess, "test prompt", handler); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 应该在运行
	if !exec.IsRunning("test-session") {
		t.Error("Session should be running")
	}

	if exec.RunningCount() != 1 {
		t.Errorf("RunningCount: got %d, want 1", exec.RunningCount())
	}

	// 等待结束
	exec.Wait("test-session")

	// 不再运行
	if exec.IsRunning("test-session") {
		t.Error("Session should not be running after Wait")
	}

	// 检查收集到的事件
	mu.Lock()
	eventCount := len(events)
	mu.Unlock()

	if eventCount < 2 {
		t.Errorf("Expected at least 2 events, got %d", eventCount)
	}

	// 检查 session 状态
	if sess.Status != SessionCompleted {
		t.Errorf("Session status: got %s, want %s", sess.Status, SessionCompleted)
	}

	// 检查 token 累积
	if sess.Result.TokensInput == 0 {
		t.Error("TokensInput should be > 0")
	}
}

func TestExecutor_StartAndWaitCodexProvider(t *testing.T) {
	tmpDir := t.TempDir()
	taskDir := filepath.Join(tmpDir, "tasks", "test-task", "sessions")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	workDir := t.TempDir()

	mockScript := filepath.Join(tmpDir, "mock-codex.sh")
	scriptContent := `#!/bin/bash
if [ "$1" != "exec" ]; then
  echo "expected exec subcommand, got $1" >&2
  exit 2
fi
cat > /dev/null
echo '{"type":"thread.started","thread_id":"codex-thread"}'
echo '{"type":"turn.started"}'
echo '{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"Hello from codex mock"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":11,"cached_input_tokens":0,"output_tokens":7}}'
`
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	config := ExecutorConfig{
		Provider:   ProviderCodex,
		CodexPath:  mockScript,
		Timeout:    10 * time.Second,
		MaxRetries: 1,
	}

	exec := NewExecutor(tmpDir, config)
	sess := NewSession("codex-session", "test-task", TypeWorker, workDir)

	var events []*SessionEvent
	var mu sync.Mutex
	handler := func(ev *SessionEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	if err := exec.Start(sess, "test prompt", handler); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	exec.Wait("codex-session")

	if sess.Status != SessionCompleted {
		t.Fatalf("Session status: got %s, want %s", sess.Status, SessionCompleted)
	}
	if sess.Result.TokensInput != 11 || sess.Result.TokensOutput != 7 {
		t.Fatalf("Unexpected token usage: %+v", sess.Result)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) < 3 {
		t.Fatalf("Expected at least 3 events, got %d", len(events))
	}
	var sawMessage bool
	for _, ev := range events {
		if ev.Type == SEventAgentMessage && ev.Text == "Hello from codex mock" {
			sawMessage = true
		}
	}
	if !sawMessage {
		t.Fatalf("Expected codex agent message event, got %+v", events)
	}
}

func TestExecutor_Stop(t *testing.T) {
	tmpDir := t.TempDir()
	taskDir := filepath.Join(tmpDir, "tasks", "test-task", "sessions")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	workDir := t.TempDir()

	// Mock 脚本：长时间运行
	mockScript := filepath.Join(tmpDir, "mock-claude-long.sh")
	scriptContent := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"long-session"}'
sleep 30
echo '{"type":"result","subtype":"success","is_error":false,"session_id":"long-session","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	config := ExecutorConfig{
		ClaudePath: mockScript,
		MaxTurns:   10,
		Timeout:    30 * time.Second,
		MaxRetries: 1,
	}

	exec := NewExecutor(tmpDir, config)
	sess := NewSession("long-session", "test-task", TypeWorker, workDir)

	if err := exec.Start(sess, "test", nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 给进程一点时间启动
	time.Sleep(200 * time.Millisecond)

	if !exec.IsRunning("long-session") {
		t.Fatal("Session should be running")
	}

	// 停止
	if err := exec.Stop("long-session"); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if exec.IsRunning("long-session") {
		t.Error("Session should not be running after Stop")
	}
	if sess.Status != SessionCancelled {
		t.Fatalf("Session status: got %s, want %s", sess.Status, SessionCancelled)
	}
}

func TestExecutor_StopTask(t *testing.T) {
	tmpDir := t.TempDir()
	workDir := t.TempDir()

	mockScript := filepath.Join(tmpDir, "mock-claude-stop-task.sh")
	scriptContent := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"task-session"}'
sleep 30
`
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	exec := NewExecutor(tmpDir, ExecutorConfig{
		ClaudePath: mockScript,
		Timeout:    30 * time.Second,
	})

	sess1 := NewSession("task-session-1", "task-a", TypeWorker, workDir)
	sess2 := NewSession("task-session-2", "task-b", TypeWorker, workDir)
	if err := exec.Start(sess1, "test", nil); err != nil {
		t.Fatalf("Start sess1 failed: %v", err)
	}
	if err := exec.Start(sess2, "test", nil); err != nil {
		t.Fatalf("Start sess2 failed: %v", err)
	}

	time.Sleep(200 * time.Millisecond)

	if err := exec.StopTask("task-a"); err != nil {
		t.Fatalf("StopTask failed: %v", err)
	}
	if sess1.Status != SessionCancelled {
		t.Fatalf("sess1 status: got %s, want %s", sess1.Status, SessionCancelled)
	}
	if !exec.IsRunning("task-session-2") {
		t.Fatal("sess2 should still be running")
	}

	if err := exec.Stop("task-session-2"); err != nil {
		t.Fatalf("Stop sess2 failed: %v", err)
	}
}

func TestExecutor_Timeout(t *testing.T) {
	tmpDir := t.TempDir()
	taskDir := filepath.Join(tmpDir, "tasks", "test-task", "sessions")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	workDir := t.TempDir()

	// Mock 脚本：运行超过 timeout
	mockScript := filepath.Join(tmpDir, "mock-claude-timeout.sh")
	scriptContent := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"timeout-session"}'
sleep 30
`
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	config := ExecutorConfig{
		ClaudePath: mockScript,
		MaxTurns:   10,
		Timeout:    1 * time.Second, // 1 秒超时
		MaxRetries: 1,
	}

	exec := NewExecutor(tmpDir, config)
	sess := NewSession("timeout-session", "test-task", TypeWorker, workDir)

	if err := exec.Start(sess, "test", nil); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 等待超时
	exec.Wait("timeout-session")

	if sess.Status != SessionTimeout {
		t.Errorf("Session status: got %s, want %s", sess.Status, SessionTimeout)
	}
}

func TestExecutor_DuplicateStart(t *testing.T) {
	tmpDir := t.TempDir()
	taskDir := filepath.Join(tmpDir, "tasks", "test-task", "sessions")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatalf("failed to create task dir: %v", err)
	}

	workDir := t.TempDir()

	mockScript := filepath.Join(tmpDir, "mock-claude-dup.sh")
	scriptContent := `#!/bin/bash
cat > /dev/null
sleep 10
`
	if err := os.WriteFile(mockScript, []byte(scriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	config := ExecutorConfig{
		ClaudePath: mockScript,
		Timeout:    30 * time.Second,
	}

	exec := NewExecutor(tmpDir, config)
	sess := NewSession("dup-session", "test-task", TypeWorker, workDir)

	if err := exec.Start(sess, "test", nil); err != nil {
		t.Fatalf("First Start failed: %v", err)
	}
	defer exec.Stop("dup-session")

	time.Sleep(100 * time.Millisecond)

	// 重复启动应该失败
	if err := exec.Start(sess, "test", nil); err == nil {
		t.Fatal("Expected error on duplicate Start")
	}
}

func TestExecutor_StopNonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultExecutorConfig()
	exec := NewExecutor(tmpDir, config)

	if err := exec.Stop("nonexistent"); err == nil {
		t.Fatal("Expected error stopping non-existent session")
	}
}

func TestExecutor_PIDFile(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultExecutorConfig()
	exec := NewExecutor(tmpDir, config)

	// 写入 PID 文件
	exec.writePIDFile("task-1", "session-1", 12345)

	// 读取
	pid, err := exec.ReadPIDFile("task-1", "session-1")
	if err != nil {
		t.Fatalf("ReadPIDFile failed: %v", err)
	}
	if pid != 12345 {
		t.Errorf("PID: got %d, want 12345", pid)
	}

	// 删除
	exec.removePIDFile("task-1", "session-1")

	// 读取不存在的
	_, err = exec.ReadPIDFile("task-1", "session-1")
	if err == nil {
		t.Fatal("Expected error reading removed PID file")
	}
}
