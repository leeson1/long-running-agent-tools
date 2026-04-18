package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager Git Worktree 管理器
// 为每个 Worker 创建独立的 worktree + 分支
type WorktreeManager struct {
	repoDir string // 主仓库目录
}

// NewWorktreeManager 创建 WorktreeManager
func NewWorktreeManager(repoDir string) *WorktreeManager {
	return &WorktreeManager{repoDir: repoDir}
}

// WorktreeInfo worktree 信息
type WorktreeInfo struct {
	Path       string // worktree 路径
	Branch     string // 分支名
	FeatureID  string // 关联的 feature ID
	BaseCommit string // 创建 worktree 时的 HEAD commit
}

// Create 创建 worktree + 新分支
// 分支名格式：agentforge/{taskID}/{featureID}
// worktree 路径：{repoDir}/.worktrees/{taskID}/{featureID}
func (wm *WorktreeManager) Create(taskID, featureID string) (*WorktreeInfo, error) {
	branch := fmt.Sprintf("agentforge/%s/%s", taskID, featureID)
	wtPath := filepath.Join(wm.repoDir, ".worktrees", taskID, featureID)
	baseCommit, err := HeadCommit(wm.repoDir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base commit: %w", err)
	}

	// 确保 .worktrees 目录存在
	if err := os.MkdirAll(filepath.Dir(wtPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees dir: %w", err)
	}

	// 创建 worktree + 新分支（基于当前 HEAD）
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath)
	cmd.Dir = wm.repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git worktree add failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	return &WorktreeInfo{
		Path:       wtPath,
		Branch:     branch,
		FeatureID:  featureID,
		BaseCommit: baseCommit,
	}, nil
}

// Remove 移除 worktree 并删除分支
func (wm *WorktreeManager) Remove(taskID, featureID string) error {
	wtPath := filepath.Join(wm.repoDir, ".worktrees", taskID, featureID)

	// 移除 worktree
	cmd := exec.Command("git", "worktree", "remove", wtPath, "--force")
	cmd.Dir = wm.repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// worktree 可能已经不存在
		if !strings.Contains(string(output), "is not a working tree") {
			return fmt.Errorf("git worktree remove failed: %s: %w", strings.TrimSpace(string(output)), err)
		}
	}

	return nil
}

// RemoveWithBranch 移除 worktree 并删除关联分支
func (wm *WorktreeManager) RemoveWithBranch(taskID, featureID string) error {
	if err := wm.Remove(taskID, featureID); err != nil {
		return err
	}

	// 删除分支
	branch := fmt.Sprintf("agentforge/%s/%s", taskID, featureID)
	cmd := exec.Command("git", "branch", "-D", branch)
	cmd.Dir = wm.repoDir
	_ = cmd.Run() // 忽略错误（分支可能不存在）

	return nil
}

// List 列出所有活跃的 worktrees
func (wm *WorktreeManager) List() ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "worktree", "list", "--porcelain")
	cmd.Dir = wm.repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git worktree list failed: %w", err)
	}

	return parseWorktreeList(string(output), wm.repoDir)
}

// Prune 清理无效的 worktrees
func (wm *WorktreeManager) Prune() error {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = wm.repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree prune failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// ResetTask removes preserved worktrees and branches for a task before a retry.
func (wm *WorktreeManager) ResetTask(taskID string) error {
	if err := wm.Prune(); err != nil {
		return err
	}

	taskDir := filepath.Join(wm.repoDir, ".worktrees", taskID)
	entries, err := os.ReadDir(taskDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read task worktrees: %w", err)
	}

	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if err := wm.RemoveWithBranch(taskID, entry.Name()); err != nil {
			errs = append(errs, err)
		}
	}
	if err := wm.deleteTaskBranches(taskID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func (wm *WorktreeManager) deleteTaskBranches(taskID string) error {
	cmd := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/agentforge/"+taskID)
	cmd.Dir = wm.repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("list task branches failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	var errs []error
	for _, branch := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}
		cmd := exec.Command("git", "branch", "-D", branch)
		cmd.Dir = wm.repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Errorf("delete branch %s failed: %s: %w", branch, strings.TrimSpace(string(out)), err))
		}
	}
	return errors.Join(errs...)
}

// parseWorktreeList 解析 git worktree list --porcelain 输出
func parseWorktreeList(output, repoDir string) ([]WorktreeInfo, error) {
	var worktrees []WorktreeInfo
	// 解析符号链接（macOS 上 /var -> /private/var）
	resolvedRepo, err := filepath.EvalSymlinks(repoDir)
	if err != nil {
		resolvedRepo = repoDir
	}
	wtDir := filepath.Join(resolvedRepo, ".worktrees")

	lines := strings.Split(output, "\n")
	var current WorktreeInfo
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			// 空行分隔不同 worktree
			if current.Path != "" && strings.HasPrefix(current.Path, wtDir) {
				// 提取 featureID（worktree 目录名）
				current.FeatureID = filepath.Base(current.Path)
				worktrees = append(worktrees, current)
			}
			current = WorktreeInfo{}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}

	// 处理最后一个条目
	if current.Path != "" && strings.HasPrefix(current.Path, wtDir) {
		current.FeatureID = filepath.Base(current.Path)
		worktrees = append(worktrees, current)
	}

	return worktrees, nil
}
