package task

import (
	"testing"
)

func TestSchedule_LinearChain(t *testing.T) {
	// A → B → C（线性依赖链，应该分成 3 个 Batch）
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "first", DependsOn: []string{}},
			{ID: "B", Description: "second", DependsOn: []string{"A"}},
			{ID: "C", Description: "third", DependsOn: []string{"B"}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(plan.Batches) != 3 {
		t.Fatalf("Expected 3 batches, got %d", len(plan.Batches))
	}

	// Batch 0: A
	assertBatchContains(t, plan.Batches[0], "A")
	// Batch 1: B
	assertBatchContains(t, plan.Batches[1], "B")
	// Batch 2: C
	assertBatchContains(t, plan.Batches[2], "C")
}

func TestSchedule_AllParallel(t *testing.T) {
	// A, B, C 无依赖（全部并行，1 个 Batch）
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{}},
			{ID: "C", Description: "c", DependsOn: []string{}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(plan.Batches) != 1 {
		t.Fatalf("Expected 1 batch, got %d", len(plan.Batches))
	}

	if len(plan.Batches[0].Features) != 3 {
		t.Errorf("Expected 3 features in batch 0, got %d", len(plan.Batches[0].Features))
	}
}

func TestSchedule_DiamondDependency(t *testing.T) {
	// 钻石依赖：A → B, A → C, B → D, C → D
	// 预期：Batch 0: [A], Batch 1: [B, C], Batch 2: [D]
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
			{ID: "C", Description: "c", DependsOn: []string{"A"}},
			{ID: "D", Description: "d", DependsOn: []string{"B", "C"}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(plan.Batches) != 3 {
		t.Fatalf("Expected 3 batches, got %d", len(plan.Batches))
	}

	assertBatchContains(t, plan.Batches[0], "A")
	assertBatchContains(t, plan.Batches[1], "B")
	assertBatchContains(t, plan.Batches[1], "C")
	assertBatchContains(t, plan.Batches[2], "D")
}

func TestSchedule_MixedDependencies(t *testing.T) {
	// 混合：A(独立), B→A, C(独立), D→B, D→C
	// 预期：Batch 0: [A, C], Batch 1: [B], Batch 2: [D]
	// 或者 Batch 0: [A, C], Batch 1: [B, D] 如果 D 只依赖 C
	// D 依赖 B 和 C，所以：Batch 0: [A, C], Batch 1: [B], Batch 2: [D]
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
			{ID: "C", Description: "c", DependsOn: []string{}},
			{ID: "D", Description: "d", DependsOn: []string{"B", "C"}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	// A 和 C 无依赖 → Batch 0
	assertBatchContains(t, plan.Batches[0], "A")
	assertBatchContains(t, plan.Batches[0], "C")

	// B 依赖 A → Batch 1
	assertBatchContains(t, plan.Batches[1], "B")

	// D 依赖 B 和 C → Batch 2
	assertBatchContains(t, plan.Batches[2], "D")
}

func TestSchedule_SingleFeature(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "only one", DependsOn: []string{}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(plan.Batches) != 1 {
		t.Fatalf("Expected 1 batch, got %d", len(plan.Batches))
	}
	if len(plan.Batches[0].Features) != 1 {
		t.Errorf("Expected 1 feature, got %d", len(plan.Batches[0].Features))
	}
}

func TestSchedule_EmptyFeatureList(t *testing.T) {
	fl := &FeatureList{Features: []Feature{}}

	s := NewScheduler()
	_, err := s.Schedule(fl)
	if err == nil {
		t.Fatal("Expected error for empty feature list")
	}
}

func TestSchedule_CircularDependency(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{"B"}},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
		},
	}

	s := NewScheduler()
	_, err := s.Schedule(fl)
	if err == nil {
		t.Fatal("Expected error for circular dependency")
	}
}

func TestSchedule_BatchFieldWriteback(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
		},
	}

	s := NewScheduler()
	_, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	// 验证 Batch 字段已回写
	fA := fl.GetByID("A")
	if fA.Batch == nil || *fA.Batch != 0 {
		t.Errorf("Feature A batch: got %v, want 0", fA.Batch)
	}

	fB := fl.GetByID("B")
	if fB.Batch == nil || *fB.Batch != 1 {
		t.Errorf("Feature B batch: got %v, want 1", fB.Batch)
	}
}

func TestSchedule_BatchStatus(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	for i, batch := range plan.Batches {
		if batch.Status != "pending" {
			t.Errorf("Batch %d status: got %s, want pending", i, batch.Status)
		}
	}
}

func TestSchedule_ComplexGraph(t *testing.T) {
	// 复杂图：
	// F001(独立), F002→F001, F003(独立), F004→F002, F005→F003,
	// F006→F004&F005, F007(独立)
	// Batch 0: [F001, F003, F007]
	// Batch 1: [F002, F005]
	// Batch 2: [F004]
	// Batch 3: [F006]
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", Description: "a", DependsOn: []string{}},
			{ID: "F002", Description: "b", DependsOn: []string{"F001"}},
			{ID: "F003", Description: "c", DependsOn: []string{}},
			{ID: "F004", Description: "d", DependsOn: []string{"F002"}},
			{ID: "F005", Description: "e", DependsOn: []string{"F003"}},
			{ID: "F006", Description: "f", DependsOn: []string{"F004", "F005"}},
			{ID: "F007", Description: "g", DependsOn: []string{}},
		},
	}

	s := NewScheduler()
	plan, err := s.Schedule(fl)
	if err != nil {
		t.Fatalf("Schedule failed: %v", err)
	}

	if len(plan.Batches) != 4 {
		t.Fatalf("Expected 4 batches, got %d", len(plan.Batches))
	}

	assertBatchContains(t, plan.Batches[0], "F001")
	assertBatchContains(t, plan.Batches[0], "F003")
	assertBatchContains(t, plan.Batches[0], "F007")
	assertBatchContains(t, plan.Batches[1], "F002")
	assertBatchContains(t, plan.Batches[1], "F005")
	assertBatchContains(t, plan.Batches[2], "F004")
	assertBatchContains(t, plan.Batches[3], "F006")
}

func TestScheduleRemaining_AllPending(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
		},
	}

	s := NewScheduler()
	plan, err := s.ScheduleRemaining(fl)
	if err != nil {
		t.Fatalf("ScheduleRemaining failed: %v", err)
	}

	if len(plan.Batches) != 2 {
		t.Fatalf("Expected 2 batches, got %d", len(plan.Batches))
	}
}

func TestScheduleRemaining_SomeCompleted(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}, Passes: true},
			{ID: "B", Description: "b", DependsOn: []string{"A"}},
			{ID: "C", Description: "c", DependsOn: []string{"B"}},
		},
	}

	s := NewScheduler()
	plan, err := s.ScheduleRemaining(fl)
	if err != nil {
		t.Fatalf("ScheduleRemaining failed: %v", err)
	}

	// A 已完成，B 的依赖已满足 → Batch 0: [B], Batch 1: [C]
	if len(plan.Batches) != 2 {
		t.Fatalf("Expected 2 batches, got %d", len(plan.Batches))
	}
	assertBatchContains(t, plan.Batches[0], "B")
	assertBatchContains(t, plan.Batches[1], "C")
}

func TestScheduleRemaining_AllCompleted(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}, Passes: true},
			{ID: "B", Description: "b", DependsOn: []string{"A"}, Passes: true},
		},
	}

	s := NewScheduler()
	plan, err := s.ScheduleRemaining(fl)
	if err != nil {
		t.Fatalf("ScheduleRemaining failed: %v", err)
	}

	if plan.Batches != nil {
		t.Errorf("Expected nil batches, got %d", len(plan.Batches))
	}
}

func TestScheduleRemaining_DependencyOnCompleted(t *testing.T) {
	// A(完成), B(完成), C→A&B(未完成), D→C(未完成)
	// C 的依赖 A、B 已完成 → Batch 0: [C], Batch 1: [D]
	fl := &FeatureList{
		Features: []Feature{
			{ID: "A", Description: "a", DependsOn: []string{}, Passes: true},
			{ID: "B", Description: "b", DependsOn: []string{}, Passes: true},
			{ID: "C", Description: "c", DependsOn: []string{"A", "B"}},
			{ID: "D", Description: "d", DependsOn: []string{"C"}},
		},
	}

	s := NewScheduler()
	plan, err := s.ScheduleRemaining(fl)
	if err != nil {
		t.Fatalf("ScheduleRemaining failed: %v", err)
	}

	if len(plan.Batches) != 2 {
		t.Fatalf("Expected 2 batches, got %d", len(plan.Batches))
	}
	assertBatchContains(t, plan.Batches[0], "C")
	assertBatchContains(t, plan.Batches[1], "D")
}

// assertBatchContains 断言 batch 包含指定 feature
func assertBatchContains(t *testing.T, batch BatchInfo, featureID string) {
	t.Helper()
	for _, fid := range batch.Features {
		if fid == featureID {
			return
		}
	}
	t.Errorf("Batch %d should contain feature %s, but got %v", batch.Batch, featureID, batch.Features)
}
