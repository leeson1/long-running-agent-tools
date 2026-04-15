package server

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/leeson1/agent-forge/internal/agent"
	"github.com/leeson1/agent-forge/internal/session"
	"github.com/leeson1/agent-forge/internal/store"
	"github.com/leeson1/agent-forge/internal/stream"
	"github.com/leeson1/agent-forge/internal/task"
	"github.com/leeson1/agent-forge/internal/template"
)

// Pipeline 任务执行管线
// Initializer → Scheduler → Batch workers → Merge / Resolve / Validate
type Pipeline struct {
	executor         *session.Executor
	taskStore        *store.TaskStore
	sessionStore     *store.SessionStore
	logStore         *store.LogStore
	eventBus         *stream.EventBus
	templateRegistry *template.Registry
}

// NewPipeline 创建执行管线
func NewPipeline(
	executor *session.Executor,
	taskStore *store.TaskStore,
	sessionStore *store.SessionStore,
	logStore *store.LogStore,
	eventBus *stream.EventBus,
	templateRegistry *template.Registry,
) *Pipeline {
	return &Pipeline{
		executor:         executor,
		taskStore:        taskStore,
		sessionStore:     sessionStore,
		logStore:         logStore,
		eventBus:         eventBus,
		templateRegistry: templateRegistry,
	}
}

// Run 执行完整管线（阻塞，应在 goroutine 中调用）
func (p *Pipeline) Run(t *task.Task) {
	taskID := t.ID
	tmpl := p.resolveTemplate(t.Template)

	if tmpl.Config.ID != t.Template && t.Template != "" && t.Template != "default" {
		p.broadcastSystem(taskID, fmt.Sprintf("ℹ️ Template %q not found. Falling back to %q.", t.Template, tmpl.Config.ID))
	}

	p.broadcast(taskID, stream.EventTaskStatus, map[string]string{
		"status":  string(t.Status),
		"message": "Pipeline started",
	})

	if _, err := session.HeadCommit(t.Config.WorkspaceDir); err != nil {
		p.failTask(t, fmt.Sprintf("Workspace must be an existing git repository with at least one commit before starting agents: %v", err))
		return
	}

	p.broadcastSystem(taskID, "🚀 Starting Initializer Agent...")
	initializer := agent.NewInitializer(p.executor, p.taskStore, p.sessionStore, p.logStore)
	initializer.OnEvent = p.makeEventCallback(taskID)
	initResult, err := initializer.Run(t, tmpl)
	if err != nil {
		p.failTask(t, fmt.Sprintf("Initializer failed: %v", err))
		return
	}

	featureList := initResult.FeatureList
	p.broadcastSystem(taskID, fmt.Sprintf("✅ Initializer completed. Found %d features.", len(featureList.Features)))

	p.broadcastSystem(taskID, "📋 Scheduling features (topological sort)...")
	scheduler := task.NewScheduler()
	plan, err := scheduler.Schedule(featureList)
	if err != nil {
		p.failTask(t, fmt.Sprintf("Scheduler failed: %v", err))
		return
	}
	if err := p.taskStore.SaveExecutionPlan(taskID, plan); err != nil {
		log.Printf("[pipeline] warning: failed to save execution plan: %v", err)
	}

	t.Progress.TotalBatches = len(plan.Batches)
	t.Progress.FeaturesTotal = len(featureList.Features)
	progressContent := p.buildProgressContent(t, featureList, "Initialization complete. Ready for development.")
	if err := p.persistCanonicalState(t, featureList, progressContent); err != nil {
		p.failTask(t, fmt.Sprintf("Failed to persist initial task state: %v", err))
		return
	}

	if err := p.transitionTask(t, task.StatusRunning, "📦 Scheduled batches. Starting execution..."); err != nil {
		p.failTask(t, fmt.Sprintf("Failed to transition task to running: %v", err))
		return
	}

	batchMgr := task.NewBatchManager(plan, featureList, func(event task.BatchEvent) {
		p.broadcast(taskID, stream.EventFeatureUpdate, map[string]interface{}{
			"type":       string(event.Type),
			"batch_num":  event.BatchNum,
			"feature_id": event.FeatureID,
			"message":    event.Message,
		})
	})

	worktreeMgr := session.NewWorktreeManager(t.Config.WorkspaceDir)
	merger := session.NewMerger(t.Config.WorkspaceDir)
	resolver := agent.NewResolver(p.executor, p.taskStore, p.sessionStore, p.logStore, merger, 2)

	batchRunner := agent.NewBatchRunner(
		p.executor, p.taskStore, p.sessionStore, p.logStore,
		worktreeMgr, t.Config.MaxParallelWorkers,
	)
	batchRunner.OnEvent = p.makeEventCallback(taskID)

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

		progressContent, _ = p.taskStore.GetProgress(taskID)
		features := make([]task.Feature, 0, len(featureIDs))
		for _, fid := range featureIDs {
			if f := featureList.GetByID(fid); f != nil {
				features = append(features, *f)
			}
		}

		t.Progress.CurrentBatch = batchNum + 1
		if err := p.taskStore.Update(t); err != nil {
			p.failTask(t, fmt.Sprintf("Failed to update task progress: %v", err))
			return
		}

		p.broadcastSystem(taskID, fmt.Sprintf("🔨 Batch %d: Running %d features in parallel...", batchNum+1, len(features)))
		p.broadcast(taskID, stream.EventBatchUpdate, map[string]interface{}{
			"batch_num": batchNum,
			"status":    "running",
			"features":  featureIDs,
		})

		batchResult := batchRunner.Run(agent.BatchRunConfig{
			TaskID:          taskID,
			TaskName:        t.Name,
			BatchNum:        batchNum,
			Features:        features,
			FeatureList:     featureList,
			ProgressContent: progressContent,
			ValidatorCmd:    p.validatorCommandLabel(tmpl),
			Template:        tmpl,
		})

		t.Progress.TotalSessions += len(batchResult.Results)
		for _, wr := range batchResult.Results {
			if wr.Session != nil {
				t.Progress.TotalTokens += wr.Session.Result.TokensInput + wr.Session.Result.TokensOutput
			}
		}
		if err := p.taskStore.Update(t); err != nil {
			p.failTask(t, fmt.Sprintf("Failed to persist session stats: %v", err))
			return
		}

		if !batchResult.AllSuccess {
			batchMgr.FailCurrentBatch("worker validation failed")
			p.broadcast(taskID, stream.EventBatchUpdate, map[string]interface{}{
				"batch_num": batchNum,
				"status":    "failed",
				"succeeded": batchResult.Succeeded,
				"failed":    batchResult.Failed,
			})
			p.failTask(t, fmt.Sprintf("Batch %d failed before merge. Preserving worktrees for inspection.", batchNum+1))
			return
		}

		if err := p.transitionTask(t, task.StatusMerging, fmt.Sprintf("🔀 Merging Batch %d into the main workspace...", batchNum+1)); err != nil {
			p.failTask(t, fmt.Sprintf("Failed to enter merging state: %v", err))
			return
		}

		mergedIDs, err := p.mergeBatch(t, tmpl, batchNum, featureIDs, batchResult.Results, merger, resolver)
		if err != nil {
			if t.Status == task.StatusConflictWait {
				batchMgr.FailCurrentBatch(err.Error())
				return
			}
			batchMgr.FailCurrentBatch(err.Error())
			p.failTask(t, fmt.Sprintf("Batch %d merge failed: %v", batchNum+1, err))
			return
		}

		if err := p.transitionTask(t, task.StatusValidating, fmt.Sprintf("🧪 Validating merged result for Batch %d...", batchNum+1)); err != nil {
			p.failTask(t, fmt.Sprintf("Failed to enter validating state: %v", err))
			return
		}
		if err := p.runMainValidator(t, tmpl, batchNum); err != nil {
			batchMgr.FailCurrentBatch(err.Error())
			p.failTask(t, fmt.Sprintf("Batch %d validation failed after merge: %v", batchNum+1, err))
			return
		}

		for _, fid := range mergedIDs {
			batchMgr.MarkFeatureCompleted(fid)
		}
		t.Progress.FeaturesCompleted = len(featureList.CompletedFeatures())
		progressContent = p.buildProgressContent(t, featureList, fmt.Sprintf("Batch %d merged and validated successfully.", batchNum+1))
		if err := p.persistCanonicalState(t, featureList, progressContent); err != nil {
			batchMgr.FailCurrentBatch(err.Error())
			p.failTask(t, fmt.Sprintf("Failed to persist canonical task state: %v", err))
			return
		}

		p.broadcast(taskID, stream.EventBatchUpdate, map[string]interface{}{
			"batch_num": batchNum,
			"status":    "completed",
			"succeeded": mergedIDs,
			"failed":    []string{},
		})
		p.broadcastSystem(taskID, fmt.Sprintf("✅ Batch %d completed successfully", batchNum+1))
		batchRunner.CleanupWorktrees(taskID, mergedIDs)

		advanced, allDone := batchMgr.TryAdvanceBatch()
		if allDone {
			break
		}
		if !advanced {
			p.failTask(t, fmt.Sprintf("Batch %d did not fully complete", batchNum+1))
			return
		}
		if err := p.transitionTask(t, task.StatusRunning, fmt.Sprintf("➡️ Moving to Batch %d...", batchMgr.CurrentBatch()+1)); err != nil {
			p.failTask(t, fmt.Sprintf("Failed to resume running state: %v", err))
			return
		}
	}

	if err := p.transitionTask(t, task.StatusCompleted, "🎉 Task completed successfully."); err != nil {
		p.failTask(t, fmt.Sprintf("Failed to mark task complete: %v", err))
		return
	}
	progressContent = p.buildProgressContent(t, featureList, "Task completed successfully.")
	if err := p.persistCanonicalState(t, featureList, progressContent); err != nil {
		p.failTask(t, fmt.Sprintf("Failed to persist final task state: %v", err))
		return
	}
}

func (p *Pipeline) mergeBatch(
	t *task.Task,
	tmpl *template.Template,
	batchNum int,
	orderedFeatureIDs []string,
	results []*agent.WorkerResult,
	merger *session.Merger,
	resolver *agent.Resolver,
) ([]string, error) {
	resultByFeature := make(map[string]*agent.WorkerResult, len(results))
	for _, result := range results {
		resultByFeature[result.FeatureID] = result
	}

	mergedIDs := make([]string, 0, len(orderedFeatureIDs))
	for _, fid := range orderedFeatureIDs {
		result := resultByFeature[fid]
		if result == nil {
			return mergedIDs, fmt.Errorf("missing worker result for feature %s", fid)
		}

		mergeResult := merger.MergeBranch(result.Branch, fid)
		if mergeResult.Success {
			mergedIDs = append(mergedIDs, fid)
			continue
		}
		if !mergeResult.HasConflict {
			return mergedIDs, fmt.Errorf("merge of %s failed: %s", fid, mergeResult.ErrorMessage)
		}

		p.broadcast(t.ID, stream.EventMergeConflict, map[string]interface{}{
			"feature_id": fid,
			"files":      mergeResult.ConflictFiles,
			"batch_num":  batchNum,
		})
		if t.Status == task.StatusMerging {
			if err := p.transitionTask(t, task.StatusAutoResolving, fmt.Sprintf("⚠️ Merge conflict detected for %s. Trying auto-resolve...", fid)); err != nil {
				return mergedIDs, err
			}
		} else {
			p.broadcastSystem(t.ID, fmt.Sprintf("⚠️ Merge conflict detected for %s. Trying auto-resolve...", fid))
		}

		autoResult := merger.AutoResolveConflict(result.Branch, fid)
		if autoResult.Success {
			mergedIDs = append(mergedIDs, fid)
			continue
		}

		if t.Status == task.StatusAutoResolving {
			if err := p.transitionTask(t, task.StatusAgentResolving, fmt.Sprintf("🤖 Auto-resolve failed for %s. Starting resolver agent...", fid)); err != nil {
				return mergedIDs, err
			}
		} else {
			p.broadcastSystem(t.ID, fmt.Sprintf("🤖 Auto-resolve failed for %s. Starting resolver agent...", fid))
		}
		detail, err := merger.GetConflictDetail(result.Branch, fid)
		if err != nil {
			return mergedIDs, fmt.Errorf("failed to get conflict detail for %s: %w", fid, err)
		}
		resolveResult := resolver.Resolve(agent.ResolveConfig{
			TaskID:           t.ID,
			TaskName:         t.Name,
			Feature:          task.Feature{ID: fid},
			Branch:           result.Branch,
			ConflictFiles:    detail.ConflictFiles,
			ConflictDiffs:    detail.FileDiffs,
			ValidatorCommand: p.validatorCommandLabel(tmpl),
		})
		if resolveResult.Success {
			mergedIDs = append(mergedIDs, fid)
			continue
		}

		if err := merger.AbortMerge(); err != nil {
			log.Printf("[pipeline] warning: failed to abort unresolved merge: %v", err)
		}
		if err := p.transitionTask(t, task.StatusConflictWait, fmt.Sprintf("🛑 Resolver could not fix the merge conflict for %s. Waiting for manual intervention.", fid)); err != nil {
			return mergedIDs, err
		}
		return mergedIDs, fmt.Errorf("manual merge intervention required for %s: %s", fid, resolveResult.Error)
	}

	return mergedIDs, nil
}

func (p *Pipeline) runMainValidator(t *task.Task, tmpl *template.Template, batchNum int) error {
	if tmpl == nil {
		return nil
	}

	hookEnv := template.HookEnv{
		TaskID:       t.ID,
		SessionID:    fmt.Sprintf("batch-%d-validation", batchNum),
		WorkspaceDir: t.Config.WorkspaceDir,
		BatchNum:     batchNum,
		Extra:        tmpl.Config.Variables,
	}
	result := template.RunValidator(tmpl, hookEnv)
	if result.Output != "" {
		p.broadcast(t.ID, stream.EventLog, map[string]string{
			"content": "[validator] " + strings.TrimSpace(result.Output),
			"level":   "info",
		})
	}
	if !result.Success {
		return fmt.Errorf("%v", result.Error)
	}
	return nil
}

func (p *Pipeline) buildProgressContent(t *task.Task, featureList *task.FeatureList, statusLine string) string {
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("Task: %s\n", t.Name))
	if t.Description != "" {
		builder.WriteString(fmt.Sprintf("Description: %s\n", t.Description))
	}
	builder.WriteString(fmt.Sprintf("Updated: %s\n", t.UpdatedAt.Format("2006-01-02 15:04:05")))
	builder.WriteString(fmt.Sprintf("Status: %s\n", statusLine))
	builder.WriteString(fmt.Sprintf("Progress: %d/%d features complete\n", len(featureList.CompletedFeatures()), len(featureList.Features)))
	builder.WriteString(fmt.Sprintf("Current Batch: %d/%d\n\n", t.Progress.CurrentBatch, t.Progress.TotalBatches))

	builder.WriteString("Completed Features:\n")
	completed := featureList.CompletedFeatures()
	if len(completed) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, f := range completed {
			builder.WriteString(fmt.Sprintf("- [x] %s: %s\n", f.ID, f.Description))
		}
	}

	builder.WriteString("\nPending Features:\n")
	pending := featureList.PendingFeatures()
	if len(pending) == 0 {
		builder.WriteString("- none\n")
	} else {
		for _, f := range pending {
			builder.WriteString(fmt.Sprintf("- [ ] %s: %s\n", f.ID, f.Description))
		}
	}

	return builder.String()
}

func (p *Pipeline) persistCanonicalState(t *task.Task, featureList *task.FeatureList, progressContent string) error {
	if err := p.taskStore.SaveFeatureList(t.ID, featureList); err != nil {
		return err
	}
	if err := p.taskStore.SaveProgress(t.ID, progressContent); err != nil {
		return err
	}

	featureListPath := filepath.Join(t.Config.WorkspaceDir, "feature_list.json")
	featureListData, err := json.MarshalIndent(featureList, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal feature list: %w", err)
	}
	if err := os.WriteFile(featureListPath, featureListData, 0644); err != nil {
		return fmt.Errorf("write workspace feature_list.json: %w", err)
	}

	progressPath := filepath.Join(t.Config.WorkspaceDir, "progress.txt")
	if err := os.WriteFile(progressPath, []byte(progressContent), 0644); err != nil {
		return fmt.Errorf("write workspace progress.txt: %w", err)
	}

	return nil
}

func (p *Pipeline) resolveTemplate(templateID string) *template.Template {
	if p.templateRegistry == nil {
		return &template.Template{
			Config:            template.TemplateConfig{ID: "default", Name: "Default"},
			InitializerPrompt: template.DefaultInitializerPrompt,
			WorkerPrompt:      template.DefaultWorkerPrompt,
		}
	}
	return p.templateRegistry.GetOrDefault(templateID)
}

func (p *Pipeline) validatorCommandLabel(tmpl *template.Template) string {
	if tmpl == nil || tmpl.Config.Validator == "" {
		return "template validator"
	}
	return tmpl.Config.Validator
}

func (p *Pipeline) transitionTask(t *task.Task, status task.TaskStatus, message string) error {
	if t.Status != status {
		if err := t.TransitionTo(status); err != nil {
			return err
		}
		if err := p.taskStore.Update(t); err != nil {
			return err
		}
		p.broadcast(t.ID, stream.EventTaskStatus, map[string]string{
			"status": string(t.Status),
		})
	}
	if message != "" {
		p.broadcastSystem(t.ID, message)
	}
	return nil
}

// failTask 标记任务失败
func (p *Pipeline) failTask(t *task.Task, message string) {
	log.Printf("[pipeline] task %s failed: %s", t.ID, message)
	if t.Status != task.StatusFailed {
		if err := t.TransitionTo(task.StatusFailed); err == nil {
			_ = p.taskStore.Update(t)
		}
	}

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

// makeEventCallback 创建将 Agent 实时事件广播到 EventBus 的回调
func (p *Pipeline) makeEventCallback(taskID string) agent.SessionEventCallback {
	return func(sessionID string, ev *session.SessionEvent) {
		switch ev.Type {
		case session.SEventInit:
			event := stream.NewEvent(stream.EventSessionStart, taskID, map[string]string{
				"session_id": sessionID,
			})
			event.SessionID = sessionID
			p.eventBus.Publish(event)
		case session.SEventAgentMessage:
			if ev.Text != "" {
				event := stream.NewEvent(stream.EventAgentMessage, taskID, map[string]string{
					"content":    ev.Text,
					"session_id": sessionID,
				})
				event.SessionID = sessionID
				p.eventBus.Publish(event)
				p.eventBus.Publish(stream.NewEvent(stream.EventLog, taskID, map[string]string{
					"content":    ev.Text,
					"session_id": sessionID,
					"level":      "info",
				}))
			}
		case session.SEventToolCall:
			event := stream.NewEvent(stream.EventToolCall, taskID, map[string]string{
				"tool_name":  ev.ToolName,
				"tool_input": ev.ToolInput,
				"session_id": sessionID,
			})
			event.SessionID = sessionID
			p.eventBus.Publish(event)
			p.eventBus.Publish(stream.NewEvent(stream.EventLog, taskID, map[string]string{
				"content":    fmt.Sprintf("[tool] %s %s", ev.ToolName, ev.ToolInput),
				"session_id": sessionID,
				"level":      "info",
			}))
		case session.SEventSystem:
			if ev.Text != "" {
				p.eventBus.Publish(stream.NewEvent(stream.EventLog, taskID, map[string]string{
					"content":    ev.Text,
					"session_id": sessionID,
					"level":      "info",
				}))
			}
		case session.SEventResult, session.SEventError:
			event := stream.NewEvent(stream.EventSessionEnd, taskID, map[string]interface{}{
				"session_id":    sessionID,
				"input_tokens":  ev.InputTokens,
				"output_tokens": ev.OutputTokens,
				"num_turns":     ev.NumTurns,
				"status":        string(ev.Type),
			})
			event.SessionID = sessionID
			p.eventBus.Publish(event)
			if ev.Text != "" {
				p.eventBus.Publish(stream.NewEvent(stream.EventLog, taskID, map[string]string{
					"content":    ev.Text,
					"session_id": sessionID,
					"level":      "info",
				}))
			}
		}
	}
}

// broadcastSystem 发布系统消息到对话流
func (p *Pipeline) broadcastSystem(taskID string, content string) {
	p.eventBus.Publish(stream.NewEvent(stream.EventAgentMessage, taskID, map[string]string{
		"content": content,
		"role":    "system",
	}))
}
