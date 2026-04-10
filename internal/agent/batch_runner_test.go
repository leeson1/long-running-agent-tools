package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/task"
)

// initTestRepo 创建测试用 git 仓库（含初始提交）
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for _, args := range [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s: %v", args, out, err)
		}
	}

	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644)
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s: %v", args, out, err)
		}
	}
	return dir
}

func setupBatchRunnerTest(t *testing.T, mockScriptContent string) (*BatchRunner, string, string) {
	t.Helper()

	baseDir := t.TempDir()
	repoDir := initTestRepo(t)

	mockScript := filepath.Join(baseDir, "mock-claude-batch.sh")
	os.WriteFile(mockScript, []byte(mockScriptContent), 0755)

	taskStore := store.NewTaskStore(baseDir)
	sessionStore := store.NewSessionStore(baseDir)
	logStore := store.NewLogStore(baseDir)

	tsk := task.NewTask("test-task", "Test", "desc", "default", task.TaskConfig{WorkspaceDir: repoDir})
	taskStore.Create(tsk)

	config := session.ExecutorConfig{
		ClaudePath: mockScript,
		MaxTurns:   10,
		Timeout:    15 * time.Second,
		MaxRetries: 1,
	}
	executor := session.NewExecutor(baseDir, config)
	wm := session.NewWorktreeManager(repoDir)

	br := NewBatchRunner(executor, taskStore, sessionStore, logStore, wm, 3)
	return br, baseDir, repoDir
}

func TestBatchRunner_RunSuccess(t *testing.T) {
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"br-test"}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":500,"num_turns":1,"result":"done","stop_reason":"end_turn","total_cost_usd":0.01,"session_id":"br-test","usage":{"input_tokens":20,"output_tokens":10,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	br, _, _ := setupBatchRunnerTest(t, mockScript)

	fl := &task.FeatureList{
		Features: []task.Feature{
			{ID: "F001", Description: "feat 1", DependsOn: []string{}},
			{ID: "F002", Description: "feat 2", DependsOn: []string{}},
		},
	}

	result := br.Run(BatchRunConfig{
		TaskID:      "test-task",
		TaskName:    "Test",
		BatchNum:    0,
		Features:    fl.Features,
		FeatureList: fl,
	})

	if !result.AllSuccess {
		t.Errorf("Expected all success, got failed: %v", result.Failed)
	}
	if len(result.Succeeded) != 2 {
		t.Errorf("Succeeded count: got %d, want 2", len(result.Succeeded))
	}
	if len(result.Results) != 2 {
		t.Errorf("Results count: got %d, want 2", len(result.Results))
	}
}

func TestBatchRunner_RunWithFailure(t *testing.T) {
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"br-fail"}'
echo '{"type":"result","subtype":"error","is_error":true,"result":"fail","session_id":"br-fail","usage":{"input_tokens":5,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
exit 1
`

	br, _, _ := setupBatchRunnerTest(t, mockScript)

	fl := &task.FeatureList{
		Features: []task.Feature{
			{ID: "F001", Description: "feat 1", DependsOn: []string{}},
		},
	}

	result := br.Run(BatchRunConfig{
		TaskID:      "test-task",
		TaskName:    "Test",
		BatchNum:    0,
		Features:    fl.Features,
		FeatureList: fl,
	})

	if result.AllSuccess {
		t.Error("Expected failure")
	}
	if len(result.Failed) != 1 {
		t.Errorf("Failed count: got %d, want 1", len(result.Failed))
	}
}

func TestBatchRunner_EmptyFeatures(t *testing.T) {
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"result","subtype":"success","is_error":false,"session_id":"x","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	br, _, _ := setupBatchRunnerTest(t, mockScript)

	fl := &task.FeatureList{Features: []task.Feature{}}

	result := br.Run(BatchRunConfig{
		TaskID:      "test-task",
		TaskName:    "Test",
		BatchNum:    0,
		Features:    nil,
		FeatureList: fl,
	})

	if !result.AllSuccess {
		t.Error("Empty features should be all success")
	}
}

func TestBatchRunner_MaxParallelOne(t *testing.T) {
	// maxParallel=1 时应该退化为串行
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"serial"}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":100,"num_turns":1,"result":"done","stop_reason":"end_turn","total_cost_usd":0.01,"session_id":"serial","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	baseDir := t.TempDir()
	repoDir := initTestRepo(t)

	mockScriptPath := filepath.Join(baseDir, "mock-serial.sh")
	os.WriteFile(mockScriptPath, []byte(mockScript), 0755)

	taskStore := store.NewTaskStore(baseDir)
	sessionStore := store.NewSessionStore(baseDir)
	logStore := store.NewLogStore(baseDir)

	tsk := task.NewTask("test-task", "Test", "desc", "default", task.TaskConfig{WorkspaceDir: repoDir})
	taskStore.Create(tsk)

	config := session.ExecutorConfig{
		ClaudePath: mockScriptPath,
		MaxTurns:   10,
		Timeout:    15 * time.Second,
	}
	executor := session.NewExecutor(baseDir, config)
	wm := session.NewWorktreeManager(repoDir)

	// maxParallel = 1
	br := NewBatchRunner(executor, taskStore, sessionStore, logStore, wm, 1)

	fl := &task.FeatureList{
		Features: []task.Feature{
			{ID: "F001", Description: "feat 1", DependsOn: []string{}},
			{ID: "F002", Description: "feat 2", DependsOn: []string{}},
		},
	}

	result := br.Run(BatchRunConfig{
		TaskID:      "test-task",
		TaskName:    "Test",
		BatchNum:    0,
		Features:    fl.Features,
		FeatureList: fl,
	})

	if !result.AllSuccess {
		t.Errorf("Expected success, got failed: %v", result.Failed)
	}
	if len(result.Succeeded) != 2 {
		t.Errorf("Succeeded: got %d, want 2", len(result.Succeeded))
	}
}

func TestBatchRunner_CleanupWorktrees(t *testing.T) {
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"result","subtype":"success","is_error":false,"session_id":"x","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	br, _, repoDir := setupBatchRunnerTest(t, mockScript)

	// 创建 worktrees 手动
	wm := session.NewWorktreeManager(repoDir)
	wm.Create("test-task", "F001")
	wm.Create("test-task", "F002")

	// 清理
	br.CleanupWorktrees("test-task", []string{"F001", "F002"})

	// 验证已清理
	wts, _ := wm.List()
	if len(wts) != 0 {
		t.Errorf("Expected 0 worktrees after cleanup, got %d", len(wts))
	}
}
