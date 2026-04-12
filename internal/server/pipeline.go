package server

import (
	"fmt"
	"log"

	"github.com/leeson1/agent-forge/internal/agent"
	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/stream"
	"github.com/leeson1/agent-forge/internal/task"
)

// Pipeline 任务执行管线
// Initializer → Scheduler → BatchManager → BatchRunner
type Pipeline struct {
	executor     *session.Executor
	taskStore    *store.TaskStore
	sessionStore *store.SessionStore
	logStore     *store.LogStore
	eventBus     *stream.EventBus
}

// NewPipeline 创建执行管线
func NewPipeline(
	executor *session.Executor,
	taskStore *store.TaskStore,
	sessionStore *store.SessionStore,
	logStore *store.LogStore,
	eventBus *stream.EventBus,
) *Pipeline {
	return &Pipeline{
		executor:     executor,
		taskStore:    taskStore,
		sessionStore: sessionStore,
		logStore:     logStore,
		eventBus:     eventBus,
	}
}

// Run 执行完整管线（阻塞，应在 goroutine 中调用）
func (p *Pipeline) Run(t *task.Task) {
	taskID := t.ID

	p.broadcast(taskID, stream.EventTaskStatus, map[string]string{
		"status":  string(t.Status),
		"message": "Pipeline started",
	})

	// ===== Phase 1: Initializer =====
	p.broadcastSystem(taskID, "🚀 Starting Initializer Agent...")

	initializer := agent.NewInitializer(p.executor, p.taskStore, p.sessionStore, p.logStore)
	initResult, err := initializer.Run(t)
	if err != nil {
		p.failTask(t, fmt.Sprintf("Initializer failed: %v", err))
		return
	}

	p.broadcastSystem(taskID, fmt.Sprintf("✅ Initializer completed. Found %d features.", len(initResult.FeatureList.Features)))

	// ===== Phase 2: Scheduler (Topological Sort) =====
	p.broadcastSystem(taskID, "📋 Scheduling features (topological sort)...")

	scheduler := task.NewScheduler()
	plan, err := scheduler.Schedule(initResult.FeatureList)
	if err != nil {
		p.failTask(t, fmt.Sprintf("Scheduler failed: %v", err))
		return
	}

	// 保存执行计划
	if err := p.taskStore.SaveExecutionPlan(taskID, plan); err != nil {
		log.Printf("[pipeline] warning: failed to save execution plan: %v", err)
	}

	// 更新任务进度
	t.Progress.TotalBatches = len(plan.Batches)
	_ = t.TransitionTo(task.StatusRunning)
	_ = p.taskStore.Update(t)

	p.broadcast(taskID, stream.EventTaskStatus, map[string]string{
		"status": string(t.Status),
	})

	p.broadcastSystem(taskID, fmt.Sprintf("📦 Scheduled %d batches. Starting execution...", len(plan.Batches)))

	// ===== Phase 3: Batch Execution =====
	batchMgr := task.NewBatchManager(plan, initResult.FeatureList, func(event task.BatchEvent) {
		p.broadcast(taskID, stream.EventFeatureUpdate, map[string]interface{}{
			"type":       string(event.Type),
			"batch_num":  event.BatchNum,
			"feature_id": event.FeatureID,
			"message":    event.Message,
		})
	})

	worktreeMgr := session.NewWorktreeManager(t.Config.WorkspaceDir)
	batchRunner := agent.NewBatchRunner(
		p.executor, p.taskStore, p.sessionStore, p.logStore,
		worktreeMgr, t.Config.MaxParallelWorkers,
	)

	for !batchMgr.IsAllCompleted() {
		batchNum := batchMgr.CurrentBatch()
		featureIDs, err := batchMgr.GetCurrentBatchFeatures()
		if err != nil {
			p.failTask(t, fmt.Sprintf("Failed to get batch features: %v", err))
			return
		}

		if err := batchMgr.StartCurrentBatch(); err != nil {
			p.failTask(t, fmt.Sprintf("Failed to start batch: %v", err))
			return
		}

		// 获取 Feature 对象
		features := make([]task.Feature, 0, len(featureIDs))
		for _, fid := range featureIDs {
			if f := initResult.FeatureList.GetByID(fid); f != nil {
				features = append(features, *f)
			}
		}

		p.broadcastSystem(taskID, fmt.Sprintf("🔨 Batch %d: Running %d features in parallel...", batchNum+1, len(features)))

		// 更新进度
		t.Progress.CurrentBatch = batchNum + 1
		_ = p.taskStore.Update(t)

		p.broadcast(taskID, stream.EventBatchUpdate, map[string]interface{}{
			"batch_num": batchNum,
			"status":    "running",
			"features":  featureIDs,
		})

		// 执行 Batch
		batchResult := batchRunner.Run(agent.BatchRunConfig{
			TaskID:      taskID,
			TaskName:    t.Name,
			BatchNum:    batchNum,
			Features:    features,
			FeatureList: initResult.FeatureList,
		})

		// 更新 BatchManager
		for _, fid := range batchResult.Succeeded {
			batchMgr.MarkFeatureCompleted(fid)
			t.Progress.FeaturesCompleted++
		}
		for _, fid := range batchResult.Failed {
			batchMgr.MarkFeatureFailed(fid, "worker execution failed")
		}

		t.Progress.TotalSessions += len(batchResult.Results)
		for _, wr := range batchResult.Results {
			if wr.Session != nil {
				t.Progress.TotalTokens += wr.Session.Result.TokensInput + wr.Session.Result.TokensOutput
			}
		}
		_ = p.taskStore.Update(t)

		p.broadcast(taskID, stream.EventBatchUpdate, map[string]interface{}{
			"batch_num": batchNum,
			"status":    "completed",
			"succeeded": batchResult.Succeeded,
			"failed":    batchResult.Failed,
		})

		if !batchResult.AllSuccess {
			p.broadcastSystem(taskID, fmt.Sprintf("⚠️ Batch %d: %d succeeded, %d failed", batchNum+1, len(batchResult.Succeeded), len(batchResult.Failed)))
		} else {
			p.broadcastSystem(taskID, fmt.Sprintf("✅ Batch %d completed successfully", batchNum+1))
		}

		// 清理 worktrees
		allFeatureIDs := append(batchResult.Succeeded, batchResult.Failed...)
		batchRunner.CleanupWorktrees(taskID, allFeatureIDs)

		// 推进到下一个 Batch
		advanced, allDone := batchMgr.TryAdvanceBatch()
		if allDone {
			break
		}
		if !advanced {
			p.failTask(t, fmt.Sprintf("Batch %d has unresolved failures, cannot advance", batchNum+1))
			return
		}
	}

	// ===== Complete =====
	_ = t.TransitionTo(task.StatusCompleted)
	_ = p.taskStore.Update(t)

	progress := batchMgr.Progress()
	p.broadcastSystem(taskID, fmt.Sprintf(
		"🎉 Task completed! %d/%d features done across %d batches.",
		progress.FeaturesCompleted, progress.FeaturesTotal, progress.TotalBatches,
	))
	p.broadcast(taskID, stream.EventTaskStatus, map[string]string{
		"status": string(t.Status),
	})
}

// failTask 标记任务失败
func (p *Pipeline) failTask(t *task.Task, message string) {
	log.Printf("[pipeline] task %s failed: %s", t.ID, message)
	_ = t.TransitionTo(task.StatusFailed)
	_ = p.taskStore.Update(t)

	p.broadcastSystem(t.ID, "❌ "+message)
	p.broadcast(t.ID, stream.EventTaskStatus, map[string]string{
		"status":  string(t.Status),
		"message": message,
	})
}

// broadcast 发布事件
func (p *Pipeline) broadcast(taskID string, eventType stream.EventType, data interface{}) {
	p.eventBus.Publish(stream.NewEvent(eventType, taskID, data))
}

// broadcastSystem 发布系统消息到对话流
func (p *Pipeline) broadcastSystem(taskID string, content string) {
	p.eventBus.Publish(stream.NewEvent(stream.EventAgentMessage, taskID, map[string]string{
		"content": content,
		"role":    "system",
	}))
}

