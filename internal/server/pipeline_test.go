package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/stream"
	"github.com/leeson1/agent-forge/internal/task"
	"github.com/leeson1/agent-forge/internal/template"
)

func initPipelineTestRepo(t *testing.T) string {
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

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0644); err != nil {
		t.Fatalf("write shared.txt: %v", err)
	}
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

func setupPipelineTest(t *testing.T, mockScriptContent string) (*Pipeline, *task.Task, *store.TaskStore, string) {
	t.Helper()

	baseDir := t.TempDir()
	repoDir := initPipelineTestRepo(t)
	mockScript := filepath.Join(baseDir, "mock-claude.sh")
	if err := os.WriteFile(mockScript, []byte(mockScriptContent), 0755); err != nil {
		t.Fatalf("write mock script: %v", err)
	}

	taskStore := store.NewTaskStore(baseDir)
	sessionStore := store.NewSessionStore(baseDir)
	logStore := store.NewLogStore(baseDir)
	eventBus := stream.NewEventBus(128)

	executor := session.NewExecutor(baseDir, session.ExecutorConfig{
		ClaudePath: mockScript,
		MaxTurns:   10,
		Timeout:    20 * time.Second,
		MaxRetries: 1,
	})
	registry, err := template.NewRegistryWithBuiltins()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	pipeline := NewPipeline(executor, taskStore, sessionStore, logStore, eventBus, registry)

	tsk := task.NewTask("task-pipeline", "Pipeline Task", "desc", "default", task.TaskConfig{
		MaxParallelWorkers: 2,
		SessionTimeout:     "20s",
		WorkspaceDir:       repoDir,
	})
	if err := taskStore.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	return pipeline, tsk, taskStore, repoDir
}

func TestPipeline_RunKeepsMergedChangesAndCleansMergedWorktrees(t *testing.T) {
	mockScript := `#!/bin/bash
PROMPT="$(cat)"
if printf '%s' "$PROMPT" | grep -q "Initializer Agent"; then
  cat > "$PWD/feature_list.json" <<'EOF'
{"features":[{"id":"F001","category":"functional","description":"Create feature file","steps":["Write file","Commit"],"depends_on":[],"batch":null,"passes":false}]}
EOF
  cat > "$PWD/init.sh" <<'EOF'
#!/bin/bash
echo init
EOF
  chmod +x "$PWD/init.sh"
  echo "Initialization complete." > "$PWD/progress.txt"
  echo '{"type":"system","subtype":"init","session_id":"init-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"init done","session_id":"init-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

FEATURE_NAME="$(basename "$PWD")"
echo "$FEATURE_NAME" > "$PWD/$FEATURE_NAME.txt"
git add "$PWD/$FEATURE_NAME.txt"
git commit -m "feat: $FEATURE_NAME" >/dev/null
echo '{"type":"system","subtype":"init","session_id":"worker-session"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"worker done","session_id":"worker-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	pipeline, tsk, taskStore, repoDir := setupPipelineTest(t, mockScript)
	pipeline.Run(tsk)

	updated, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if updated.Status != task.StatusCompleted {
		t.Fatalf("Expected completed task, got %s", updated.Status)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "F001.txt")); err != nil {
		t.Fatalf("Expected merged feature file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".worktrees", tsk.ID, "F001")); !os.IsNotExist(err) {
		t.Fatalf("Merged worktree should be removed, stat err=%v", err)
	}

	cmd := exec.Command("git", "branch", "--list", "agentforge/"+tsk.ID+"/F001")
	cmd.Dir = repoDir
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git branch --list failed: %v", err)
	}
	if len(output) != 0 {
		t.Fatalf("Expected merged branch to be deleted, got %q", string(output))
	}
}

func TestPipeline_RunFailsWhenWorkerProducesNoCommit(t *testing.T) {
	mockScript := `#!/bin/bash
PROMPT="$(cat)"
if printf '%s' "$PROMPT" | grep -q "Initializer Agent"; then
  cat > "$PWD/feature_list.json" <<'EOF'
{"features":[{"id":"F001","category":"functional","description":"No-op worker","steps":["Do nothing"],"depends_on":[],"batch":null,"passes":false}]}
EOF
  cat > "$PWD/init.sh" <<'EOF'
#!/bin/bash
echo init
EOF
  chmod +x "$PWD/init.sh"
  echo "Initialization complete." > "$PWD/progress.txt"
  echo '{"type":"system","subtype":"init","session_id":"init-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"init done","session_id":"init-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

echo '{"type":"system","subtype":"init","session_id":"worker-session"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"worker done","session_id":"worker-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	pipeline, tsk, taskStore, repoDir := setupPipelineTest(t, mockScript)
	pipeline.Run(tsk)

	updated, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if updated.Status != task.StatusFailed {
		t.Fatalf("Expected failed task, got %s", updated.Status)
	}

	featureList, err := taskStore.GetFeatureList(tsk.ID)
	if err != nil {
		t.Fatalf("GetFeatureList failed: %v", err)
	}
	if featureList.Features[0].Passes {
		t.Fatal("Feature should not be marked complete")
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".worktrees", tsk.ID, "F001")); err != nil {
		t.Fatalf("Expected failed batch worktree to be preserved: %v", err)
	}
}

func TestPipeline_RunEntersConflictWaitWhenResolverCannotResolve(t *testing.T) {
	mockScript := `#!/bin/bash
PROMPT="$(cat)"
if printf '%s' "$PROMPT" | grep -q "Initializer Agent"; then
  cat > "$PWD/feature_list.json" <<'EOF'
{"features":[
  {"id":"F001","category":"functional","description":"Modify shared file","steps":["Modify file"],"depends_on":[],"batch":null,"passes":false},
  {"id":"F002","category":"functional","description":"Delete shared file","steps":["Delete file"],"depends_on":[],"batch":null,"passes":false}
]}
EOF
  cat > "$PWD/init.sh" <<'EOF'
#!/bin/bash
echo init
EOF
  chmod +x "$PWD/init.sh"
  echo "Initialization complete." > "$PWD/progress.txt"
  echo '{"type":"system","subtype":"init","session_id":"init-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"init done","session_id":"init-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

if printf '%s' "$PROMPT" | grep -q "Git 冲突解决专家"; then
  echo '{"type":"system","subtype":"init","session_id":"resolver-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"resolver did nothing","session_id":"resolver-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

FEATURE_NAME="$(basename "$PWD")"
if [ "$FEATURE_NAME" = "F001" ]; then
  echo "feature-one" > "$PWD/shared.txt"
  git add "$PWD/shared.txt"
  git commit -m "feat: modify shared" >/dev/null
else
  rm "$PWD/shared.txt"
  git add -u "$PWD/shared.txt"
  git commit -m "feat: delete shared" >/dev/null
fi
echo '{"type":"system","subtype":"init","session_id":"worker-session"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"worker done","session_id":"worker-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	pipeline, tsk, taskStore, repoDir := setupPipelineTest(t, mockScript)
	pipeline.Run(tsk)

	updated, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if updated.Status != task.StatusConflictWait {
		t.Fatalf("Expected conflict_wait task, got %s", updated.Status)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".worktrees", tsk.ID, "F001")); err != nil {
		t.Fatalf("Expected worktrees to be preserved on conflict_wait: %v", err)
	}
}

func TestPipeline_RunStopsAfterTaskCancellation(t *testing.T) {
	mockScript := `#!/bin/bash
PROMPT="$(cat)"
if printf '%s' "$PROMPT" | grep -q "Initializer Agent"; then
  cat > "$PWD/feature_list.json" <<'EOF'
{"features":[
  {"id":"F001","category":"functional","description":"Slow feature one","steps":["Wait","Commit"],"depends_on":[],"batch":null,"passes":false},
  {"id":"F002","category":"functional","description":"Slow feature two","steps":["Wait","Commit"],"depends_on":[],"batch":null,"passes":false}
]}
EOF
  cat > "$PWD/init.sh" <<'EOF'
#!/bin/bash
echo init
EOF
  chmod +x "$PWD/init.sh"
  echo "Initialization complete." > "$PWD/progress.txt"
  echo '{"type":"system","subtype":"init","session_id":"init-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"init done","session_id":"init-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

FEATURE_NAME="$(basename "$PWD")"
echo '{"type":"system","subtype":"init","session_id":"worker-session"}'
sleep 30
echo "$FEATURE_NAME" > "$PWD/$FEATURE_NAME.txt"
git add "$PWD/$FEATURE_NAME.txt"
git commit -m "feat: $FEATURE_NAME" >/dev/null
echo '{"type":"result","subtype":"success","is_error":false,"result":"worker done","session_id":"worker-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	pipeline, tsk, taskStore, _ := setupPipelineTest(t, mockScript)
	tsk.Config.MaxParallelWorkers = 1
	if err := taskStore.Update(tsk); err != nil {
		t.Fatalf("Update task failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		pipeline.Run(tsk)
	}()

	deadline := time.Now().Add(10 * time.Second)
	for {
		latest, err := taskStore.Get(tsk.ID)
		if err != nil {
			t.Fatalf("Get task failed: %v", err)
		}
		if latest.Status == task.StatusRunning && pipeline.executor.RunningCount() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker session did not start in time")
		}
		time.Sleep(100 * time.Millisecond)
	}

	latest, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if err := latest.TransitionTo(task.StatusCancelled); err != nil {
		t.Fatalf("TransitionTo(cancelled) failed: %v", err)
	}
	if err := taskStore.Update(latest); err != nil {
		t.Fatalf("Update cancelled task failed: %v", err)
	}
	if err := pipeline.executor.StopTask(tsk.ID); err != nil {
		t.Fatalf("StopTask failed: %v", err)
	}

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pipeline did not stop after cancellation")
	}

	updated, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if updated.Status != task.StatusCancelled {
		t.Fatalf("Expected cancelled task, got %s", updated.Status)
	}

	sessions, err := pipeline.sessionStore.List(tsk.ID)
	if err != nil {
		t.Fatalf("List sessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("Expected initializer + one worker session, got %d sessions", len(sessions))
	}
}

func TestPipeline_RunCanRetryAfterFailedWorktreePreservation(t *testing.T) {
	initialScript := `#!/bin/bash
PROMPT="$(cat)"
if printf '%s' "$PROMPT" | grep -q "Initializer Agent"; then
  cat > "$PWD/feature_list.json" <<'EOF'
{"features":[{"id":"F001","category":"functional","description":"Retry me","steps":["Commit"],"depends_on":[],"batch":null,"passes":false}]}
EOF
  cat > "$PWD/init.sh" <<'EOF'
#!/bin/bash
echo init
EOF
  chmod +x "$PWD/init.sh"
  echo "Initialization complete." > "$PWD/progress.txt"
  echo '{"type":"system","subtype":"init","session_id":"init-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"init done","session_id":"init-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

echo '{"type":"system","subtype":"init","session_id":"worker-session"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"worker done","session_id":"worker-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	pipeline, tsk, taskStore, repoDir := setupPipelineTest(t, initialScript)
	pipeline.Run(tsk)

	updated, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task failed: %v", err)
	}
	if updated.Status != task.StatusFailed {
		t.Fatalf("Expected failed task after first run, got %s", updated.Status)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".worktrees", tsk.ID, "F001")); err != nil {
		t.Fatalf("Expected preserved worktree after failure: %v", err)
	}

	retryScript := filepath.Join(taskStore.PromptsDir(tsk.ID), "..", "..", "..", "mock-claude.sh")
	_ = retryScript
	mockScriptPath := pipeline.executor.Config().ClaudePath
	retryContent := `#!/bin/bash
PROMPT="$(cat)"
if printf '%s' "$PROMPT" | grep -q "Initializer Agent"; then
  cat > "$PWD/feature_list.json" <<'EOF'
{"features":[{"id":"F001","category":"functional","description":"Retry me","steps":["Commit"],"depends_on":[],"batch":null,"passes":false}]}
EOF
  cat > "$PWD/init.sh" <<'EOF'
#!/bin/bash
echo init
EOF
  chmod +x "$PWD/init.sh"
  echo "Initialization complete." > "$PWD/progress.txt"
  echo '{"type":"system","subtype":"init","session_id":"init-session"}'
  echo '{"type":"result","subtype":"success","is_error":false,"result":"init done","session_id":"init-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
  exit 0
fi

FEATURE_NAME="$(basename "$PWD")"
echo "$FEATURE_NAME" > "$PWD/$FEATURE_NAME.txt"
git add "$PWD/$FEATURE_NAME.txt"
git commit -m "feat: $FEATURE_NAME" >/dev/null
echo '{"type":"system","subtype":"init","session_id":"worker-session"}'
echo '{"type":"result","subtype":"success","is_error":false,"result":"worker done","session_id":"worker-session","usage":{"input_tokens":1,"output_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`
	if err := os.WriteFile(mockScriptPath, []byte(retryContent), 0755); err != nil {
		t.Fatalf("rewrite mock script failed: %v", err)
	}

	retryTask, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get task for retry failed: %v", err)
	}
	if err := retryTask.TransitionTo(task.StatusInitializing); err != nil {
		t.Fatalf("TransitionTo(initializing) failed: %v", err)
	}
	if err := taskStore.Update(retryTask); err != nil {
		t.Fatalf("Update retry task failed: %v", err)
	}

	pipeline.Run(retryTask)

	retried, err := taskStore.Get(tsk.ID)
	if err != nil {
		t.Fatalf("Get retried task failed: %v", err)
	}
	if retried.Status != task.StatusCompleted {
		t.Fatalf("Expected completed task after retry, got %s", retried.Status)
	}
	if _, err := os.Stat(filepath.Join(repoDir, "F001.txt")); err != nil {
		t.Fatalf("Expected merged feature file after retry: %v", err)
	}
}
