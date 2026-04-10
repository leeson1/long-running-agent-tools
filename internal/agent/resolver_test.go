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

// initResolverTestRepo 创建测试用 git 仓库（含初始提交）
func initResolverTestRepo(t *testing.T) string {
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

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %s: %v", args, out, err)
	}
}

func setupResolverTest(t *testing.T, mockScriptContent string) (*Resolver, *session.Merger, string, string) {
	t.Helper()

	baseDir := t.TempDir()
	repoDir := initResolverTestRepo(t)

	mockScript := filepath.Join(baseDir, "mock-claude-resolver.sh")
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
	}
	executor := session.NewExecutor(baseDir, config)
	merger := session.NewMerger(repoDir)

	resolver := NewResolver(executor, taskStore, sessionStore, logStore, merger, 1)
	return resolver, merger, baseDir, repoDir
}

func TestResolver_Resolve_NoConflict(t *testing.T) {
	// Mock 脚本不会被调用（因为 merge 直接成功）
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"result","subtype":"success","is_error":false,"session_id":"x","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	resolver, _, _, repoDir := setupResolverTest(t, mockScript)

	// 创建一个不冲突的分支
	os.WriteFile(filepath.Join(repoDir, "new.txt"), []byte("new\n"), 0644)
	runGit(t, repoDir, "checkout", "-b", "agentforge/test-task/F001")
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "feature")
	runGit(t, repoDir, "checkout", "main")

	result := resolver.Resolve(ResolveConfig{
		TaskID:   "test-task",
		TaskName: "Test",
		Feature:  task.Feature{ID: "F001"},
		Branch:   "agentforge/test-task/F001",
	})

	if !result.Success {
		t.Errorf("Expected success (no conflict), got error: %s", result.Error)
	}
}

func TestResolver_BuildPrompt(t *testing.T) {
	mockScript := `#!/bin/bash
echo done`

	resolver, _, _, _ := setupResolverTest(t, mockScript)

	prompt := resolver.buildPrompt(ResolveConfig{
		TaskID:        "test-task",
		TaskName:      "My Project",
		Feature:       task.Feature{ID: "F001"},
		Branch:        "agentforge/test-task/F001",
		ConflictFiles: []string{"file1.go", "file2.go"},
		ConflictDiffs: map[string]string{"file1.go": "diff content"},
	})

	if prompt == "" {
		t.Error("Prompt should not be empty")
	}
	if !containsStr(prompt, "My Project") {
		t.Error("Prompt should contain task name")
	}
	if !containsStr(prompt, "F001") {
		t.Error("Prompt should contain feature ID")
	}
}

func TestResolver_ThreeLevelResolve_Level1Success(t *testing.T) {
	// Level 1 auto-resolve should succeed for simple conflicts
	mockScript := `#!/bin/bash
cat > /dev/null
echo '{"type":"result","subtype":"success","is_error":false,"session_id":"x","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}'
`

	resolver, _, _, repoDir := setupResolverTest(t, mockScript)

	// 制造冲突
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Main\n"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "main change")

	runGit(t, repoDir, "checkout", "-b", "agentforge/test-task/F001", "HEAD~1")
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Feature\n"), 0644)
	runGit(t, repoDir, "add", ".")
	runGit(t, repoDir, "commit", "-m", "feature change")
	runGit(t, repoDir, "checkout", "main")

	tsk := task.NewTask("test-task", "Test", "desc", "default", task.TaskConfig{})

	result := resolver.ThreeLevelResolve(tsk, "F001", "agentforge/test-task/F001", []string{"README.md"})
	if !result.Success {
		t.Errorf("ThreeLevelResolve should succeed at Level 1, got: %s", result.Error)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
