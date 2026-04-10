package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// initMergeTestRepo 创建一个用于合并测试的 git 仓库
func initMergeTestRepo(t *testing.T) string {
	t.Helper()
	dir := initTestRepo(t) // 复用 worktree_test.go 的 initTestRepo
	return dir
}

// createBranchWithChange 在 repo 中创建一个分支并修改文件
func createBranchWithChange(t *testing.T, repoDir, branch, filename, content string) {
	t.Helper()

	// 创建并切到新分支
	run(t, repoDir, "git", "checkout", "-b", branch)

	// 写文件
	os.WriteFile(filepath.Join(repoDir, filename), []byte(content), 0644)

	// 提交
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "change on "+branch)

	// 切回 main
	run(t, repoDir, "git", "checkout", "main")
}

// run 执行 git 命令
func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v failed: %s: %v", args, out, err)
	}
}

func TestMerger_MergeBranch_NoConflict(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	// 创建分支：新增一个文件（不会冲突）
	createBranchWithChange(t, repoDir, "agentforge/task-1/F001", "feature1.txt", "feature 1 content\n")

	result := merger.MergeBranch("agentforge/task-1/F001", "F001")
	if !result.Success {
		t.Errorf("Expected success, got error: %s", result.ErrorMessage)
	}
	if result.HasConflict {
		t.Error("Should not have conflict")
	}

	// 验证文件存在
	if _, err := os.Stat(filepath.Join(repoDir, "feature1.txt")); os.IsNotExist(err) {
		t.Error("feature1.txt should exist after merge")
	}
}

func TestMerger_MergeBranch_WithConflict(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	// main 分支修改 README
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Main version\n"), 0644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "main change")

	// 创建 feature 分支（基于旧 main）
	run(t, repoDir, "git", "checkout", "-b", "agentforge/task-1/F001", "HEAD~1")
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Feature version\n"), 0644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "feature change")
	run(t, repoDir, "git", "checkout", "main")

	result := merger.MergeBranch("agentforge/task-1/F001", "F001")
	if result.Success {
		t.Error("Expected conflict")
	}
	if !result.HasConflict {
		t.Error("Should have conflict")
	}
	if len(result.ConflictFiles) == 0 {
		t.Error("Should have conflict files")
	}

	// 清理冲突状态
	merger.AbortMerge()
}

func TestMerger_MergeBatch_AllSuccess(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	// 创建两个不冲突的分支
	createBranchWithChange(t, repoDir, "agentforge/task-1/F001", "f1.txt", "f1\n")
	createBranchWithChange(t, repoDir, "agentforge/task-1/F002", "f2.txt", "f2\n")

	result := merger.MergeBatch("task-1", []string{"F001", "F002"})
	if !result.AllSuccess {
		t.Error("Expected all success")
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("Expected no conflicts, got %d", len(result.Conflicts))
	}
}

func TestMerger_AutoResolveConflict_Success(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	// 在 main 修改 README
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Main\n"), 0644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "main change")

	// 创建 feature 分支（基于旧 main）修改同一文件
	run(t, repoDir, "git", "checkout", "-b", "agentforge/task-1/F001", "HEAD~1")
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Feature\n"), 0644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "feature change")
	run(t, repoDir, "git", "checkout", "main")

	result := merger.AutoResolveConflict("agentforge/task-1/F001", "F001")
	// AutoResolve 使用 --theirs 策略，应该成功
	if !result.Success {
		t.Errorf("AutoResolve should succeed, got error: %s", result.ErrorMessage)
	}
}

func TestMerger_AutoResolveConflict_NoConflict(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	createBranchWithChange(t, repoDir, "agentforge/task-1/F001", "new.txt", "content\n")

	result := merger.AutoResolveConflict("agentforge/task-1/F001", "F001")
	if !result.Success {
		t.Errorf("Should succeed without conflict, got: %s", result.ErrorMessage)
	}
}

func TestMerger_AbortMerge(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	// AbortMerge 在没有活跃合并时也不应该 panic
	err := merger.AbortMerge()
	// 可能报错（没有合并进行中），但不应该 panic
	_ = err
}

func TestMerger_GetConflictDetail(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	// 制造冲突
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Main version\n"), 0644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "main")

	run(t, repoDir, "git", "checkout", "-b", "agentforge/task-1/F001", "HEAD~1")
	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Feature version\n"), 0644)
	run(t, repoDir, "git", "add", ".")
	run(t, repoDir, "git", "commit", "-m", "feature")
	run(t, repoDir, "git", "checkout", "main")

	detail, err := merger.GetConflictDetail("agentforge/task-1/F001", "F001")
	if err != nil {
		t.Fatalf("GetConflictDetail failed: %v", err)
	}

	if len(detail.ConflictFiles) == 0 {
		t.Error("Should have conflict files")
	}
	if detail.FeatureID != "F001" {
		t.Errorf("FeatureID: got %s, want F001", detail.FeatureID)
	}
}

func TestMerger_RepoDir(t *testing.T) {
	merger := NewMerger("/test/path")
	if merger.RepoDir() != "/test/path" {
		t.Errorf("RepoDir: got %s, want /test/path", merger.RepoDir())
	}
}

func TestMerger_GetRemainingConflicts_NoConflict(t *testing.T) {
	repoDir := initMergeTestRepo(t)
	merger := NewMerger(repoDir)

	conflicts := merger.GetRemainingConflicts()
	if len(conflicts) != 0 {
		t.Errorf("Expected no conflicts, got %d", len(conflicts))
	}
}
