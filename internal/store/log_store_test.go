package store

import (
	"os"
	"strings"
	"testing"
)

func TestLogStore_AppendAndRead(t *testing.T) {
	dir := setupTestDir(t)
	store := NewLogStore(dir)

	taskID := "test-task"
	sessionID := "session-001"

	// 创建 sessions 子目录
	if err := os.MkdirAll(dir+"/tasks/"+taskID+"/sessions", 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Append
	if err := store.Append(taskID, sessionID, "line 1\n"); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := store.Append(taskID, sessionID, "line 2\n"); err != nil {
		t.Fatalf("Append failed: %v", err)
	}
	if err := store.Append(taskID, sessionID, "line 3\n"); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read
	content, err := store.Read(taskID, sessionID)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if content != "line 1\nline 2\nline 3\n" {
		t.Errorf("Content mismatch: got %q", content)
	}

	// Read non-existent
	empty, err := store.Read(taskID, "nonexistent")
	if err != nil {
		t.Fatalf("Read non-existent failed: %v", err)
	}
	if empty != "" {
		t.Errorf("Expected empty, got: %q", empty)
	}
}

func TestLogStore_Tail(t *testing.T) {
	dir := setupTestDir(t)
	store := NewLogStore(dir)

	taskID := "test-task"
	sessionID := "session-002"

	if err := os.MkdirAll(dir+"/tasks/"+taskID+"/sessions", 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Write multi-line content
	lines := []string{}
	for i := 1; i <= 10; i++ {
		lines = append(lines, "log line "+string(rune('0'+i)))
	}
	content := strings.Join(lines, "\n") + "\n"
	if err := store.Append(taskID, sessionID, content); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Tail last 3 lines
	tail, err := store.Tail(taskID, sessionID, 3)
	if err != nil {
		t.Fatalf("Tail failed: %v", err)
	}
	if len(tail) != 3 {
		t.Errorf("Tail lines: got %d, want 3", len(tail))
	}

	// Tail more than available
	all, err := store.Tail(taskID, sessionID, 100)
	if err != nil {
		t.Fatalf("Tail all failed: %v", err)
	}
	// Should return all non-empty lines
	if len(all) < 10 {
		t.Errorf("Tail all: got %d, want >= 10", len(all))
	}

	// Tail non-existent
	empty, err := store.Tail(taskID, "nonexistent", 5)
	if err != nil {
		t.Fatalf("Tail non-existent: %v", err)
	}
	if empty != nil {
		t.Errorf("Expected nil, got: %v", empty)
	}
}

func TestLogStore_ReadFrom(t *testing.T) {
	dir := setupTestDir(t)
	store := NewLogStore(dir)

	taskID := "test-task"
	sessionID := "session-003"

	if err := os.MkdirAll(dir+"/tasks/"+taskID+"/sessions", 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Write initial content
	if err := store.Append(taskID, sessionID, "hello "); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read from beginning
	content, offset, err := store.ReadFrom(taskID, sessionID, 0)
	if err != nil {
		t.Fatalf("ReadFrom failed: %v", err)
	}
	if content != "hello " {
		t.Errorf("Content: got %q, want %q", content, "hello ")
	}
	if offset != 6 {
		t.Errorf("Offset: got %d, want 6", offset)
	}

	// Append more
	if err := store.Append(taskID, sessionID, "world"); err != nil {
		t.Fatalf("Append failed: %v", err)
	}

	// Read from last offset (incremental)
	content2, offset2, err := store.ReadFrom(taskID, sessionID, offset)
	if err != nil {
		t.Fatalf("ReadFrom incremental failed: %v", err)
	}
	if content2 != "world" {
		t.Errorf("Incremental content: got %q, want %q", content2, "world")
	}
	if offset2 != 11 {
		t.Errorf("Offset2: got %d, want 11", offset2)
	}
}

func TestLogStore_Events(t *testing.T) {
	dir := setupTestDir(t)
	store := NewLogStore(dir)

	taskID := "test-task"

	if err := os.MkdirAll(dir+"/tasks/"+taskID+"/events", 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	// Append events
	events := []string{
		`{"type":"task_status","task_id":"test-task","status":"running"}`,
		`{"type":"session_start","task_id":"test-task","session_id":"s1"}`,
		`{"type":"feature_update","task_id":"test-task","feature_id":"F001"}`,
	}
	for _, ev := range events {
		if err := store.AppendEvent(taskID, ev); err != nil {
			t.Fatalf("AppendEvent failed: %v", err)
		}
	}

	// Read events
	got, err := store.ReadEvents(taskID)
	if err != nil {
		t.Fatalf("ReadEvents failed: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("Events count: got %d, want 3", len(got))
	}
	for i, ev := range got {
		if ev != events[i] {
			t.Errorf("Event %d mismatch:\ngot:  %s\nwant: %s", i, ev, events[i])
		}
	}

	// Read events from non-existent task
	empty, err := store.ReadEvents("nonexistent")
	if err != nil {
		t.Fatalf("ReadEvents non-existent: %v", err)
	}
	if empty != nil {
		t.Errorf("Expected nil, got: %v", empty)
	}
}
