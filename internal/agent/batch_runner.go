package agent

import (
	"fmt"
	"sync"

	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/task"
)

// BatchRunner Batch 并行执行控制器
// 在给定 Batch 中并行启动多个 Worker，受 maxParallel 限制
type BatchRunner struct {
	executor     *session.Executor
	taskStore    *store.TaskStore
	sessionStore *store.SessionStore
	logStore     *store.LogStore
	worktreeMgr  *session.WorktreeManager
	maxParallel  int
}

// NewBatchRunner 创建 BatchRunner
func NewBatchRunner(
	executor *session.Executor,
	taskStore *store.TaskStore,
	sessionStore *store.SessionStore,
	logStore *store.LogStore,
	worktreeMgr *session.WorktreeManager,
	maxParallel int,
) *BatchRunner {
	if maxParallel <= 0 {
		maxParallel = 1
	}
	return &BatchRunner{
		executor:     executor,
		taskStore:    taskStore,
		sessionStore: sessionStore,
		logStore:     logStore,
		worktreeMgr:  worktreeMgr,
		maxParallel:  maxParallel,
	}
}

// BatchRunConfig Batch 执行配置
type BatchRunConfig struct {
	TaskID          string
	TaskName        string
	BatchNum        int
	Features        []task.Feature
	FeatureList     *task.FeatureList // 完整 feature list（用于获取 pending features）
	ProgressContent string
	ValidatorCmd    string
}

// BatchRunResult Batch 执行结果
type BatchRunResult struct {
	BatchNum   int             `json:"batch_num"`
	Results    []*WorkerResult `json:"results"`
	Succeeded  []string        `json:"succeeded"`  // 成功的 feature IDs
	Failed     []string        `json:"failed"`     // 失败的 feature IDs
	AllSuccess bool            `json:"all_success"` // 是否全部成功
}

// Run 执行一个 Batch 中的所有 features
// 使用 semaphore 控制并发数
func (br *BatchRunner) Run(config BatchRunConfig) *BatchRunResult {
	result := &BatchRunResult{
		BatchNum: config.BatchNum,
	}

	if len(config.Features) == 0 {
		result.AllSuccess = true
		return result
	}

	// 计算 pending features 文本
	pendingText := FormatPendingFeatures(config.FeatureList.PendingFeatures())

	// semaphore 控制并发
	sem := make(chan struct{}, br.maxParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex

	sessionNum := 0
	for _, feature := range config.Features {
		sessionNum++
		wg.Add(1)

		go func(f task.Feature, sn int) {
			defer wg.Done()

			// 获取 semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// 创建 worktree
			wtInfo, err := br.worktreeMgr.Create(config.TaskID, f.ID)
			if err != nil {
				wr := &WorkerResult{
					FeatureID: f.ID,
					Error:     fmt.Sprintf("failed to create worktree: %v", err),
				}
				mu.Lock()
				result.Results = append(result.Results, wr)
				result.Failed = append(result.Failed, f.ID)
				mu.Unlock()
				return
			}

			// 运行 Worker
			worker := NewWorker(br.executor, br.taskStore, br.sessionStore, br.logStore)
			wr := worker.Run(WorkerConfig{
				TaskID:           config.TaskID,
				TaskName:         config.TaskName,
				Feature:          f,
				BatchNum:         config.BatchNum,
				SessionNumber:    sn,
				WorkDir:          wtInfo.Path,
				ProgressContent:  config.ProgressContent,
				PendingFeatures:  pendingText,
				ValidatorCommand: config.ValidatorCmd,
			})

			mu.Lock()
			result.Results = append(result.Results, wr)
			if wr.Success {
				result.Succeeded = append(result.Succeeded, f.ID)
			} else {
				result.Failed = append(result.Failed, f.ID)
			}
			mu.Unlock()

			// 注意：不在这里清理 worktree
			// worktree 保留到合并阶段完成后再清理
		}(feature, sessionNum)
	}

	wg.Wait()

	result.AllSuccess = len(result.Failed) == 0
	return result
}

// CleanupWorktrees 清理 Batch 中所有 features 的 worktrees
func (br *BatchRunner) CleanupWorktrees(taskID string, featureIDs []string) {
	for _, fid := range featureIDs {
		_ = br.worktreeMgr.RemoveWithBranch(taskID, fid)
	}
}
