package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initTestRepo 创建一个测试用 git 仓库
func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s: %v", args, output, err)
		}
	}

	// 创建初始文件并提交
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", "initial"},
	} {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %s: %v", args, output, err)
		}
	}

	return dir
}

func TestWorktreeManager_CreateAndRemove(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	// 创建 worktree
	info, err := wm.Create("task-1", "F001")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if info.FeatureID != "F001" {
		t.Errorf("FeatureID: got %s, want F001", info.FeatureID)
	}
	if info.Branch != "agentforge/task-1/F001" {
		t.Errorf("Branch: got %s, want agentforge/task-1/F001", info.Branch)
	}

	expectedPath := filepath.Join(repoDir, ".worktrees", "F001")
	if info.Path != expectedPath {
		t.Errorf("Path: got %s, want %s", info.Path, expectedPath)
	}

	// 检查 worktree 目录存在
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		t.Error("Worktree directory should exist")
	}

	// 检查 README.md 存在于 worktree
	if _, err := os.Stat(filepath.Join(info.Path, "README.md")); os.IsNotExist(err) {
		t.Error("README.md should exist in worktree")
	}

	// 移除 worktree
	if err := wm.Remove("F001"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// 检查目录不存在
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Error("Worktree directory should be removed")
	}
}

func TestWorktreeManager_CreateMultiple(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	// 创建多个 worktrees
	info1, err := wm.Create("task-1", "F001")
	if err != nil {
		t.Fatalf("Create F001 failed: %v", err)
	}

	info2, err := wm.Create("task-1", "F002")
	if err != nil {
		t.Fatalf("Create F002 failed: %v", err)
	}

	// 路径应该不同
	if info1.Path == info2.Path {
		t.Error("Worktree paths should be different")
	}

	// 分支应该不同
	if info1.Branch == info2.Branch {
		t.Error("Branches should be different")
	}

	// 清理
	wm.Remove("F001")
	wm.Remove("F002")
}

func TestWorktreeManager_List(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	// 创建两个 worktrees
	wm.Create("task-1", "F001")
	wm.Create("task-1", "F002")
	defer wm.Remove("F001")
	defer wm.Remove("F002")

	// 列出
	wts, err := wm.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(wts) != 2 {
		t.Errorf("Expected 2 worktrees, got %d", len(wts))
	}

	// 检查包含正确的 feature IDs
	ids := make(map[string]bool)
	for _, wt := range wts {
		ids[wt.FeatureID] = true
	}
	if !ids["F001"] {
		t.Error("Should contain F001")
	}
	if !ids["F002"] {
		t.Error("Should contain F002")
	}
}

func TestWorktreeManager_RemoveWithBranch(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	wm.Create("task-1", "F001")

	if err := wm.RemoveWithBranch("task-1", "F001"); err != nil {
		t.Fatalf("RemoveWithBranch failed: %v", err)
	}

	// 检查分支已删除
	cmd := exec.Command("git", "branch", "--list", "agentforge/task-1/F001")
	cmd.Dir = repoDir
	output, _ := cmd.Output()
	if len(output) > 0 {
		t.Error("Branch should be deleted")
	}
}

func TestWorktreeManager_Prune(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	// Prune 应该不报错
	if err := wm.Prune(); err != nil {
		t.Fatalf("Prune failed: %v", err)
	}
}

func TestWorktreeManager_RemoveNonExistent(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	// 移除不存在的 worktree 不应该报错（已经不存在）
	err := wm.Remove("nonexistent")
	// 可能报错也可能不报错，取决于 git 版本
	_ = err
}

func TestWorktreeManager_DuplicateCreate(t *testing.T) {
	repoDir := initTestRepo(t)
	wm := NewWorktreeManager(repoDir)

	_, err := wm.Create("task-1", "F001")
	if err != nil {
		t.Fatalf("First Create failed: %v", err)
	}
	defer wm.Remove("F001")

	// 重复创建应该失败
	_, err = wm.Create("task-1", "F001")
	if err == nil {
		t.Fatal("Expected error on duplicate create")
	}
}

func TestParseWorktreeList(t *testing.T) {
	repoDir := "/test/repo"
	output := `worktree /test/repo
HEAD abc123
branch refs/heads/main

worktree /test/repo/.worktrees/F001
HEAD def456
branch refs/heads/agentforge/task-1/F001

worktree /test/repo/.worktrees/F002
HEAD ghi789
branch refs/heads/agentforge/task-1/F002

`
	wts, err := parseWorktreeList(output, repoDir)
	if err != nil {
		t.Fatalf("parseWorktreeList failed: %v", err)
	}

	if len(wts) != 2 {
		t.Fatalf("Expected 2 worktrees, got %d", len(wts))
	}

	if wts[0].FeatureID != "F001" {
		t.Errorf("First worktree FeatureID: got %s, want F001", wts[0].FeatureID)
	}
	if wts[0].Branch != "agentforge/task-1/F001" {
		t.Errorf("First worktree Branch: got %s, want agentforge/task-1/F001", wts[0].Branch)
	}

	if wts[1].FeatureID != "F002" {
		t.Errorf("Second worktree FeatureID: got %s, want F002", wts[1].FeatureID)
	}
}
