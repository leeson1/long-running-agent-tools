package task

import "testing"

func TestTaskStatus_IsTerminal(t *testing.T) {
	tests := []struct {
		status   TaskStatus
		terminal bool
	}{
		{StatusPending, false},
		{StatusRunning, false},
		{StatusCompleted, true},
		{StatusFailed, true},
		{StatusCancelled, true},
		{StatusPaused, false},
	}
	for _, tt := range tests {
		if got := tt.status.IsTerminal(); got != tt.terminal {
			t.Errorf("%s.IsTerminal() = %v, want %v", tt.status, got, tt.terminal)
		}
	}
}

func TestTaskStatus_IsActive(t *testing.T) {
	tests := []struct {
		status TaskStatus
		active bool
	}{
		{StatusPending, false},
		{StatusInitializing, true},
		{StatusRunning, true},
		{StatusMerging, true},
		{StatusValidating, true},
		{StatusPaused, false},
		{StatusCompleted, false},
	}
	for _, tt := range tests {
		if got := tt.status.IsActive(); got != tt.active {
			t.Errorf("%s.IsActive() = %v, want %v", tt.status, got, tt.active)
		}
	}
}

func TestTaskStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from  TaskStatus
		to    TaskStatus
		valid bool
	}{
		{StatusPending, StatusInitializing, true},
		{StatusPending, StatusRunning, false},
		{StatusPending, StatusCancelled, true},
		{StatusInitializing, StatusPlanning, true},
		{StatusInitializing, StatusCompleted, false},
		{StatusRunning, StatusMerging, true},
		{StatusRunning, StatusPaused, true},
		{StatusRunning, StatusCompleted, true},
		{StatusCompleted, StatusRunning, false},
		{StatusFailed, StatusRunning, true},
		{StatusPaused, StatusRunning, true},
		{StatusConflictWait, StatusRunning, true},
	}
	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.valid {
			t.Errorf("%s.CanTransitionTo(%s) = %v, want %v", tt.from, tt.to, got, tt.valid)
		}
	}
}

func TestTask_TransitionTo(t *testing.T) {
	tk := NewTask("id", "name", "desc", "web", TaskConfig{})

	// Valid transition
	if err := tk.TransitionTo(StatusInitializing); err != nil {
		t.Errorf("TransitionTo(initializing) unexpected error: %v", err)
	}
	if tk.Status != StatusInitializing {
		t.Errorf("Status after transition: got %s, want %s", tk.Status, StatusInitializing)
	}

	// Invalid transition
	if err := tk.TransitionTo(StatusCompleted); err == nil {
		t.Error("Expected error for invalid transition initializing -> completed")
	}
	// Status should not change on error
	if tk.Status != StatusInitializing {
		t.Errorf("Status should not change on invalid transition: got %s", tk.Status)
	}
}

func TestNewTask(t *testing.T) {
	tk := NewTask("abc", "My Task", "description", "cli-tool", TaskConfig{
		MaxParallelWorkers: 2,
		SessionTimeout:     "15m",
		WorkspaceDir:       "/tmp/test",
	})

	if tk.ID != "abc" {
		t.Errorf("ID: got %s, want abc", tk.ID)
	}
	if tk.Status != StatusPending {
		t.Errorf("Status: got %s, want pending", tk.Status)
	}
	if tk.Config.MaxParallelWorkers != 2 {
		t.Errorf("MaxParallelWorkers: got %d, want 2", tk.Config.MaxParallelWorkers)
	}
	if tk.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}
