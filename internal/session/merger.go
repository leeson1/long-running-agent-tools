package session

import (
	"fmt"
	"os/exec"
	"strings"
)

// Merger 分支合并控制器
// 负责将 Worker 分支逐个合并到主分支
type Merger struct {
	repoDir string
}

// NewMerger 创建 Merger
func NewMerger(repoDir string) *Merger {
	return &Merger{repoDir: repoDir}
}

// RepoDir 返回仓库目录
func (m *Merger) RepoDir() string {
	return m.repoDir
}

// GetRemainingConflicts 获取当前剩余的冲突文件
func (m *Merger) GetRemainingConflicts() []string {
	return m.getConflictFiles()
}

// MergeResult 单次合并结果
type MergeResult struct {
	FeatureID      string   `json:"feature_id"`
	Branch         string   `json:"branch"`
	Success        bool     `json:"success"`
	HasConflict    bool     `json:"has_conflict"`
	ConflictFiles  []string `json:"conflict_files,omitempty"`
	ErrorMessage   string   `json:"error_message,omitempty"`
}

// BatchMergeResult 批量合并结果
type BatchMergeResult struct {
	Results    []MergeResult `json:"results"`
	AllSuccess bool          `json:"all_success"`
	Conflicts  []MergeResult `json:"conflicts,omitempty"` // 有冲突的合并结果
}

// MergeBranch 合并指定分支到当前分支
// 返回合并结果，包括是否有冲突及冲突文件列表
func (m *Merger) MergeBranch(branch, featureID string) *MergeResult {
	result := &MergeResult{
		FeatureID: featureID,
		Branch:    branch,
	}

	// 尝试 git merge --no-edit
	cmd := exec.Command("git", "merge", "--no-edit", branch)
	cmd.Dir = m.repoDir
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	if err == nil {
		// 合并成功，无冲突
		result.Success = true
		return result
	}

	// 检查是否是冲突
	if strings.Contains(outputStr, "CONFLICT") || strings.Contains(outputStr, "Merge conflict") {
		result.HasConflict = true
		result.ConflictFiles = m.getConflictFiles()
		return result
	}

	// 其他错误
	result.ErrorMessage = strings.TrimSpace(outputStr)
	return result
}

// MergeBatch 合并一个 Batch 中所有分支
func (m *Merger) MergeBatch(taskID string, featureIDs []string) *BatchMergeResult {
	batchResult := &BatchMergeResult{}

	for _, fid := range featureIDs {
		branch := fmt.Sprintf("agentforge/%s/%s", taskID, fid)
		result := m.MergeBranch(branch, fid)
		batchResult.Results = append(batchResult.Results, *result)

		if result.HasConflict {
			batchResult.Conflicts = append(batchResult.Conflicts, *result)
			// 冲突时中止合并（abort），等待解决后再继续
			m.AbortMerge()
		}
	}

	batchResult.AllSuccess = len(batchResult.Conflicts) == 0
	// 检查是否有非冲突的错误
	for _, r := range batchResult.Results {
		if !r.Success && !r.HasConflict {
			batchResult.AllSuccess = false
			break
		}
	}

	return batchResult
}

// AbortMerge 中止当前合并
func (m *Merger) AbortMerge() error {
	cmd := exec.Command("git", "merge", "--abort")
	cmd.Dir = m.repoDir
	_, err := cmd.CombinedOutput()
	return err
}

// getConflictFiles 获取冲突文件列表
func (m *Merger) getConflictFiles() []string {
	cmd := exec.Command("git", "diff", "--name-only", "--diff-filter=U")
	cmd.Dir = m.repoDir
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var files []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files
}

// AutoResolveConflict Level 1: 尝试自动解决冲突
// 策略：对每个冲突文件，如果是简单的追加冲突，自动选择 "both"
func (m *Merger) AutoResolveConflict(branch, featureID string) *MergeResult {
	result := &MergeResult{
		FeatureID: featureID,
		Branch:    branch,
	}

	// 先尝试 merge
	cmd := exec.Command("git", "merge", "--no-edit", branch)
	cmd.Dir = m.repoDir
	output, err := cmd.CombinedOutput()

	if err == nil {
		result.Success = true
		return result
	}

	if !strings.Contains(string(output), "CONFLICT") {
		result.ErrorMessage = strings.TrimSpace(string(output))
		m.AbortMerge()
		return result
	}

	// 获取冲突文件
	conflictFiles := m.getConflictFiles()
	result.ConflictFiles = conflictFiles

	if len(conflictFiles) == 0 {
		m.AbortMerge()
		result.ErrorMessage = "merge failed but no conflict files detected"
		return result
	}

	// 尝试对每个文件使用 theirs 策略解决（保留 worker 分支的改动）
	allResolved := true
	for _, f := range conflictFiles {
		if !m.tryAutoResolveFile(f) {
			allResolved = false
			break
		}
	}

	if allResolved {
		// 所有冲突已解决，提交
		if err := m.commitMerge(fmt.Sprintf("Auto-resolve merge conflict for %s", featureID)); err != nil {
			result.ErrorMessage = fmt.Sprintf("auto-resolve commit failed: %v", err)
			m.AbortMerge()
			return result
		}
		result.Success = true
		result.HasConflict = true // 标记曾有冲突但已自动解决
		return result
	}

	// 自动解决失败
	result.HasConflict = true
	m.AbortMerge()
	return result
}

// tryAutoResolveFile 尝试自动解决单个文件的冲突
// 使用 checkout --theirs 策略（保留 worker 分支版本）
func (m *Merger) tryAutoResolveFile(filename string) bool {
	// 使用 git checkout --theirs 接受 worker 分支的版本
	cmd := exec.Command("git", "checkout", "--theirs", filename)
	cmd.Dir = m.repoDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return false
	}

	// git add 标记已解决
	cmd = exec.Command("git", "add", filename)
	cmd.Dir = m.repoDir
	if _, err := cmd.CombinedOutput(); err != nil {
		return false
	}

	return true
}

// commitMerge 提交合并结果
func (m *Merger) commitMerge(message string) error {
	cmd := exec.Command("git", "commit", "--no-edit", "-m", message)
	cmd.Dir = m.repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("commit failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// ConflictDetail 冲突详情（供 UI 展示和 Resolver Agent 使用）
type ConflictDetail struct {
	FeatureID     string   `json:"feature_id"`
	Branch        string   `json:"branch"`
	ConflictFiles []string `json:"conflict_files"`
	FileDiffs     map[string]string `json:"file_diffs"` // 文件名 -> diff 内容
}

// GetConflictDetail 获取冲突详情
func (m *Merger) GetConflictDetail(branch, featureID string) (*ConflictDetail, error) {
	detail := &ConflictDetail{
		FeatureID: featureID,
		Branch:    branch,
		FileDiffs: make(map[string]string),
	}

	// 先尝试 merge 制造冲突状态
	cmd := exec.Command("git", "merge", "--no-edit", branch)
	cmd.Dir = m.repoDir
	cmd.CombinedOutput()

	// 获取冲突文件
	detail.ConflictFiles = m.getConflictFiles()

	// 获取每个冲突文件的 diff
	for _, f := range detail.ConflictFiles {
		diffCmd := exec.Command("git", "diff", f)
		diffCmd.Dir = m.repoDir
		output, _ := diffCmd.Output()
		detail.FileDiffs[f] = string(output)
	}

	// Abort 恢复
	m.AbortMerge()

	return detail, nil
}
