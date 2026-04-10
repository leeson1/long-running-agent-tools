package task

import (
	"fmt"
	"sync"
)

// BatchStatus Batch 状态
type BatchStatus string

const (
	BatchPending   BatchStatus = "pending"
	BatchRunning   BatchStatus = "running"
	BatchCompleted BatchStatus = "completed"
	BatchFailed    BatchStatus = "failed"
)

// BatchManager Batch 执行管理器
// 负责跟踪 Batch 执行进度，控制 Batch 间的串行执行
type BatchManager struct {
	mu           sync.RWMutex
	plan         *ExecutionPlan
	featureList  *FeatureList
	currentBatch int
	listener     BatchEventListener
}

// BatchEvent Batch 事件
type BatchEvent struct {
	Type      BatchEventType `json:"type"`
	BatchNum  int            `json:"batch_num"`
	FeatureID string         `json:"feature_id,omitempty"`
	Message   string         `json:"message,omitempty"`
}

// BatchEventType 事件类型
type BatchEventType string

const (
	EventBatchStarted    BatchEventType = "batch_started"
	EventBatchCompleted  BatchEventType = "batch_completed"
	EventBatchFailed     BatchEventType = "batch_failed"
	EventFeatureStarted  BatchEventType = "feature_started"
	EventFeatureCompleted BatchEventType = "feature_completed"
	EventFeatureFailed   BatchEventType = "feature_failed"
	EventAllCompleted    BatchEventType = "all_completed"
)

// BatchEventListener 事件监听器
type BatchEventListener func(event BatchEvent)

// NewBatchManager 创建 Batch 管理器
func NewBatchManager(plan *ExecutionPlan, fl *FeatureList, listener BatchEventListener) *BatchManager {
	return &BatchManager{
		plan:         plan,
		featureList:  fl,
		currentBatch: 0,
		listener:     listener,
	}
}

// CurrentBatch 返回当前 Batch 编号
func (bm *BatchManager) CurrentBatch() int {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.currentBatch
}

// TotalBatches 返回总 Batch 数
func (bm *BatchManager) TotalBatches() int {
	return bm.plan.TotalBatches()
}

// IsAllCompleted 是否所有 Batch 已完成
func (bm *BatchManager) IsAllCompleted() bool {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.currentBatch >= len(bm.plan.Batches)
}

// GetCurrentBatchFeatures 获取当前 Batch 的 feature IDs
func (bm *BatchManager) GetCurrentBatchFeatures() ([]string, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	if bm.currentBatch >= len(bm.plan.Batches) {
		return nil, fmt.Errorf("all batches completed")
	}

	batch := bm.plan.Batches[bm.currentBatch]
	return batch.Features, nil
}

// StartCurrentBatch 标记当前 Batch 开始执行
func (bm *BatchManager) StartCurrentBatch() error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.currentBatch >= len(bm.plan.Batches) {
		return fmt.Errorf("all batches completed")
	}

	batch := &bm.plan.Batches[bm.currentBatch]
	batch.Status = string(BatchRunning)

	if bm.listener != nil {
		bm.listener(BatchEvent{
			Type:     EventBatchStarted,
			BatchNum: bm.currentBatch,
			Message:  fmt.Sprintf("Batch %d started with %d features", bm.currentBatch, len(batch.Features)),
		})
	}

	return nil
}

// MarkFeatureCompleted 标记某个 feature 完成
func (bm *BatchManager) MarkFeatureCompleted(featureID string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// 更新 FeatureList 中的 passes 字段
	if f := bm.featureList.GetByID(featureID); f != nil {
		f.Passes = true
	}

	if bm.listener != nil {
		bm.listener(BatchEvent{
			Type:      EventFeatureCompleted,
			BatchNum:  bm.currentBatch,
			FeatureID: featureID,
		})
	}
}

// MarkFeatureFailed 标记某个 feature 失败
func (bm *BatchManager) MarkFeatureFailed(featureID string, reason string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.listener != nil {
		bm.listener(BatchEvent{
			Type:      EventFeatureFailed,
			BatchNum:  bm.currentBatch,
			FeatureID: featureID,
			Message:   reason,
		})
	}
}

// TryAdvanceBatch 尝试推进到下一个 Batch
// 当前 Batch 所有 features 完成后才能推进
// 返回：是否推进成功，是否全部完成
func (bm *BatchManager) TryAdvanceBatch() (advanced bool, allDone bool) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.currentBatch >= len(bm.plan.Batches) {
		return false, true
	}

	batch := &bm.plan.Batches[bm.currentBatch]

	// 检查当前 Batch 是否所有 features 都完成了
	allCompleted := true
	anyFailed := false
	for _, fid := range batch.Features {
		f := bm.featureList.GetByID(fid)
		if f == nil || !f.Passes {
			allCompleted = false
			// 这里只是检测未完成，不判断失败
			// 失败由外部 Worker 管理器决定
		}
		_ = anyFailed
	}

	if !allCompleted {
		return false, false
	}

	// 当前 Batch 完成
	batch.Status = string(BatchCompleted)
	if bm.listener != nil {
		bm.listener(BatchEvent{
			Type:     EventBatchCompleted,
			BatchNum: bm.currentBatch,
		})
	}

	// 推进到下一个 Batch
	bm.currentBatch++

	if bm.currentBatch >= len(bm.plan.Batches) {
		if bm.listener != nil {
			bm.listener(BatchEvent{
				Type:    EventAllCompleted,
				Message: "All batches completed",
			})
		}
		return true, true
	}

	return true, false
}

// FailCurrentBatch 标记当前 Batch 失败
func (bm *BatchManager) FailCurrentBatch(reason string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.currentBatch < len(bm.plan.Batches) {
		bm.plan.Batches[bm.currentBatch].Status = string(BatchFailed)
	}

	if bm.listener != nil {
		bm.listener(BatchEvent{
			Type:     EventBatchFailed,
			BatchNum: bm.currentBatch,
			Message:  reason,
		})
	}
}

// GetPlan 返回当前执行计划
func (bm *BatchManager) GetPlan() *ExecutionPlan {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.plan
}

// GetFeatureList 返回当前 FeatureList
func (bm *BatchManager) GetFeatureList() *FeatureList {
	bm.mu.RLock()
	defer bm.mu.RUnlock()
	return bm.featureList
}

// BatchProgress 返回 Batch 进度摘要
type BatchProgress struct {
	CurrentBatch      int     `json:"current_batch"`
	TotalBatches      int     `json:"total_batches"`
	FeaturesCompleted int     `json:"features_completed"`
	FeaturesTotal     int     `json:"features_total"`
	CompletionRate    float64 `json:"completion_rate"`
}

// Progress 返回当前进度
func (bm *BatchManager) Progress() BatchProgress {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return BatchProgress{
		CurrentBatch:      bm.currentBatch,
		TotalBatches:      len(bm.plan.Batches),
		FeaturesCompleted: len(bm.featureList.CompletedFeatures()),
		FeaturesTotal:     len(bm.featureList.Features),
		CompletionRate:    bm.featureList.CompletionRate(),
	}
}
