package store

import (
	"os"
	"testing"

	"github.com/leeson1/agent-forge/internal/task"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/tasks", 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	return dir
}

func TestTaskStore_CreateAndGet(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	tk := task.NewTask("test-001", "Test Task", "A test task", "fullstack-web", task.TaskConfig{
		MaxParallelWorkers: 3,
		SessionTimeout:     "30m",
		WorkspaceDir:       "/workspace/test",
	})

	// Create
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Duplicate create should fail
	if err := store.Create(tk); err == nil {
		t.Fatal("expected error on duplicate create")
	}

	// Get
	got, err := store.Get("test-001")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.ID != tk.ID {
		t.Errorf("ID mismatch: got %s, want %s", got.ID, tk.ID)
	}
	if got.Name != tk.Name {
		t.Errorf("Name mismatch: got %s, want %s", got.Name, tk.Name)
	}
	if got.Status != task.StatusPending {
		t.Errorf("Status mismatch: got %s, want %s", got.Status, task.StatusPending)
	}
	if got.Config.MaxParallelWorkers != 3 {
		t.Errorf("MaxParallelWorkers mismatch: got %d, want 3", got.Config.MaxParallelWorkers)
	}
}

func TestTaskStore_Update(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	tk := task.NewTask("test-002", "Update Task", "desc", "cli-tool", task.TaskConfig{})
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update status
	tk.Status = task.StatusRunning
	tk.Progress.FeaturesTotal = 10
	tk.Progress.FeaturesCompleted = 5
	if err := store.Update(tk); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	got, err := store.Get("test-002")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.Status != task.StatusRunning {
		t.Errorf("Status after update: got %s, want %s", got.Status, task.StatusRunning)
	}
	if got.Progress.FeaturesCompleted != 5 {
		t.Errorf("FeaturesCompleted: got %d, want 5", got.Progress.FeaturesCompleted)
	}

	// Update non-existent task
	tk2 := task.NewTask("nonexistent", "No", "no", "no", task.TaskConfig{})
	if err := store.Update(tk2); err == nil {
		t.Fatal("expected error updating non-existent task")
	}
}

func TestTaskStore_Delete(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	tk := task.NewTask("test-003", "Delete Task", "desc", "cli-tool", task.TaskConfig{})
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := store.Delete("test-003"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if _, err := store.Get("test-003"); err == nil {
		t.Fatal("expected error getting deleted task")
	}

	// Delete non-existent
	if err := store.Delete("nonexistent"); err == nil {
		t.Fatal("expected error deleting non-existent task")
	}
}

func TestTaskStore_List(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	// Create multiple tasks
	t1 := task.NewTask("task-a", "Task A", "desc", "web", task.TaskConfig{})
	t2 := task.NewTask("task-b", "Task B", "desc", "cli", task.TaskConfig{})
	t3 := task.NewTask("task-c", "Task C", "desc", "web", task.TaskConfig{})
	t3.Status = task.StatusRunning

	for _, tk := range []*task.Task{t1, t2, t3} {
		if err := store.Create(tk); err != nil {
			t.Fatalf("Create failed: %v", err)
		}
		// Need to update t3 status after creation
		if tk.ID == "task-c" {
			if err := store.Update(tk); err != nil {
				t.Fatalf("Update failed: %v", err)
			}
		}
	}

	// List all
	all, err := store.List(nil)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("List all: got %d tasks, want 3", len(all))
	}

	// List with status filter
	running := task.StatusRunning
	filtered, err := store.List(&running)
	if err != nil {
		t.Fatalf("List filtered failed: %v", err)
	}
	if len(filtered) != 1 {
		t.Errorf("List running: got %d tasks, want 1", len(filtered))
	}
	if len(filtered) > 0 && filtered[0].ID != "task-c" {
		t.Errorf("Filtered task ID: got %s, want task-c", filtered[0].ID)
	}
}

func TestTaskStore_FeatureList(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	tk := task.NewTask("test-fl", "FL Task", "desc", "web", task.TaskConfig{})
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	batch1 := 1
	fl := &task.FeatureList{
		Features: []task.Feature{
			{
				ID:          "F001",
				Category:    "functional",
				Description: "User Registration",
				Steps:       []string{"Create form", "Add validation"},
				DependsOn:   []string{},
				Batch:       &batch1,
				Passes:      false,
			},
			{
				ID:          "F002",
				Category:    "functional",
				Description: "Product List",
				Steps:       []string{"Create API", "Create UI"},
				DependsOn:   []string{},
				Batch:       &batch1,
				Passes:      true,
			},
			{
				ID:          "F003",
				Category:    "functional",
				Description: "Shopping Cart",
				Steps:       []string{"Add to cart", "Remove from cart"},
				DependsOn:   []string{"F001", "F002"},
				Batch:       nil,
				Passes:      false,
			},
		},
	}

	// Save
	if err := store.SaveFeatureList("test-fl", fl); err != nil {
		t.Fatalf("SaveFeatureList failed: %v", err)
	}

	// Get
	got, err := store.GetFeatureList("test-fl")
	if err != nil {
		t.Fatalf("GetFeatureList failed: %v", err)
	}
	if len(got.Features) != 3 {
		t.Errorf("Features count: got %d, want 3", len(got.Features))
	}
	if got.Features[0].ID != "F001" {
		t.Errorf("First feature ID: got %s, want F001", got.Features[0].ID)
	}
	if got.Features[1].Passes != true {
		t.Error("F002 should be passes=true")
	}
	if got.Features[2].Batch != nil {
		t.Error("F003 batch should be nil")
	}
}

func TestTaskStore_ExecutionPlan(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	tk := task.NewTask("test-ep", "EP Task", "desc", "web", task.TaskConfig{})
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	ep := &task.ExecutionPlan{
		GeneratedAt: "2026-04-09T10:00:00Z",
		Batches: []task.BatchInfo{
			{Batch: 1, Features: []string{"F001", "F002"}, Status: "completed"},
			{Batch: 2, Features: []string{"F003"}, Status: "pending"},
		},
	}

	if err := store.SaveExecutionPlan("test-ep", ep); err != nil {
		t.Fatalf("SaveExecutionPlan failed: %v", err)
	}

	got, err := store.GetExecutionPlan("test-ep")
	if err != nil {
		t.Fatalf("GetExecutionPlan failed: %v", err)
	}
	if len(got.Batches) != 2 {
		t.Errorf("Batches count: got %d, want 2", len(got.Batches))
	}
	if got.Batches[0].Status != "completed" {
		t.Errorf("Batch 1 status: got %s, want completed", got.Batches[0].Status)
	}
}

func TestTaskStore_Progress(t *testing.T) {
	dir := setupTestDir(t)
	store := NewTaskStore(dir)

	tk := task.NewTask("test-prog", "Prog Task", "desc", "web", task.TaskConfig{})
	if err := store.Create(tk); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	content := "Session 1: Completed user registration feature.\nSession 2: Working on product list.\n"
	if err := store.SaveProgress("test-prog", content); err != nil {
		t.Fatalf("SaveProgress failed: %v", err)
	}

	got, err := store.GetProgress("test-prog")
	if err != nil {
		t.Fatalf("GetProgress failed: %v", err)
	}
	if got != content {
		t.Errorf("Progress content mismatch:\ngot:  %q\nwant: %q", got, content)
	}

	// Empty progress for non-existent file
	empty, err := store.GetProgress("nonexistent")
	if err != nil {
		t.Fatalf("GetProgress for non-existent: %v", err)
	}
	if empty != "" {
		t.Errorf("Expected empty progress, got: %q", empty)
	}
}
