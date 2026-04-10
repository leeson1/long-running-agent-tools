package task

import (
	"sync"
	"testing"
)

func newTestPlanAndFL() (*ExecutionPlan, *FeatureList) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{}},
			{ID: "C", Description: "c", DependsOn: []string{"A", "B"}},
		},
	}

	plan := &ExecutionPlan{
		Batches: []BatchInfo{
			{Batch: 0, Features: []string{"A", "B"}, Status: "pending"},
			{Batch: 1, Features: []string{"C"}, Status: "pending"},
		},
	}

	return plan, fl
}

func TestBatchManager_CurrentBatch(t *testing.T) {
	plan, fl := newTestPlanAndFL()
	bm := NewBatchManager(plan, fl, nil)

	if bm.CurrentBatch() != 0 {
		t.Errorf("CurrentBatch: got %d, want 0", bm.CurrentBatch())
	}
	if bm.TotalBatches() != 2 {
		t.Errorf("TotalBatches: got %d, want 2", bm.TotalBatches())
	}
}

func TestBatchManager_StartCurrentBatch(t *testing.T) {
	plan, fl := newTestPlanAndFL()

	var events []BatchEvent
	var mu sync.Mutex
	listener := func(ev BatchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	bm := NewBatchManager(plan, fl, listener)

	if err := bm.StartCurrentBatch(); err != nil {
		t.Fatalf("StartCurrentBatch failed: %v", err)
	}

	mu.Lock()
	if len(events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(events))
	}
	if events[0].Type != EventBatchStarted {
		t.Errorf("Event type: got %s, want %s", events[0].Type, EventBatchStarted)
	}
	mu.Unlock()

	if plan.Batches[0].Status != string(BatchRunning) {
		t.Errorf("Batch 0 status: got %s, want %s", plan.Batches[0].Status, BatchRunning)
	}
}

func TestBatchManager_GetCurrentBatchFeatures(t *testing.T) {
	plan, fl := newTestPlanAndFL()
	bm := NewBatchManager(plan, fl, nil)

	features, err := bm.GetCurrentBatchFeatures()
	if err != nil {
		t.Fatalf("GetCurrentBatchFeatures failed: %v", err)
	}
	if len(features) != 2 {
		t.Errorf("Expected 2 features, got %d", len(features))
	}
}

func TestBatchManager_MarkFeatureCompleted(t *testing.T) {
	plan, fl := newTestPlanAndFL()

	var events []BatchEvent
	var mu sync.Mutex
	listener := func(ev BatchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	bm := NewBatchManager(plan, fl, listener)

	bm.MarkFeatureCompleted("A")

	if f := fl.GetByID("A"); !f.Passes {
		t.Error("Feature A should be marked as passes")
	}

	mu.Lock()
	if len(events) != 1 || events[0].Type != EventFeatureCompleted {
		t.Errorf("Expected feature_completed event")
	}
	mu.Unlock()
}

func TestBatchManager_TryAdvanceBatch_NotReady(t *testing.T) {
	plan, fl := newTestPlanAndFL()
	bm := NewBatchManager(plan, fl, nil)

	// 没有完成任何 feature，不应该推进
	advanced, allDone := bm.TryAdvanceBatch()
	if advanced {
		t.Error("Should not advance when features are not completed")
	}
	if allDone {
		t.Error("Should not be all done")
	}
}

func TestBatchManager_TryAdvanceBatch_PartialComplete(t *testing.T) {
	plan, fl := newTestPlanAndFL()
	bm := NewBatchManager(plan, fl, nil)

	// 只完成 A，B 还没完成
	bm.MarkFeatureCompleted("A")

	advanced, allDone := bm.TryAdvanceBatch()
	if advanced {
		t.Error("Should not advance when only partial features completed")
	}
	if allDone {
		t.Error("Should not be all done")
	}
}

func TestBatchManager_TryAdvanceBatch_Success(t *testing.T) {
	plan, fl := newTestPlanAndFL()

	var events []BatchEvent
	var mu sync.Mutex
	listener := func(ev BatchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	bm := NewBatchManager(plan, fl, listener)

	// 完成 Batch 0 所有 features
	bm.MarkFeatureCompleted("A")
	bm.MarkFeatureCompleted("B")

	advanced, allDone := bm.TryAdvanceBatch()
	if !advanced {
		t.Error("Should advance after all features completed")
	}
	if allDone {
		t.Error("Should not be all done (batch 1 remaining)")
	}

	if bm.CurrentBatch() != 1 {
		t.Errorf("CurrentBatch: got %d, want 1", bm.CurrentBatch())
	}

	// 检查事件
	mu.Lock()
	hasCompleted := false
	for _, ev := range events {
		if ev.Type == EventBatchCompleted {
			hasCompleted = true
		}
	}
	mu.Unlock()
	if !hasCompleted {
		t.Error("Expected batch_completed event")
	}
}

func TestBatchManager_TryAdvanceBatch_AllDone(t *testing.T) {
	plan, fl := newTestPlanAndFL()

	var events []BatchEvent
	var mu sync.Mutex
	listener := func(ev BatchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	bm := NewBatchManager(plan, fl, listener)

	// 完成 Batch 0
	bm.MarkFeatureCompleted("A")
	bm.MarkFeatureCompleted("B")
	bm.TryAdvanceBatch()

	// 完成 Batch 1
	bm.MarkFeatureCompleted("C")
	advanced, allDone := bm.TryAdvanceBatch()

	if !advanced {
		t.Error("Should advance")
	}
	if !allDone {
		t.Error("Should be all done")
	}
	if !bm.IsAllCompleted() {
		t.Error("IsAllCompleted should return true")
	}

	// 检查 all_completed 事件
	mu.Lock()
	hasAll := false
	for _, ev := range events {
		if ev.Type == EventAllCompleted {
			hasAll = true
		}
	}
	mu.Unlock()
	if !hasAll {
		t.Error("Expected all_completed event")
	}
}

func TestBatchManager_FailCurrentBatch(t *testing.T) {
	plan, fl := newTestPlanAndFL()

	var events []BatchEvent
	var mu sync.Mutex
	listener := func(ev BatchEvent) {
		mu.Lock()
		events = append(events, ev)
		mu.Unlock()
	}

	bm := NewBatchManager(plan, fl, listener)
	bm.FailCurrentBatch("worker crashed")

	if plan.Batches[0].Status != string(BatchFailed) {
		t.Errorf("Batch 0 status: got %s, want %s", plan.Batches[0].Status, BatchFailed)
	}

	mu.Lock()
	if len(events) != 1 || events[0].Type != EventBatchFailed {
		t.Error("Expected batch_failed event")
	}
	mu.Unlock()
}

func TestBatchManager_Progress(t *testing.T) {
	plan, fl := newTestPlanAndFL()
	bm := NewBatchManager(plan, fl, nil)

	p := bm.Progress()
	if p.CurrentBatch != 0 {
		t.Errorf("CurrentBatch: got %d, want 0", p.CurrentBatch)
	}
	if p.TotalBatches != 2 {
		t.Errorf("TotalBatches: got %d, want 2", p.TotalBatches)
	}
	if p.FeaturesTotal != 3 {
		t.Errorf("FeaturesTotal: got %d, want 3", p.FeaturesTotal)
	}
	if p.FeaturesCompleted != 0 {
		t.Errorf("FeaturesCompleted: got %d, want 0", p.FeaturesCompleted)
	}

	bm.MarkFeatureCompleted("A")
	p = bm.Progress()
	if p.FeaturesCompleted != 1 {
		t.Errorf("FeaturesCompleted after A: got %d, want 1", p.FeaturesCompleted)
	}
}

func TestBatchManager_StartAfterAllDone(t *testing.T) {
	plan := &ExecutionPlan{
		Batches: []BatchInfo{
			{Batch: 0, Features: []string{"A"}, Status: "pending"},
		},
	}
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
		},
	}

	bm := NewBatchManager(plan, fl, nil)
	bm.MarkFeatureCompleted("A")
	bm.TryAdvanceBatch()

	// 尝试在全部完成后 start
	if err := bm.StartCurrentBatch(); err == nil {
		t.Error("Expected error when starting after all batches done")
	}

	// 尝试获取 features
	_, err := bm.GetCurrentBatchFeatures()
	if err == nil {
		t.Error("Expected error when getting features after all batches done")
	}
}
