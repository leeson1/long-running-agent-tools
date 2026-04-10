package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/task"
)

func setupWorkerTest(t *testing.T, mockScriptContent string) (*Worker, string, string) {
	t.Helper()

	baseDir := t.TempDir()
	workDir := t.TempDir()

	mockScript := filepath.Join(baseDir, "mock-claude-worker.sh")
	if err := os.WriteFile(mockScript, []byte(mockScriptContent), 0755); err != nil {
		t.Fatalf("failed to write mock script: %v", err)
	}

	taskStore := store.NewTaskStore(baseDir)
	sessionStore := store.NewSessionStore(baseDir)
	logStore := store.NewLogStore(baseDir)

	// 创建任务目录
	tsk := task.NewTask("test-task", "Test", "desc", "default", task.TaskConfig{WorkspaceDir: workDir})
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("failed to create task: %v", err)
	}

	config := session.ExecutorConfig{
		ClaudePath: mockScript,
		MaxTurns:   10,
		Timeout:    15 * time.Second,
		MaxRetries: 1,
	}
	executor := session.NewExecutor(baseDir, config)

	worker := NewWorker(executor, taskStore, sessionStore, logStore)
	return worker, baseDir, workDir
}

func TestWorker_RunSuccess(t *testing.T) {
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"w-test"}'
echo '{"type":"assistant","message":{"id":"msg_01","role":"assistant","content":[{"type":"text","text":"Feature implemented."}],"usage":{"input_tokens":50,"output_tokens":30,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}},"session_id":"w-test"}'
echo '{"type":"result","subtype":"success","is_error":false,"duration_ms":1000,"num_turns":1,"result":"done","stop_reason":"end_turn","total_cost_usd":0.02,"session_id":"w-test","usage":{"input_tokens":50,"output_tokens":30,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	worker, _, workDir := setupWorkerTest(t, mockScript)

	result := worker.Run(WorkerConfig{
		TaskID:          "test-task",
		TaskName:        "Test Project",
		Feature:         task.Feature{ID: "F001", Description: "Setup project", Steps: []string{"Step 1", "Step 2"}},
		BatchNum:        0,
		SessionNumber:   1,
		WorkDir:         workDir,
		ProgressContent: "Initial progress",
		PendingFeatures: "- F002: Next feature",
	})

	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.Error)
	}
	if result.FeatureID != "F001" {
		t.Errorf("FeatureID: got %s, want F001", result.FeatureID)
	}
	if result.Session == nil {
		t.Fatal("Session should not be nil")
	}
}

func TestWorker_RunFailure(t *testing.T) {
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"system","subtype":"init","session_id":"w-fail"}'
echo '{"type":"result","subtype":"error","is_error":true,"result":"crash","session_id":"w-fail","usage":{"input_tokens":10,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
exit 1
`

	worker, _, workDir := setupWorkerTest(t, mockScript)

	result := worker.Run(WorkerConfig{
		TaskID:        "test-task",
		TaskName:      "Test",
		Feature:       task.Feature{ID: "F001", Description: "test"},
		WorkDir:       workDir,
		SessionNumber: 1,
	})

	if result.Success {
		t.Error("Expected failure")
	}
	if result.Error == "" {
		t.Error("Error should not be empty")
	}
}

func TestFormatPendingFeatures(t *testing.T) {
	features := []task.Feature{
		{ID: "F001", Description: "Setup", DependsOn: []string{}},
		{ID: "F002", Description: "Core", DependsOn: []string{"F001"}},
	}

	result := FormatPendingFeatures(features)
	if result == "" {
		t.Error("Result should not be empty")
	}
	if !contains(result, "F001") || !contains(result, "F002") {
		t.Errorf("Result should contain feature IDs: %s", result)
	}
}

func TestFormatPendingFeatures_Empty(t *testing.T) {
	result := FormatPendingFeatures(nil)
	if result != "No pending features." {
		t.Errorf("got %q, want 'No pending features.'", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
