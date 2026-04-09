package session

import (
	"encoding/json"
	"time"
)

// StreamEventType stream-json 事件类型
type StreamEventType string

const (
	EventTypeSystem    StreamEventType = "system"
	EventTypeAssistant StreamEventType = "assistant"
	EventTypeUser      StreamEventType = "user"
	EventTypeResult    StreamEventType = "result"
)

// RawStreamEvent 原始 stream-json 事件（宽松解析）
type RawStreamEvent struct {
	Type      StreamEventType `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	UUID      string          `json:"uuid,omitempty"`

	// system 事件字段
	CWD   string `json:"cwd,omitempty"`
	Model string `json:"model,omitempty"`

	// assistant 事件字段
	Message *AssistantMessage `json:"message,omitempty"`

	// result 事件字段
	IsError      bool    `json:"is_error,omitempty"`
	DurationMs   int64   `json:"duration_ms,omitempty"`
	NumTurns     int     `json:"num_turns,omitempty"`
	Result       string  `json:"result,omitempty"`
	StopReason   string  `json:"stop_reason,omitempty"`
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	Usage        *UsageInfo `json:"usage,omitempty"`
}

// AssistantMessage assistant 消息
type AssistantMessage struct {
	ID      string          `json:"id"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // 可以是 text 或 tool_use 数组
	Usage   *MessageUsage   `json:"usage,omitempty"`
}

// MessageUsage 消息级别的 token 用量
type MessageUsage struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	ServiceTier              string `json:"service_tier,omitempty"`
}

// UsageInfo result 级别的汇总用量
type UsageInfo struct {
	InputTokens              int64  `json:"input_tokens"`
	OutputTokens             int64  `json:"output_tokens"`
	CacheCreationInputTokens int64  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64  `json:"cache_read_input_tokens"`
	ServiceTier              string `json:"service_tier,omitempty"`
}

// ContentBlock 内容块（text 或 tool_use）
type ContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// SessionEvent 解析后的结构化事件（供上层使用）
type SessionEvent struct {
	Timestamp time.Time       `json:"timestamp"`
	Type      SessionEventType `json:"type"`
	SessionID string          `json:"session_id"`

	// 不同类型携带不同字段
	Text       string `json:"text,omitempty"`        // AgentMessage
	ToolName   string `json:"tool_name,omitempty"`   // ToolCall
	ToolInput  string `json:"tool_input,omitempty"`  // ToolCall
	ToolResult string `json:"tool_result,omitempty"` // ToolResult

	// token/cost 信息
	InputTokens  int64   `json:"input_tokens,omitempty"`
	OutputTokens int64   `json:"output_tokens,omitempty"`
	TotalCost    float64 `json:"total_cost,omitempty"`

	// result 信息
	IsError    bool   `json:"is_error,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`
	NumTurns   int    `json:"num_turns,omitempty"`
	DurationMs int64  `json:"duration_ms,omitempty"`

	// 原始 JSON（调试用）
	RawJSON string `json:"-"`
}

// SessionEventType 解析后的事件类型
type SessionEventType string

const (
	SEventInit         SessionEventType = "init"
	SEventAgentMessage SessionEventType = "agent_message"
	SEventToolCall     SessionEventType = "tool_call"
	SEventResult       SessionEventType = "result"
	SEventError        SessionEventType = "error"
	SEventSystem       SessionEventType = "system"
)

// ParseStreamLine 解析一行 stream-json 输出
func ParseStreamLine(line []byte) (*SessionEvent, error) {
	var raw RawStreamEvent
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil, err
	}

	now := time.Now()

	switch raw.Type {
	case EventTypeSystem:
		return parseSystemEvent(raw, now), nil
	case EventTypeAssistant:
		return parseAssistantEvent(raw, now)
	case EventTypeResult:
		return parseResultEvent(raw, now), nil
	default:
		// 未知类型也返回，标记为 system
		return &SessionEvent{
			Timestamp: now,
			Type:      SEventSystem,
			SessionID: raw.SessionID,
			RawJSON:   string(line),
		}, nil
	}
}

func parseSystemEvent(raw RawStreamEvent, ts time.Time) *SessionEvent {
	ev := &SessionEvent{
		Timestamp: ts,
		Type:      SEventSystem,
		SessionID: raw.SessionID,
	}

	if raw.Subtype == "init" {
		ev.Type = SEventInit
		ev.Text = "Session initialized"
	}

	return ev
}

func parseAssistantEvent(raw RawStreamEvent, ts time.Time) (*SessionEvent, error) {
	if raw.Message == nil {
		return &SessionEvent{
			Timestamp: ts,
			Type:      SEventAgentMessage,
			SessionID: raw.SessionID,
		}, nil
	}

	// 解析 content blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(raw.Message.Content, &blocks); err != nil {
		// content 可能是字符串而非数组
		return &SessionEvent{
			Timestamp: ts,
			Type:      SEventAgentMessage,
			SessionID: raw.SessionID,
			Text:      string(raw.Message.Content),
		}, nil
	}

	// 提取 token 信息
	var inputTokens, outputTokens int64
	if raw.Message.Usage != nil {
		inputTokens = raw.Message.Usage.InputTokens
		outputTokens = raw.Message.Usage.OutputTokens
	}

	// 遍历 content blocks
	var events []*SessionEvent
	for _, block := range blocks {
		switch block.Type {
		case "text":
			events = append(events, &SessionEvent{
				Timestamp:    ts,
				Type:         SEventAgentMessage,
				SessionID:    raw.SessionID,
				Text:         block.Text,
				InputTokens:  inputTokens,
				OutputTokens: outputTokens,
			})
		case "tool_use":
			inputStr := ""
			if block.Input != nil {
				inputStr = string(block.Input)
			}
			events = append(events, &SessionEvent{
				Timestamp: ts,
				Type:      SEventToolCall,
				SessionID: raw.SessionID,
				ToolName:  block.Name,
				ToolInput: inputStr,
			})
		}
	}

	// 如果没有有效的 block，返回一个空消息事件
	if len(events) == 0 {
		return &SessionEvent{
			Timestamp:    ts,
			Type:         SEventAgentMessage,
			SessionID:    raw.SessionID,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
		}, nil
	}

	// 返回第一个事件（后续可改为返回多个）
	return events[0], nil
}

func parseResultEvent(raw RawStreamEvent, ts time.Time) *SessionEvent {
	ev := &SessionEvent{
		Timestamp:  ts,
		Type:       SEventResult,
		SessionID:  raw.SessionID,
		IsError:    raw.IsError,
		StopReason: raw.StopReason,
		NumTurns:   raw.NumTurns,
		DurationMs: raw.DurationMs,
		TotalCost:  raw.TotalCostUSD,
		Text:       raw.Result,
	}

	if raw.Usage != nil {
		ev.InputTokens = raw.Usage.InputTokens
		ev.OutputTokens = raw.Usage.OutputTokens
	}

	if raw.IsError {
		ev.Type = SEventError
	}

	return ev
}

// ExtractTokenUsage 从 result 事件中提取 token 汇总
func ExtractTokenUsage(ev *SessionEvent) (input, output int64, cost float64) {
	return ev.InputTokens, ev.OutputTokens, ev.TotalCost
}
