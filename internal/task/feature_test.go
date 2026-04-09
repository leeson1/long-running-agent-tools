package task

import "testing"

func intPtr(v int) *int { return &v }

func TestFeatureList_GetByID(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", Description: "Feature 1"},
			{ID: "F002", Description: "Feature 2"},
		},
	}

	f := fl.GetByID("F001")
	if f == nil {
		t.Fatal("Expected to find F001")
	}
	if f.Description != "Feature 1" {
		t.Errorf("Description: got %s, want Feature 1", f.Description)
	}

	if fl.GetByID("F999") != nil {
		t.Error("Expected nil for non-existent ID")
	}
}

func TestFeatureList_PendingAndCompleted(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", Passes: false},
			{ID: "F002", Passes: true},
			{ID: "F003", Passes: false},
			{ID: "F004", Passes: true},
		},
	}

	pending := fl.PendingFeatures()
	if len(pending) != 2 {
		t.Errorf("Pending count: got %d, want 2", len(pending))
	}

	completed := fl.CompletedFeatures()
	if len(completed) != 2 {
		t.Errorf("Completed count: got %d, want 2", len(completed))
	}
}

func TestFeatureList_CompletionRate(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", Passes: true},
			{ID: "F002", Passes: true},
			{ID: "F003", Passes: false},
			{ID: "F004", Passes: false},
		},
	}

	rate := fl.CompletionRate()
	if rate != 0.5 {
		t.Errorf("CompletionRate: got %f, want 0.5", rate)
	}

	// Empty list
	empty := &FeatureList{}
	if empty.CompletionRate() != 0 {
		t.Errorf("Empty CompletionRate: got %f, want 0", empty.CompletionRate())
	}
}

func TestFeatureList_FeaturesInBatch(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", Batch: intPtr(1)},
			{ID: "F002", Batch: intPtr(1)},
			{ID: "F003", Batch: intPtr(2)},
			{ID: "F004", Batch: nil},
		},
	}

	batch1 := fl.FeaturesInBatch(1)
	if len(batch1) != 2 {
		t.Errorf("Batch 1 count: got %d, want 2", len(batch1))
	}

	batch2 := fl.FeaturesInBatch(2)
	if len(batch2) != 1 {
		t.Errorf("Batch 2 count: got %d, want 1", len(batch2))
	}

	batch3 := fl.FeaturesInBatch(3)
	if len(batch3) != 0 {
		t.Errorf("Batch 3 count: got %d, want 0", len(batch3))
	}
}

func TestFeatureList_Validate_Valid(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", DependsOn: []string{}},
			{ID: "F002", DependsOn: []string{}},
			{ID: "F003", DependsOn: []string{"F001", "F002"}},
			{ID: "F004", DependsOn: []string{"F003"}},
		},
	}

	if err := fl.Validate(); err != nil {
		t.Errorf("Validate should pass: %v", err)
	}
}

func TestFeatureList_Validate_EmptyID(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "", DependsOn: []string{}},
		},
	}

	err := fl.Validate()
	if err == nil {
		t.Fatal("Expected validation error for empty ID")
	}
}

func TestFeatureList_Validate_DuplicateID(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", DependsOn: []string{}},
			{ID: "F001", DependsOn: []string{}},
		},
	}

	err := fl.Validate()
	if err == nil {
		t.Fatal("Expected validation error for duplicate ID")
	}
}

func TestFeatureList_Validate_UnknownDependency(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", DependsOn: []string{"F999"}},
		},
	}

	err := fl.Validate()
	if err == nil {
		t.Fatal("Expected validation error for unknown dependency")
	}
}

func TestFeatureList_Validate_CircularDependency(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", DependsOn: []string{"F003"}},
			{ID: "F002", DependsOn: []string{"F001"}},
			{ID: "F003", DependsOn: []string{"F002"}},
		},
	}

	err := fl.Validate()
	if err == nil {
		t.Fatal("Expected validation error for circular dependency")
	}
}

func TestFeatureList_Validate_SelfDependency(t *testing.T) {
	fl := &FeatureList{
		Features: []Feature{
			{ID: "F001", DependsOn: []string{"F001"}},
		},
	}

	err := fl.Validate()
	if err == nil {
		t.Fatal("Expected validation error for self dependency")
	}
}

func TestExecutionPlan_GetBatch(t *testing.T) {
	ep := &ExecutionPlan{
		Batches: []BatchInfo{
			{Batch: 1, Features: []string{"F001"}, Status: "completed"},
			{Batch: 2, Features: []string{"F002", "F003"}, Status: "running"},
		},
	}

	b1 := ep.GetBatch(1)
	if b1 == nil {
		t.Fatal("Expected to find batch 1")
	}
	if b1.Status != "completed" {
		t.Errorf("Batch 1 status: got %s, want completed", b1.Status)
	}

	if ep.GetBatch(99) != nil {
		t.Error("Expected nil for non-existent batch")
	}

	if ep.TotalBatches() != 2 {
		t.Errorf("TotalBatches: got %d, want 2", ep.TotalBatches())
	}
}
