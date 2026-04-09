package session

import (
	"testing"
)

func TestParseStreamLine_SystemInit(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"init","cwd":"/workspace","session_id":"abc-123","model":"claude-sonnet-4-6"}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}

	if ev.Type != SEventInit {
		t.Errorf("Type: got %s, want %s", ev.Type, SEventInit)
	}
	if ev.SessionID != "abc-123" {
		t.Errorf("SessionID: got %s, want abc-123", ev.SessionID)
	}
}

func TestParseStreamLine_SystemHook(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"hook_started","hook_name":"SessionStart","session_id":"abc-123"}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}

	if ev.Type != SEventSystem {
		t.Errorf("Type: got %s, want %s", ev.Type, SEventSystem)
	}
}

func TestParseStreamLine_AssistantTextMessage(t *testing.T) {
	line := []byte(`{
		"type": "assistant",
		"message": {
			"id": "msg_001",
			"role": "assistant",
			"content": [{"type": "text", "text": "Hello, I will help you."}],
			"usage": {"input_tokens": 100, "output_tokens": 20, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
		},
		"session_id": "abc-123"
	}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}

	if ev.Type != SEventAgentMessage {
		t.Errorf("Type: got %s, want %s", ev.Type, SEventAgentMessage)
	}
	if ev.Text != "Hello, I will help you." {
		t.Errorf("Text: got %q, want %q", ev.Text, "Hello, I will help you.")
	}
	if ev.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", ev.InputTokens)
	}
	if ev.OutputTokens != 20 {
		t.Errorf("OutputTokens: got %d, want 20", ev.OutputTokens)
	}
}

func TestParseStreamLine_AssistantToolUse(t *testing.T) {
	line := []byte(`{
		"type": "assistant",
		"message": {
			"id": "msg_002",
			"role": "assistant",
			"content": [{"type": "tool_use", "id": "tu_001", "name": "Bash", "input": {"command": "ls -la"}}],
			"usage": {"input_tokens": 50, "output_tokens": 30, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
		},
		"session_id": "abc-123"
	}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}

	if ev.Type != SEventToolCall {
		t.Errorf("Type: got %s, want %s", ev.Type, SEventToolCall)
	}
	if ev.ToolName != "Bash" {
		t.Errorf("ToolName: got %s, want Bash", ev.ToolName)
	}
	if ev.ToolInput == "" {
		t.Error("ToolInput should not be empty")
	}
}

func TestParseStreamLine_ResultSuccess(t *testing.T) {
	line := []byte(`{
		"type": "result",
		"subtype": "success",
		"is_error": false,
		"duration_ms": 4186,
		"num_turns": 3,
		"result": "Task completed successfully",
		"stop_reason": "end_turn",
		"total_cost_usd": 0.138,
		"session_id": "abc-123",
		"usage": {"input_tokens": 1000, "output_tokens": 500, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}

	if ev.Type != SEventResult {
		t.Errorf("Type: got %s, want %s", ev.Type, SEventResult)
	}
	if ev.IsError {
		t.Error("IsError should be false")
	}
	if ev.NumTurns != 3 {
		t.Errorf("NumTurns: got %d, want 3", ev.NumTurns)
	}
	if ev.DurationMs != 4186 {
		t.Errorf("DurationMs: got %d, want 4186", ev.DurationMs)
	}
	if ev.TotalCost != 0.138 {
		t.Errorf("TotalCost: got %f, want 0.138", ev.TotalCost)
	}
	if ev.InputTokens != 1000 {
		t.Errorf("InputTokens: got %d, want 1000", ev.InputTokens)
	}
	if ev.OutputTokens != 500 {
		t.Errorf("OutputTokens: got %d, want 500", ev.OutputTokens)
	}
	if ev.StopReason != "end_turn" {
		t.Errorf("StopReason: got %s, want end_turn", ev.StopReason)
	}
}

func TestParseStreamLine_ResultError(t *testing.T) {
	line := []byte(`{
		"type": "result",
		"subtype": "error",
		"is_error": true,
		"result": "Something went wrong",
		"session_id": "abc-123",
		"usage": {"input_tokens": 50, "output_tokens": 0, "cache_creation_input_tokens": 0, "cache_read_input_tokens": 0}
	}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}

	if ev.Type != SEventError {
		t.Errorf("Type: got %s, want %s", ev.Type, SEventError)
	}
	if !ev.IsError {
		t.Error("IsError should be true")
	}
}

func TestParseStreamLine_InvalidJSON(t *testing.T) {
	line := []byte(`not valid json`)
	_, err := ParseStreamLine(line)
	if err == nil {
		t.Fatal("Expected error for invalid JSON")
	}
}

func TestParseStreamLine_UnknownType(t *testing.T) {
	line := []byte(`{"type":"rate_limit_event","session_id":"abc-123"}`)

	ev, err := ParseStreamLine(line)
	if err != nil {
		t.Fatalf("ParseStreamLine failed: %v", err)
	}
	if ev.Type != SEventSystem {
		t.Errorf("Unknown type should be SEventSystem, got %s", ev.Type)
	}
}

func TestExtractTokenUsage(t *testing.T) {
	ev := &SessionEvent{
		InputTokens:  1000,
		OutputTokens: 500,
		TotalCost:    0.138,
	}

	input, output, cost := ExtractTokenUsage(ev)
	if input != 1000 {
		t.Errorf("input: got %d, want 1000", input)
	}
	if output != 500 {
		t.Errorf("output: got %d, want 500", output)
	}
	if cost != 0.138 {
		t.Errorf("cost: got %f, want 0.138", cost)
	}
}
