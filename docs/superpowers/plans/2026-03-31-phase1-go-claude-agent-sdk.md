# Phase 1: Go Claude Agent SDK — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go SDK that wraps the `claude` CLI binary for programmatic agent interactions, with full feature parity with the official TypeScript/Python SDKs.

**Architecture:** The SDK spawns a `claude` CLI subprocess, communicating via bidirectional NDJSON on stdin/stdout. A `Transport` interface abstracts process management. `Query()` provides one-shot streaming, `Client` provides multi-turn interactive sessions. Functional options configure model, tools, permissions, MCP servers, hooks, and agents.

**Tech Stack:** Go 1.22+, no external dependencies (stdlib only). The `claude` CLI binary must be installed on the host.

---

### File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `go.mod` | Module declaration: `github.com/anthropics/claude-agent-sdk-go` |
| Create | `error.go` | `SDKError` interface + `CLINotFoundError`, `CLIConnectionError`, `ProcessError`, `JSONDecodeError`, `MessageParseError` |
| Create | `message.go` | `SDKMessage` tagged union + `UserMessage`, `AssistantMessage`, `SystemMessage`, `ResultMessage`, `RateLimitEvent`, `TaskStartedMessage`, `TaskProgressMessage`, `TaskNotificationMessage` |
| Create | `content.go` | `ContentBlock` tagged union + `TextBlock`, `ThinkingBlock`, `ToolUseBlock`, `ToolResultBlock` |
| Create | `options.go` | `QueryOption` functional options, `queryConfig` struct, all `With*` option functions, `TaskBudget`, `SandboxSettings`, `PermissionUpdate`, `PermissionRule`, `ToolConfig`, `PreviewFormat` |
| Create | `hook.go` | `HookEvent` constants (27 events), `HookMatcher`, `HookCallback`, `HookInput`, `HookOutput`, `HookSpecificOutput` |
| Create | `mcp.go` | `McpServerConfig` variants (stdio, SSE, HTTP, SDK, proxy), `McpSdkServer`, `McpTool`, `McpServerInfo`, `McpToolInfo`, `ToolAnnotations`, `Tool[T]()` generic helper, `CreateSdkMcpServer()` |
| Create | `internal/clilookup/clilookup.go` | Claude CLI binary discovery logic |
| Create | `internal/transport/transport.go` | `Transport` interface definition |
| Create | `internal/transport/subprocess.go` | `SubprocessTransport` implementation (spawn, stdin/stdout, shutdown) |
| Create | `controlprotocol.go` | `controlHandler` for routing hook callbacks, permission checks, and SDK MCP tool calls |
| Create | `sdk.go` | `Query()` entry point, `Stream` type (iterator over `SDKMessage`), `NewStreamFromTransport()`, `BuildCLIArgsForTest()` |
| Create | `client.go` | `Client` type for interactive multi-turn sessions, `ContextUsage`, `ServerInfo`, `RuntimeSettings`, `ReloadPluginsResult`, `SetMaxThinkingTokens`, `GetContextUsage`, `GetServerInfo`, `GetSettings`, `PromptSuggestion`, `EnableChannel`, `ReloadPlugins`, `RewindFiles`, `SupportedCommands`, `SupportedModels`, `SupportedAgents`, `GetAccountInfo`, `SeedReadState`, `ApplyFlagSettings`, `SetFastMode`, V2 Preview API (`V2Session`, `UnstableV2CreateSession`, `UnstableV2ResumeSession`, `UnstableV2Prompt`) |
| Create | `session.go` | `ListSessions`, `GetSessionInfo`, `GetSessionMessages`, mutations |
| Create | `examples/basic/main.go` | Minimal one-shot query example |
| Create | `examples/hooks/main.go` | Hook callback example |
| Create | `examples/mcp/main.go` | MCP server example |
| Create | `examples/interactive/main.go` | Interactive client example |
| Create | `sdk_test.go` | Unit tests (mocked transport) + integration tests (real CLI) |

---

### Task 1: Project scaffolding

**Files:**
- Create: `go.mod`
- Create: `internal/version.go`

- [ ] **Step 1: Initialize the Go module**

Create `go.mod`:

```
module github.com/anthropics/claude-agent-sdk-go

go 1.22
```

- [ ] **Step 2: Create version constant**

Create `internal/version.go`:

```go
package internal

// Version is the SDK version. Updated on release.
const Version = "0.1.0"
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: Clean build (no source files in root yet, but internal/ compiles).

- [ ] **Step 4: Commit**

```bash
git add go.mod internal/version.go
git commit -m "chore: initialize Go module for claude-agent-sdk-go"
```

---

### Task 2: Error types

**Files:**
- Create: `error.go`

- [ ] **Step 1: Write error types**

Create `error.go`:

```go
package agentsdk

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SDKError is the common interface for all SDK error types.
type SDKError interface {
	error
	sdkError() // marker method
}

// CLINotFoundError is returned when the claude CLI binary cannot be found.
type CLINotFoundError struct {
	SearchedPaths []string
}

func (e *CLINotFoundError) Error() string {
	return fmt.Sprintf("claude CLI not found (searched: %s)", strings.Join(e.SearchedPaths, ", "))
}

func (*CLINotFoundError) sdkError() {}

// CLIConnectionError is returned when the SDK cannot connect to the claude process.
type CLIConnectionError struct {
	Reason string
}

func (e *CLIConnectionError) Error() string {
	return fmt.Sprintf("claude CLI connection error: %s", e.Reason)
}

func (*CLIConnectionError) sdkError() {}

// ProcessError is returned when the claude subprocess exits with a non-zero code.
type ProcessError struct {
	ExitCode int
	Stderr   string
}

func (e *ProcessError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("claude process exited with code %d: %s", e.ExitCode, e.Stderr)
	}
	return fmt.Sprintf("claude process exited with code %d", e.ExitCode)
}

func (*ProcessError) sdkError() {}

// JSONDecodeError is returned when a stdout line contains invalid JSON.
type JSONDecodeError struct {
	Line          string
	OriginalError error
}

func (e *JSONDecodeError) Error() string {
	return fmt.Sprintf("invalid JSON on stdout: %s (line: %.100s)", e.OriginalError, e.Line)
}

func (e *JSONDecodeError) Unwrap() error {
	return e.OriginalError
}

func (*JSONDecodeError) sdkError() {}

// MessageParseError is returned when a message is missing required fields.
type MessageParseError struct {
	Data json.RawMessage
	Err  error
}

func (e *MessageParseError) Error() string {
	return fmt.Sprintf("failed to parse message: %s (data: %.200s)", e.Err, string(e.Data))
}

func (e *MessageParseError) Unwrap() error {
	return e.Err
}

func (*MessageParseError) sdkError() {}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add error.go
git commit -m "feat: add error types for CLI, process, and parse failures"
```

---

### Task 3: Content block types

**Files:**
- Create: `content.go`

Content blocks are the building blocks of messages. They need to be defined before messages since `AssistantMessage.Content` is `[]ContentBlock`.

- [ ] **Step 1: Write content block types**

Create `content.go`:

```go
package agentsdk

import "encoding/json"

// ContentBlock is a tagged union of content block types within a message.
// Use the As* methods to access the concrete type.
type ContentBlock struct {
	Type string          `json:"type"` // "text", "thinking", "tool_use", "tool_result"
	Raw  json.RawMessage `json:"-"`    // Original JSON for advanced use cases
}

// UnmarshalJSON implements custom unmarshalling to capture raw JSON.
func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	// Capture raw data.
	b.Raw = append(b.Raw[:0], data...)

	// Extract type field.
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	b.Type = envelope.Type
	return nil
}

// AsText returns the block as a TextBlock if Type == "text".
func (b ContentBlock) AsText() (*TextBlock, bool) {
	if b.Type != "text" {
		return nil, false
	}
	var t TextBlock
	if err := json.Unmarshal(b.Raw, &t); err != nil {
		return nil, false
	}
	return &t, true
}

// AsThinking returns the block as a ThinkingBlock if Type == "thinking".
func (b ContentBlock) AsThinking() (*ThinkingBlock, bool) {
	if b.Type != "thinking" {
		return nil, false
	}
	var t ThinkingBlock
	if err := json.Unmarshal(b.Raw, &t); err != nil {
		return nil, false
	}
	return &t, true
}

// AsToolUse returns the block as a ToolUseBlock if Type == "tool_use".
func (b ContentBlock) AsToolUse() (*ToolUseBlock, bool) {
	if b.Type != "tool_use" {
		return nil, false
	}
	var t ToolUseBlock
	if err := json.Unmarshal(b.Raw, &t); err != nil {
		return nil, false
	}
	return &t, true
}

// AsToolResult returns the block as a ToolResultBlock if Type == "tool_result".
func (b ContentBlock) AsToolResult() (*ToolResultBlock, bool) {
	if b.Type != "tool_result" {
		return nil, false
	}
	var t ToolResultBlock
	if err := json.Unmarshal(b.Raw, &t); err != nil {
		return nil, false
	}
	return &t, true
}

// TextBlock contains plain text content.
type TextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// ThinkingBlock contains extended thinking content.
type ThinkingBlock struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

// ToolUseBlock represents a tool invocation by the model.
type ToolUseBlock struct {
	Type  string         `json:"type"`
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResultBlock contains the result of a tool invocation.
type ToolResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content"` // string or []ContentBlock
	IsError   *bool  `json:"is_error,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add content.go
git commit -m "feat: add ContentBlock tagged union with Text, Thinking, ToolUse, ToolResult"
```

---

### Task 4: Message types

**Files:**
- Create: `message.go`

- [ ] **Step 1: Write the SDKMessage union and concrete message types**

Create `message.go`:

```go
package agentsdk

import "encoding/json"

// SDKMessage is a tagged union of all message types from the claude process.
// Use the As* methods to access the concrete type.
type SDKMessage struct {
	Type string          `json:"type"` // "user", "assistant", "system", "result", "tool_result", "stream_event", "rate_limit", "task_started", "task_progress", "task_notification"
	Raw  json.RawMessage `json:"-"`    // Original JSON for advanced use cases
}

// UnmarshalJSON implements custom unmarshalling to capture raw JSON and extract type.
func (m *SDKMessage) UnmarshalJSON(data []byte) error {
	m.Raw = append(m.Raw[:0], data...)

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	m.Type = envelope.Type
	return nil
}

// AsUser returns the message as a UserMessage if Type == "user".
func (m SDKMessage) AsUser() (*UserMessage, bool) {
	if m.Type != "user" {
		return nil, false
	}
	var msg UserMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsAssistant returns the message as an AssistantMessage if Type == "assistant".
func (m SDKMessage) AsAssistant() (*AssistantMessage, bool) {
	if m.Type != "assistant" {
		return nil, false
	}
	var msg AssistantMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsSystem returns the message as a SystemMessage if Type == "system".
func (m SDKMessage) AsSystem() (*SystemMessage, bool) {
	if m.Type != "system" {
		return nil, false
	}
	var msg SystemMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsResult returns the message as a ResultMessage if Type == "result".
func (m SDKMessage) AsResult() (*ResultMessage, bool) {
	if m.Type != "result" {
		return nil, false
	}
	var msg ResultMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsStreamEvent returns the message as a StreamEvent if Type == "stream_event".
func (m SDKMessage) AsStreamEvent() (*StreamEvent, bool) {
	if m.Type != "stream_event" {
		return nil, false
	}
	var msg StreamEvent
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsRateLimit returns the message as a RateLimitEvent if Type == "rate_limit".
func (m SDKMessage) AsRateLimit() (*RateLimitEvent, bool) {
	if m.Type != "rate_limit" {
		return nil, false
	}
	var msg RateLimitEvent
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// UserMessage represents a user input message.
type UserMessage struct {
	Type            string `json:"type"`
	Content         any    `json:"content"` // string or []ContentBlock
	UUID            string `json:"uuid"`
	SessionID       string `json:"session_id"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
}

// AssistantMessage represents a complete response from Claude.
type AssistantMessage struct {
	Type            string         `json:"type"`
	Content         []ContentBlock `json:"content"`
	Model           string         `json:"model"`
	StopReason      string         `json:"stop_reason"`
	UUID            string         `json:"uuid"`
	MessageID       string         `json:"message_id,omitempty"`
	SessionID       string         `json:"session_id"`
	ParentToolUseID string         `json:"parent_tool_use_id,omitempty"`
	Usage           *MessageUsage  `json:"usage,omitempty"`
	// Error is set when the assistant response encountered an error.
	// Values: "authentication_failed", "billing_error", "rate_limit",
	// "invalid_request", "server_error", "max_output_tokens", "unknown".
	Error string `json:"error,omitempty"`
}

// SystemMessage represents a system event (init, status, etc.).
type SystemMessage struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"` // "init", "api_retry", "mcp_status", etc.
	UUID      string          `json:"uuid"`
	SessionID string          `json:"session_id"`
	Data      json.RawMessage `json:"data,omitempty"` // Subtype-specific payload (MCP status, init data, etc.)
}

// ResultMessage is the final message with execution summary.
// Subtype is one of: "success", "error_max_turns", "error_during_execution",
// "error_max_budget_usd", "error_max_structured_output_retries", "paused".
type ResultMessage struct {
	Type             string            `json:"type"`
	Subtype          string            `json:"subtype"`
	SessionID        string            `json:"session_id"`
	DurationMs       int               `json:"duration_ms"`
	DurationAPIMs    int               `json:"duration_api_ms"`
	IsError          bool              `json:"is_error"`
	NumTurns         int               `json:"num_turns"`
	TotalCostUSD     *float64          `json:"total_cost_usd,omitempty"`
	Result           string            `json:"result"`
	Errors           []string          `json:"errors,omitempty"` // Error details when subtype is error_*
	StopReason       string            `json:"stop_reason"`
	Usage            *ResultUsage      `json:"usage,omitempty"`
	ModelUsage       map[string]any    `json:"model_usage,omitempty"`       // Per-model usage breakdown
	StructuredOutput any               `json:"structured_output,omitempty"` // Structured output when output_format is set
	PermissionDenials []PermissionDenial `json:"permission_denials,omitempty"`
}

// PermissionDenial records a tool use that was denied by permission policy.
type PermissionDenial struct {
	ToolName  string         `json:"tool_name"`
	ToolUseID string         `json:"tool_use_id"`
	ToolInput map[string]any `json:"tool_input"`
}

// StreamEvent represents a partial streaming event (token deltas, etc.).
type StreamEvent struct {
	Type      string          `json:"type"`
	Event     json.RawMessage `json:"event"` // Varies by event type
	SessionID string          `json:"session_id"`
	UUID      string          `json:"uuid"`
}

// RateLimitEvent is emitted when a rate limit is encountered.
type RateLimitEvent struct {
	Type          string         `json:"type"`
	RateLimitInfo *RateLimitInfo `json:"rate_limit_info"`
	SessionID     string         `json:"session_id"`
	UUID          string         `json:"uuid"`
}

// RateLimitInfo contains details about a rate limit.
type RateLimitInfo struct {
	Status                string  `json:"status"`                            // "allowed", "allowed_warning", "rejected"
	ResetsAt              string  `json:"resets_at"`                         // ISO 8601 timestamp
	RateLimitType         string  `json:"rate_limit_type"`                   // "five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet", "overage"
	Utilization           float64 `json:"utilization"`                       // 0.0–1.0
	RequestsRemaining     *int    `json:"requests_remaining,omitempty"`      // Remaining requests in window
	RequestsLimit         *int    `json:"requests_limit,omitempty"`          // Total request limit
	TokensRemaining       *int    `json:"tokens_remaining,omitempty"`        // Remaining tokens in window
	TokensLimit           *int    `json:"tokens_limit,omitempty"`            // Total token limit
	OverageStatus         string  `json:"overage_status,omitempty"`          // Overage billing status
	OverageResetsAt       string  `json:"overage_resets_at,omitempty"`       // Overage reset timestamp
	OverageDisabledReason string  `json:"overage_disabled_reason,omitempty"` // Why overage is disabled
}

// AsToolResult returns the message as a ToolResultMessage if Type == "tool_result".
// This is a top-level message type (distinct from ToolResultBlock content block).
func (m SDKMessage) AsToolResult() (*ToolResultMessage, bool) {
	if m.Type != "tool_result" {
		return nil, false
	}
	var msg ToolResultMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// ToolResultMessage represents the result of a tool execution.
type ToolResultMessage struct {
	Type            string `json:"type"`
	ToolUseID       string `json:"tool_use_id"`
	Content         any    `json:"content"` // string or structured content
	IsError         *bool  `json:"is_error,omitempty"`
	SessionID       string `json:"session_id"`
	UUID            string `json:"uuid"`
	ParentToolUseID string `json:"parent_tool_use_id,omitempty"`
}

// AsTaskStarted returns the message as a TaskStartedMessage if Type == "task_started".
func (m SDKMessage) AsTaskStarted() (*TaskStartedMessage, bool) {
	if m.Type != "task_started" {
		return nil, false
	}
	var msg TaskStartedMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsTaskProgress returns the message as a TaskProgressMessage if Type == "task_progress".
func (m SDKMessage) AsTaskProgress() (*TaskProgressMessage, bool) {
	if m.Type != "task_progress" {
		return nil, false
	}
	var msg TaskProgressMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// AsTaskNotification returns the message as a TaskNotificationMessage if Type == "task_notification".
func (m SDKMessage) AsTaskNotification() (*TaskNotificationMessage, bool) {
	if m.Type != "task_notification" {
		return nil, false
	}
	var msg TaskNotificationMessage
	if err := json.Unmarshal(m.Raw, &msg); err != nil {
		return nil, false
	}
	return &msg, true
}

// TaskStartedMessage is emitted when a background task begins execution.
type TaskStartedMessage struct {
	Type      string `json:"type"`
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	UUID      string `json:"uuid"`
}

// TaskProgressMessage is emitted periodically while a background task runs.
type TaskProgressMessage struct {
	Type      string     `json:"type"`
	TaskID    string     `json:"task_id"`
	SessionID string     `json:"session_id"`
	UUID      string     `json:"uuid"`
	Usage     *TaskUsage `json:"usage,omitempty"`
}

// TaskNotificationMessage is emitted when a background task completes, fails, or is stopped.
type TaskNotificationMessage struct {
	Type       string     `json:"type"`
	TaskID     string     `json:"task_id"`
	Status     string     `json:"status"` // "completed", "failed", "stopped"
	OutputFile string     `json:"output_file,omitempty"`
	Summary    string     `json:"summary,omitempty"`
	SessionID  string     `json:"session_id"`
	UUID       string     `json:"uuid"`
	Usage      *TaskUsage `json:"usage,omitempty"`
}

// TaskUsage tracks resource usage for a background task.
type TaskUsage struct {
	TotalTokens int   `json:"total_tokens"`
	ToolUses    int   `json:"tool_uses"`
	DurationMs  int64 `json:"duration_ms"`
}

// MessageUsage tracks token usage for a single message.
type MessageUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ResultUsage tracks aggregate token usage for the entire session.
type ResultUsage struct {
	InputTokens              int      `json:"input_tokens"`
	OutputTokens             int      `json:"output_tokens"`
	CacheCreationInputTokens int      `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int      `json:"cache_read_input_tokens,omitempty"`
	CostUSD                  *float64 `json:"cost_usd,omitempty"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add message.go
git commit -m "feat: add SDKMessage tagged union with User, Assistant, System, Result, StreamEvent"
```

---

### Task 5: Hook types

**Files:**
- Create: `hook.go`

- [ ] **Step 1: Write hook types**

Create `hook.go`:

```go
package agentsdk

import (
	"context"
	"time"
)

// HookEvent identifies when a hook fires in the agent lifecycle.
type HookEvent string

const (
	HookPreToolUse         HookEvent = "PreToolUse"
	HookPostToolUse        HookEvent = "PostToolUse"
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"
	HookUserPromptSubmit   HookEvent = "UserPromptSubmit"
	HookSessionStart       HookEvent = "SessionStart"
	HookSessionEnd         HookEvent = "SessionEnd"
	HookStop               HookEvent = "Stop"
	HookStopFailure        HookEvent = "StopFailure"
	HookSubagentStart      HookEvent = "SubagentStart"
	HookSubagentStop       HookEvent = "SubagentStop"
	HookPreCompact         HookEvent = "PreCompact"
	HookPostCompact        HookEvent = "PostCompact"
	HookNotification       HookEvent = "Notification"
	HookPermissionRequest  HookEvent = "PermissionRequest"
	HookPermissionDenied   HookEvent = "PermissionDenied"
	HookSetup              HookEvent = "Setup"
	HookInstructionsLoaded HookEvent = "InstructionsLoaded"
	HookElicitation        HookEvent = "Elicitation"
	HookElicitationResult  HookEvent = "ElicitationResult"
	HookTeammateIdle       HookEvent = "TeammateIdle"
	HookTaskCreated        HookEvent = "TaskCreated"
	HookTaskCompleted      HookEvent = "TaskCompleted"
	HookConfigChange       HookEvent = "ConfigChange"
	HookCwdChanged         HookEvent = "CwdChanged"
	HookFileChanged        HookEvent = "FileChanged"
	HookWorktreeCreate     HookEvent = "WorktreeCreate"
	HookWorktreeRemove     HookEvent = "WorktreeRemove"
)

// HookCallback is a function invoked when a hook fires.
// It receives the hook input and returns a hook output.
type HookCallback func(ctx context.Context, input HookInput, toolUseID string) (HookOutput, error)

// HookMatcher defines which events trigger which callbacks.
type HookMatcher struct {
	Matcher string         // Regex pattern to match (e.g., tool name pattern "Bash|Write|Edit")
	Hooks   []HookCallback // Callbacks to invoke when matched
	Timeout time.Duration  // Timeout for hook execution (default 60s if zero)
}

// HookInput is the data passed to a hook callback.
// Fields are populated based on the hook event type.
type HookInput struct {
	// Common fields (present on all events).
	HookEventName  string `json:"hook_event_name"`
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	PermissionMode string `json:"permission_mode,omitempty"`
	AgentID        string `json:"agent_id,omitempty"`
	AgentType      string `json:"agent_type,omitempty"`

	// SessionStart fields.
	Source string `json:"source,omitempty"` // "startup", "resume", "clear", "compact"

	// SessionEnd fields.
	ExitReason string `json:"exit_reason,omitempty"` // "completed", "interrupted", "error"

	// Tool-specific fields (PreToolUse, PostToolUse, PostToolUseFailure).
	ToolName   string         `json:"tool_name,omitempty"`
	ToolInput  map[string]any `json:"tool_input,omitempty"`
	ToolResult any            `json:"tool_result,omitempty"` // PostToolUse only
	ToolUseID  string         `json:"tool_use_id,omitempty"`

	// UserPromptSubmit fields.
	Prompt string `json:"prompt,omitempty"`

	// Stop / SubagentStop fields.
	StopHookActive      bool   `json:"stop_hook_active,omitempty"`
	AgentTranscriptPath string `json:"agent_transcript_path,omitempty"` // SubagentStop only
	LastAssistantMessage string `json:"last_assistant_message,omitempty"` // SubagentStop only

	// PreCompact fields.
	Trigger            string `json:"trigger,omitempty"`             // "manual" or "auto"
	CustomInstructions string `json:"custom_instructions,omitempty"`

	// Notification fields.
	Message          string `json:"message,omitempty"`
	Title            string `json:"title,omitempty"`
	NotificationType string `json:"notification_type,omitempty"`

	// PermissionRequest fields.
	PermissionSuggestions []map[string]any `json:"permission_suggestions,omitempty"`

	// PostToolUseFailure fields.
	Error       string `json:"error,omitempty"`
	IsInterrupt bool   `json:"is_interrupt,omitempty"`

	// TeammateIdle fields.
	IdleDurationMs int64  `json:"idle_duration_ms,omitempty"`
	TeammateID     string `json:"teammate_id,omitempty"`

	// TaskCompleted fields.
	TaskID     string `json:"task_id,omitempty"`
	TaskStatus string `json:"task_status,omitempty"` // "completed", "failed", "stopped"
	OutputFile string `json:"output_file,omitempty"`

	// ConfigChange fields.
	ConfigPath   string `json:"config_path,omitempty"`   // Path to changed config file
	ChangeType   string `json:"change_type,omitempty"`   // "created", "modified", "deleted"

	// WorktreeCreate / WorktreeRemove fields.
	WorktreePath   string `json:"worktree_path,omitempty"`
	WorktreeBranch string `json:"worktree_branch,omitempty"`

	// StopFailure fields.
	FailureReason string `json:"failure_reason,omitempty"` // Why the stop failed

	// PostCompact fields.
	TokensSaved int `json:"tokens_saved,omitempty"` // Tokens recovered by compaction

	// InstructionsLoaded fields.
	InstructionsPath string `json:"instructions_path,omitempty"` // Path to loaded CLAUDE.md file

	// Elicitation fields.
	ElicitationID string         `json:"elicitation_id,omitempty"` // MCP elicitation request ID
	ServerName    string         `json:"server_name,omitempty"`    // MCP server requesting input
	Schema        map[string]any `json:"schema,omitempty"`         // Input schema for elicitation

	// ElicitationResult fields.
	ElicitationResponse map[string]any `json:"elicitation_response,omitempty"` // User's response to elicitation

	// CwdChanged fields.
	OldCwd string `json:"old_cwd,omitempty"` // Previous working directory
	NewCwd string `json:"new_cwd,omitempty"` // New working directory

	// FileChanged fields.
	FilePath       string `json:"file_path,omitempty"`       // Path of changed file
	FileChangeType string `json:"file_change_type,omitempty"` // "created", "modified", "deleted"

	// PermissionDenied fields.
	DeniedToolName string `json:"denied_tool_name,omitempty"` // Tool that was denied
	DeniedReason   string `json:"denied_reason,omitempty"`    // Reason for denial
}

// HookOutput is the response from a hook callback.
type HookOutput struct {
	Continue       bool   `json:"continue,omitempty"`       // Continue execution after hook
	SuppressOutput bool   `json:"suppressOutput,omitempty"` // Suppress output from this hook
	SystemMessage  string `json:"systemMessage,omitempty"`  // Inject a system message into context
	Reason         string `json:"reason,omitempty"`         // Debug reason for Claude
	Decision       string `json:"decision,omitempty"`       // "block" for certain hooks
	StopReason     string `json:"stopReason,omitempty"`     // Reason for stopping execution

	// Async output — return immediately without blocking the agent loop.
	// Use for fire-and-forget side effects (logging, webhooks).
	Async        bool `json:"async,omitempty"`        // Fire-and-forget mode
	AsyncTimeout int  `json:"asyncTimeout,omitempty"` // Timeout in milliseconds

	// HookSpecificOutput contains event-specific response data.
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput contains event-specific fields in a hook response.
type HookSpecificOutput struct {
	HookEventName            string         `json:"hookEventName"`
	PermissionDecision       string         `json:"permissionDecision,omitempty"`       // "allow", "deny", "ask"
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"` // Human-readable reason
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`             // Modify tool input (PreToolUse allow)
	AdditionalContext        string         `json:"additionalContext,omitempty"`        // Extra context for Claude
	UpdatedMCPToolOutput     map[string]any `json:"updatedMCPToolOutput,omitempty"`     // Modify MCP tool output (PostToolUse)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add hook.go
git commit -m "feat: add hook event types, matcher, callback, and I/O structs"
```

---

### Task 6: MCP server types

**Files:**
- Create: `mcp.go`

- [ ] **Step 1: Write MCP server types**

Create `mcp.go`:

```go
package agentsdk

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
)

// McpServerConfig is a union of MCP server configuration types.
// Exactly one field should be set.
type McpServerConfig struct {
	// Stdio launches an MCP server as a subprocess.
	Stdio *McpStdioServerConfig `json:"stdio,omitempty"`
	// SSE connects to an MCP server over Server-Sent Events.
	SSE *McpSSEServerConfig `json:"sse,omitempty"`
	// HTTP connects to an MCP server over HTTP.
	HTTP *McpHTTPServerConfig `json:"http,omitempty"`
	// Proxy connects through the claude.ai proxy (for managed MCP servers).
	Proxy *McpProxyServerConfig `json:"proxy,omitempty"`
	// SDK is an in-process MCP server defined in Go.
	SDK *McpSdkServer `json:"-"` // Not serialized — handled by control protocol
}

// MarshalJSON implements custom marshalling for the union.
func (c McpServerConfig) MarshalJSON() ([]byte, error) {
	if c.Stdio != nil {
		return json.Marshal(struct {
			Type string `json:"type"`
			*McpStdioServerConfig
		}{"stdio", c.Stdio})
	}
	if c.SSE != nil {
		return json.Marshal(struct {
			Type string `json:"type"`
			*McpSSEServerConfig
		}{"sse", c.SSE})
	}
	if c.HTTP != nil {
		return json.Marshal(struct {
			Type string `json:"type"`
			*McpHTTPServerConfig
		}{"http", c.HTTP})
	}
	if c.Proxy != nil {
		return json.Marshal(struct {
			Type string `json:"type"`
			*McpProxyServerConfig
		}{"claude_ai_proxy", c.Proxy})
	}
	return []byte("{}"), nil
}

// McpStdioServerConfig launches an MCP server as a child process.
type McpStdioServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// McpSSEServerConfig connects to an MCP server via Server-Sent Events.
type McpSSEServerConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// McpHTTPServerConfig connects to an MCP server via HTTP.
type McpHTTPServerConfig struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// McpProxyServerConfig connects through the claude.ai proxy for managed MCP servers.
type McpProxyServerConfig struct {
	ServerID string `json:"serverId,omitempty"` // Managed server identifier
}

// McpSdkServer is an in-process MCP server defined in Go code.
// Tools are registered via the Tool() helper function.
type McpSdkServer struct {
	Name    string
	Version string
	Tools   []McpTool
}

// McpTool defines a single tool exposed by an SDK MCP server.
type McpTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON Schema for the tool input
	Handler     func(ctx context.Context, input json.RawMessage) (*McpToolResult, error)
	Annotations *ToolAnnotations // Optional MCP tool annotations
}

// ToolAnnotations provides metadata hints about a tool's behavior.
type ToolAnnotations struct {
	ReadOnly    bool `json:"readOnly,omitempty"`    // Tool only reads data, no side effects
	Destructive bool `json:"destructive,omitempty"` // Tool may perform destructive operations
	OpenWorld   bool `json:"openWorld,omitempty"`   // Tool interacts with external systems
}

// McpToolResult is the response from an MCP tool handler.
type McpToolResult struct {
	Content []McpToolContent `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

// McpToolContent is a content item in an MCP tool result.
type McpToolContent struct {
	Type string `json:"type"` // "text" or "image"
	Text string `json:"text,omitempty"`
}

// McpServerStatus reports the connection status of an MCP server.
type McpServerStatus struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"` // "connected", "failed", "pending", "needs-auth", "needs-approval", "disabled"
	ServerInfo *McpServerInfo    `json:"serverInfo,omitempty"`
	Error      string            `json:"error,omitempty"`
	Config     map[string]any    `json:"config,omitempty"` // Server configuration snapshot
	Scope      string            `json:"scope,omitempty"`  // "user", "project", "local"
	Tools      []McpToolInfo     `json:"tools,omitempty"`
}

// McpServerInfo identifies a connected MCP server.
type McpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// McpToolInfo describes a tool exposed by an MCP server.
type McpToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Annotations *ToolAnnotations `json:"annotations,omitempty"`
}

// ToolOption configures an McpTool created by the Tool() helper.
type ToolOption func(*McpTool)

// WithAnnotations sets MCP tool annotations on a tool.
func WithAnnotations(annotations ToolAnnotations) ToolOption {
	return func(t *McpTool) { t.Annotations = &annotations }
}

// Tool creates a typed McpTool with automatic JSON schema generation from the
// type parameter T. The handler receives a decoded T value.
func Tool[T any](name, description string, handler func(context.Context, T) (*McpToolResult, error), opts ...ToolOption) McpTool {
	// Generate JSON schema from T by marshalling a zero value.
	// For production use, a proper schema generator should be used.
	var zero T
	schema, _ := json.Marshal(schemaFromType(zero))

	tool := McpTool{
		Name:        name,
		Description: description,
		InputSchema: schema,
		Handler: func(ctx context.Context, input json.RawMessage) (*McpToolResult, error) {
			var v T
			if err := json.Unmarshal(input, &v); err != nil {
				return nil, err
			}
			return handler(ctx, v)
		},
	}
	for _, opt := range opts {
		opt(&tool)
	}
	return tool
}

// CreateSdkMcpServer is a convenience constructor for McpSdkServer.
func CreateSdkMcpServer(name, version string, tools ...McpTool) *McpSdkServer {
	return &McpSdkServer{
		Name:    name,
		Version: version,
		Tools:   tools,
	}
}

// schemaFromType generates a JSON schema object from a Go struct using reflection.
// Supports struct fields with json tags. Nested structs are expanded recursively.
func schemaFromType(v any) map[string]any {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return map[string]any{"type": "object"}
	}

	properties := map[string]any{}
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		name := field.Name
		if tag := field.Tag.Get("json"); tag != "" {
			parts := strings.Split(tag, ",")
			if parts[0] == "-" {
				continue
			}
			if parts[0] != "" {
				name = parts[0]
			}
			// Fields without omitempty are required.
			hasOmitempty := false
			for _, p := range parts[1:] {
				if p == "omitempty" {
					hasOmitempty = true
				}
			}
			if !hasOmitempty {
				required = append(required, name)
			}
		} else {
			required = append(required, name)
		}

		properties[name] = goTypeToJSONSchema(field.Type)
	}

	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// goTypeToJSONSchema maps Go types to JSON Schema type strings.
func goTypeToJSONSchema(t reflect.Type) map[string]any {
	// Dereference pointers.
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return map[string]any{"type": "integer"}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": goTypeToJSONSchema(t.Elem())}
	case reflect.Map:
		if t.Key().Kind() == reflect.String {
			return map[string]any{"type": "object", "additionalProperties": goTypeToJSONSchema(t.Elem())}
		}
		return map[string]any{"type": "object"}
	case reflect.Struct:
		return schemaFromType(reflect.New(t).Elem().Interface())
	case reflect.Interface:
		return map[string]any{} // any type — no constraint
	default:
		return map[string]any{"type": "object"}
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add mcp.go
git commit -m "feat: add MCP server config types (stdio, SSE, HTTP, in-process SDK)"
```

---

### Task 7: Options (functional options pattern)

**Files:**
- Create: `options.go`

- [ ] **Step 1: Write the queryConfig struct and all option functions**

Create `options.go`:

```go
package agentsdk

import "time"

// PermissionMode controls how tool permissions are handled.
type PermissionMode string

const (
	PermissionDefault         PermissionMode = "default"
	PermissionAcceptEdits     PermissionMode = "acceptEdits"
	PermissionPlan            PermissionMode = "plan"
	PermissionBypassAll       PermissionMode = "bypassPermissions"
	PermissionDontAsk         PermissionMode = "dontAsk"
)

// PreviewFormat controls how AskUserQuestion previews are rendered.
type PreviewFormat string

const (
	PreviewFormatMarkdown PreviewFormat = "markdown"
	PreviewFormatHTML     PreviewFormat = "html"
)

// ToolConfig configures tool-specific behavior.
type ToolConfig struct {
	AskUserQuestion *AskUserQuestionConfig `json:"askUserQuestion,omitempty"`
}

// AskUserQuestionConfig configures the AskUserQuestion tool.
type AskUserQuestionConfig struct {
	PreviewFormat PreviewFormat `json:"previewFormat,omitempty"` // "markdown" or "html"
}

// Effort controls how hard Claude tries (maps to --effort flag).
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortMax    Effort = "max"
)

// ThinkingConfig configures extended thinking.
type ThinkingConfig struct {
	Type         string `json:"type"`                    // "enabled", "disabled", or "adaptive" (default for compatible models)
	BudgetTokens int    `json:"budget_tokens,omitempty"` // Max tokens for thinking (only for "enabled")
}

// SettingSource is a path to a Claude Code settings file.
type SettingSource string

// PluginConfig defines a plugin to load.
type PluginConfig struct {
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// TaskBudget configures token budgets for background tasks.
type TaskBudget struct {
	Total int `json:"total"`
}

// SandboxSettings configures filesystem and network isolation.
type SandboxSettings struct {
	Enabled                    bool                      `json:"enabled"`
	AutoAllowBashIfSandboxed   bool                      `json:"autoAllowBashIfSandboxed,omitempty"`
	ExcludedCommands           []string                  `json:"excludedCommands,omitempty"`
	AllowUnsandboxedCommands   bool                      `json:"allowUnsandboxedCommands,omitempty"`
	Network                    *SandboxNetworkConfig     `json:"network,omitempty"`
	IgnoreViolations           *SandboxIgnoreViolations  `json:"ignoreViolations,omitempty"`
	EnableWeakerNestedSandbox  bool                      `json:"enableWeakerNestedSandbox,omitempty"`
}

// SandboxNetworkConfig configures network isolation for sandboxed sessions.
type SandboxNetworkConfig struct {
	AllowUnixSockets    []string `json:"allowUnixSockets,omitempty"`
	AllowAllUnixSockets bool     `json:"allowAllUnixSockets,omitempty"`
	AllowLocalBinding   bool     `json:"allowLocalBinding,omitempty"`
	HTTPProxyPort       int      `json:"httpProxyPort,omitempty"`
	SOCKSProxyPort      int      `json:"socksProxyPort,omitempty"`
}

// SandboxIgnoreViolations configures which sandbox violation patterns to ignore.
type SandboxIgnoreViolations struct {
	File    []string `json:"file,omitempty"`    // File path patterns to ignore
	Network []string `json:"network,omitempty"` // Network address patterns to ignore
}

// AgentDefinition defines a subagent configuration.
type AgentDefinition struct {
	Description     string   `json:"description"`
	Prompt          string   `json:"prompt"`
	Tools           []string `json:"tools,omitempty"`
	DisallowedTools []string `json:"disallowedTools,omitempty"`
	Model           string   `json:"model,omitempty"`          // "sonnet", "opus", "haiku", "inherit"
	McpServers      []string `json:"mcpServers,omitempty"`     // Reference parent MCP server names
	Skills          []string `json:"skills,omitempty"`         // Preload specialized skills
	MaxTurns        int      `json:"maxTurns,omitempty"`
	InitialPrompt   string   `json:"initialPrompt,omitempty"` // Prompt sent when agent starts
	Memory          string   `json:"memory,omitempty"`         // "user", "project", or "local"
}

// PermissionResult is the response from a CanUseToolFunc callback.
type PermissionResult struct {
	Behavior           string             `json:"behavior"`                      // "allow", "deny", or "ask" (delegate to CLI default prompt)
	Message            string             `json:"message,omitempty"`             // Reason message (deny/ask)
	Interrupt          bool               `json:"interrupt,omitempty"`           // Stop execution (deny only)
	UpdatedInput       map[string]any     `json:"updatedInput,omitempty"`       // Modified tool input (allow only)
	UpdatedPermissions []PermissionUpdate `json:"updatedPermissions,omitempty"` // Dynamic permission rule changes (allow only)
	Suggestions        []PermissionUpdate `json:"suggestions,omitempty"`        // Suggested permission updates for the user
}

// PermissionUpdate describes a dynamic change to permission rules.
type PermissionUpdate struct {
	Type        string               `json:"type"`                  // "addRules", "replaceRules", "removeRules", "setMode", "addDirectories", "removeDirectories"
	Rules       []PermissionRule     `json:"rules,omitempty"`
	Behavior    string               `json:"behavior,omitempty"`    // "allow", "deny", "ask"
	Mode        PermissionMode       `json:"mode,omitempty"`        // For "setMode" type
	Directories []string             `json:"directories,omitempty"` // For "addDirectories"/"removeDirectories"
	Destination string               `json:"destination,omitempty"` // "userSettings", "projectSettings", "localSettings", "session"
}

// PermissionRule defines a single permission rule pattern.
type PermissionRule struct {
	Tool    string `json:"tool"`              // Tool name or pattern
	Pattern string `json:"pattern,omitempty"` // Optional additional matching pattern
}

// ToolPermissionContext provides context for permission decisions.
type ToolPermissionContext struct {
	ToolUseID   string             `json:"tool_use_id"`
	AgentID     string             `json:"agent_id,omitempty"`
	Suggestions []PermissionUpdate `json:"suggestions,omitempty"` // CLI-provided permission update suggestions
}

// CanUseToolFunc is called when claude requests permission to use a tool.
// Return a PermissionResult to allow or deny the tool use.
type CanUseToolFunc func(toolName string, toolInput map[string]any, ctx ToolPermissionContext) (*PermissionResult, error)

// queryConfig holds the resolved configuration for a Query or Client.
type queryConfig struct {
	// Model & Reasoning
	model         string
	fallbackModel string
	thinking      *ThinkingConfig
	effort        Effort

	// Tools & Permissions
	allowedTools                    []string
	disallowedTools                 []string
	tools                           []string
	toolsPreset                     bool
	permissionMode                  PermissionMode
	bypassPermissions               bool
	allowDangerouslySkipPermissions bool
	canUseTool                      CanUseToolFunc
	permissionPromptToolName        string

	// System Prompt
	systemPrompt       string
	systemPromptPreset string // append string for preset mode
	systemPromptFile   string

	// Conversation
	resumeSessionID string
	continueSession bool
	sessionID       string
	forkSession     bool
	maxTurns        int
	maxBudgetUSD    float64

	// Session persistence
	persistSession *bool // nil = default (true), false = in-memory only

	// Environment
	cwd                  string
	env                  map[string]string
	additionalDirs       []string
	cliPath              string
	settingSources       []SettingSource
	apiKey               string

	// Tool config
	toolConfig *ToolConfig

	// Extensions
	mcpServers map[string]McpServerConfig
	agents     map[string]AgentDefinition
	hooks      map[HookEvent][]HookMatcher
	plugins    []PluginConfig

	// Budget
	taskBudget *TaskBudget

	// Sandbox
	sandbox *SandboxSettings

	// Identity
	user string

	// Extra CLI arguments (escape hatch)
	extraArgs []string

	// Output
	outputFormat           map[string]any // JSON schema for structured output
	includePartialMessages bool

	// File checkpointing
	fileCheckpointing bool

	// Debugging
	stderrFunc    func(string)
	debug         bool
	debugFile     string
	maxBufferSize int

	// Beta features
	betas []string

	// Timeouts
	processTimeout time.Duration
}

// QueryOption configures a Query or Client.
type QueryOption func(*queryConfig)

// --- Model & Reasoning ---

// WithModel sets the Claude model to use (e.g., "sonnet", "opus", "haiku").
func WithModel(model string) QueryOption {
	return func(c *queryConfig) { c.model = model }
}

// WithFallbackModel sets a fallback model if the primary is unavailable.
func WithFallbackModel(model string) QueryOption {
	return func(c *queryConfig) { c.fallbackModel = model }
}

// WithThinking enables extended thinking with the given configuration.
func WithThinking(config ThinkingConfig) QueryOption {
	return func(c *queryConfig) { c.thinking = &config }
}

// WithEffort sets the effort level for reasoning.
func WithEffort(effort Effort) QueryOption {
	return func(c *queryConfig) { c.effort = effort }
}

// --- Tools & Permissions ---

// WithAllowedTools sets the list of tools the model may use.
func WithAllowedTools(tools ...string) QueryOption {
	return func(c *queryConfig) { c.allowedTools = tools }
}

// WithDisallowedTools sets tools the model may NOT use.
func WithDisallowedTools(tools ...string) QueryOption {
	return func(c *queryConfig) { c.disallowedTools = tools }
}

// WithTools sets explicit tool names to enable.
func WithTools(tools ...string) QueryOption {
	return func(c *queryConfig) { c.tools = tools }
}

// WithToolsPreset uses the Claude Code default tool set.
func WithToolsPreset() QueryOption {
	return func(c *queryConfig) { c.toolsPreset = true }
}

// WithPermissionMode sets the tool permission mode.
func WithPermissionMode(mode PermissionMode) QueryOption {
	return func(c *queryConfig) { c.permissionMode = mode }
}

// WithBypassPermissions bypasses all permission checks.
// Requires WithAllowDangerouslySkipPermissions to be set.
func WithBypassPermissions() QueryOption {
	return func(c *queryConfig) { c.bypassPermissions = true }
}

// WithAllowDangerouslySkipPermissions acknowledges the risk of bypassing permissions.
func WithAllowDangerouslySkipPermissions() QueryOption {
	return func(c *queryConfig) { c.allowDangerouslySkipPermissions = true }
}

// WithCanUseTool sets a callback for runtime permission decisions.
func WithCanUseTool(fn CanUseToolFunc) QueryOption {
	return func(c *queryConfig) { c.canUseTool = fn }
}

// WithPermissionPromptToolName sets a custom MCP tool name for permission prompts.
// The named tool will be called instead of the default CLI permission prompt.
func WithPermissionPromptToolName(name string) QueryOption {
	return func(c *queryConfig) { c.permissionPromptToolName = name }
}

// --- System Prompt ---

// WithSystemPrompt sets a custom system prompt (replaces default).
func WithSystemPrompt(prompt string) QueryOption {
	return func(c *queryConfig) { c.systemPrompt = prompt }
}

// WithSystemPromptPreset uses the Claude Code default prompt with appended text.
func WithSystemPromptPreset(append string) QueryOption {
	return func(c *queryConfig) { c.systemPromptPreset = append }
}

// WithSystemPromptFile loads the system prompt from a file path.
func WithSystemPromptFile(path string) QueryOption {
	return func(c *queryConfig) { c.systemPromptFile = path }
}

// --- Conversation ---

// WithResume resumes an existing session by ID.
func WithResume(sessionID string) QueryOption {
	return func(c *queryConfig) { c.resumeSessionID = sessionID }
}

// WithContinue continues the most recent session.
func WithContinue() QueryOption {
	return func(c *queryConfig) { c.continueSession = true }
}

// WithSessionID sets a specific session ID for the conversation.
func WithSessionID(id string) QueryOption {
	return func(c *queryConfig) { c.sessionID = id }
}

// WithForkSession forks the resumed session instead of continuing in-place.
func WithForkSession() QueryOption {
	return func(c *queryConfig) { c.forkSession = true }
}

// WithMaxTurns limits the number of agentic turns.
func WithMaxTurns(n int) QueryOption {
	return func(c *queryConfig) { c.maxTurns = n }
}

// WithMaxBudgetUSD sets a maximum spend limit in USD.
func WithMaxBudgetUSD(budget float64) QueryOption {
	return func(c *queryConfig) { c.maxBudgetUSD = budget }
}

// --- Environment ---

// WithCwd sets the working directory for the claude process.
func WithCwd(dir string) QueryOption {
	return func(c *queryConfig) { c.cwd = dir }
}

// WithEnv sets additional environment variables for the claude process.
func WithEnv(env map[string]string) QueryOption {
	return func(c *queryConfig) { c.env = env }
}

// WithAdditionalDirectories adds directories to the file search scope.
func WithAdditionalDirectories(dirs ...string) QueryOption {
	return func(c *queryConfig) { c.additionalDirs = dirs }
}

// WithCLIPath sets an explicit path to the claude CLI binary.
func WithCLIPath(path string) QueryOption {
	return func(c *queryConfig) { c.cliPath = path }
}

// WithAPIKey sets the Anthropic API key for the claude process.
// This is passed via the ANTHROPIC_API_KEY environment variable.
func WithAPIKey(key string) QueryOption {
	return func(c *queryConfig) { c.apiKey = key }
}

// WithSettingSources adds Claude Code settings files.
func WithSettingSources(sources ...SettingSource) QueryOption {
	return func(c *queryConfig) { c.settingSources = sources }
}

// --- Extensions ---

// WithMcpServers configures MCP servers for the session.
func WithMcpServers(servers map[string]McpServerConfig) QueryOption {
	return func(c *queryConfig) { c.mcpServers = servers }
}

// WithAgents configures subagent definitions.
func WithAgents(agents map[string]AgentDefinition) QueryOption {
	return func(c *queryConfig) { c.agents = agents }
}

// WithHooks configures hook callbacks for agent lifecycle events.
func WithHooks(hooks map[HookEvent][]HookMatcher) QueryOption {
	return func(c *queryConfig) { c.hooks = hooks }
}

// WithPlugins configures plugins to load.
func WithPlugins(plugins ...PluginConfig) QueryOption {
	return func(c *queryConfig) { c.plugins = plugins }
}

// --- Output ---

// WithOutputFormat sets a JSON schema for structured output.
func WithOutputFormat(schema map[string]any) QueryOption {
	return func(c *queryConfig) { c.outputFormat = schema }
}

// WithIncludePartialMessages includes streaming partial messages in the output.
func WithIncludePartialMessages() QueryOption {
	return func(c *queryConfig) { c.includePartialMessages = true }
}

// --- Tool Config ---

// WithToolConfig sets tool-specific configuration (e.g., AskUserQuestion preview format).
func WithToolConfig(config ToolConfig) QueryOption {
	return func(c *queryConfig) { c.toolConfig = &config }
}

// --- Debugging ---

// WithStderr provides a callback for claude process stderr output.
func WithStderr(fn func(string)) QueryOption {
	return func(c *queryConfig) { c.stderrFunc = fn }
}

// WithDebug enables debug mode.
func WithDebug() QueryOption {
	return func(c *queryConfig) { c.debug = true }
}

// WithDebugFile writes debug output to a file.
func WithDebugFile(path string) QueryOption {
	return func(c *queryConfig) { c.debugFile = path }
}

// WithMaxBufferSize sets the maximum stdout buffer size in bytes.
func WithMaxBufferSize(size int) QueryOption {
	return func(c *queryConfig) { c.maxBufferSize = size }
}

// --- Session Persistence ---

// WithPersistSession controls whether the session is persisted to disk.
// Default is true. Set to false for in-memory-only sessions.
func WithPersistSession(persist bool) QueryOption {
	return func(c *queryConfig) { c.persistSession = &persist }
}

// --- Beta Features ---

// WithBetas enables beta feature flags (e.g., "context-1m-2025-08-07").
func WithBetas(betas ...string) QueryOption {
	return func(c *queryConfig) { c.betas = betas }
}

// --- Budget ---

// WithTaskBudget sets token budgets for background tasks.
func WithTaskBudget(budget TaskBudget) QueryOption {
	return func(c *queryConfig) { c.taskBudget = &budget }
}

// --- Sandbox ---

// WithSandbox configures filesystem and network isolation.
func WithSandbox(settings SandboxSettings) QueryOption {
	return func(c *queryConfig) { c.sandbox = &settings }
}

// --- Identity ---

// WithUser sets the user identifier for the session.
func WithUser(user string) QueryOption {
	return func(c *queryConfig) { c.user = user }
}

// --- File Checkpointing ---

// WithFileCheckpointing enables file change tracking for rewind support.
func WithFileCheckpointing() QueryOption {
	return func(c *queryConfig) { c.fileCheckpointing = true }
}

// --- Timeouts ---

// WithProcessTimeout sets the maximum duration for the claude process.
func WithProcessTimeout(d time.Duration) QueryOption {
	return func(c *queryConfig) { c.processTimeout = d }
}

// --- Extra CLI Arguments ---

// WithExtraArgs passes additional CLI arguments directly to the claude process.
// This is an escape hatch for features not yet covered by typed options.
func WithExtraArgs(args ...string) QueryOption {
	return func(c *queryConfig) { c.extraArgs = args }
}

// applyDefaults fills in default values for unset fields.
func (c *queryConfig) applyDefaults() {
	if c.maxBufferSize <= 0 {
		c.maxBufferSize = 1 << 20 // 1MB
	}
	if c.processTimeout <= 0 {
		c.processTimeout = 30 * time.Minute
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add options.go
git commit -m "feat: add QueryOption functional options and queryConfig struct

Covers model, reasoning, tools, permissions, system prompt, conversation,
environment, extensions, output, and debugging configuration."
```

---

### Task 8: CLI binary discovery

**Files:**
- Create: `internal/clilookup/clilookup.go`
- Create: `internal/clilookup/clilookup_test.go`

- [ ] **Step 1: Write failing test for CLI lookup**

Create `internal/clilookup/clilookup_test.go`:

```go
package clilookup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindCLI_ExplicitPath(t *testing.T) {
	// Create a temp file to act as the CLI binary.
	dir := t.TempDir()
	fakeCLI := filepath.Join(dir, "claude")
	if err := os.WriteFile(fakeCLI, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	path, err := FindCLI(fakeCLI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != fakeCLI {
		t.Errorf("expected %q, got %q", fakeCLI, path)
	}
}

func TestFindCLI_ExplicitPathNotFound(t *testing.T) {
	_, err := FindCLI("/nonexistent/path/claude")
	if err == nil {
		t.Fatal("expected error for nonexistent explicit path")
	}
}

func TestFindCLI_SearchPaths(t *testing.T) {
	// Test that FindCLI returns an error listing searched paths when nothing is found.
	_, err := FindCLI("")
	if err == nil {
		// claude might actually be installed; skip the assertion.
		t.Skip("claude CLI found on system, cannot test not-found path")
	}
	notFound, ok := err.(*NotFoundError)
	if !ok {
		t.Fatalf("expected *NotFoundError, got %T", err)
	}
	if len(notFound.SearchedPaths) == 0 {
		t.Error("SearchedPaths should not be empty")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/clilookup/ -v`
Expected: Compilation error — `FindCLI`, `NotFoundError` not defined.

- [ ] **Step 3: Implement CLI lookup**

Create `internal/clilookup/clilookup.go`:

```go
package clilookup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// NotFoundError is returned when the claude CLI binary cannot be found.
type NotFoundError struct {
	SearchedPaths []string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("claude CLI not found (searched: %s)", strings.Join(e.SearchedPaths, ", "))
}

// FindCLI locates the claude CLI binary.
// If explicitPath is non-empty, it is used directly (must exist and be executable).
// Otherwise, searches standard locations and $PATH.
func FindCLI(explicitPath string) (string, error) {
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return "", &NotFoundError{SearchedPaths: []string{explicitPath}}
		}
		return explicitPath, nil
	}

	var searched []string

	// 1. Check $PATH via exec.LookPath.
	if path, err := exec.LookPath("claude"); err == nil {
		return path, nil
	}
	searched = append(searched, "$PATH")

	// 2. Standard installation locations.
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".claude", "local", "claude"),
		"/usr/local/bin/claude",
		"/usr/bin/claude",
	}

	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			"/opt/homebrew/bin/claude",
			filepath.Join(home, ".nvm", "versions", "node"), // Will be expanded below
		)
	}

	// On Windows, also check .exe variants.
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			candidates = append(candidates, filepath.Join(appData, "npm", "claude.cmd"))
		}
	}

	// 3. Check npm global installations.
	npmGlobal := npmGlobalBinDir()
	if npmGlobal != "" {
		candidates = append(candidates, filepath.Join(npmGlobal, "claude"))
	}

	for _, path := range candidates {
		searched = append(searched, path)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", &NotFoundError{SearchedPaths: searched}
}

// npmGlobalBinDir returns the npm global bin directory, or empty string.
// Uses "npm config get prefix" since "npm bin -g" is deprecated since npm 9.
func npmGlobalBinDir() string {
	out, err := exec.Command("npm", "config", "get", "prefix").Output()
	if err != nil {
		return ""
	}
	prefix := strings.TrimSpace(string(out))
	if prefix == "" {
		return ""
	}
	if runtime.GOOS == "windows" {
		return prefix // npm installs binaries directly in prefix on Windows
	}
	return filepath.Join(prefix, "bin")
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/clilookup/ -v`
Expected: All PASS (or skip on systems where claude is installed).

- [ ] **Step 5: Commit**

```bash
git add internal/clilookup/
git commit -m "feat: add claude CLI binary discovery with standard path search"
```

---

### Task 9: Transport interface

**Files:**
- Create: `internal/transport/transport.go`

- [ ] **Step 1: Write the Transport interface**

Create `internal/transport/transport.go`:

```go
package transport

import (
	"context"
	"encoding/json"
)

// Transport abstracts the communication channel with the claude process.
// The default implementation spawns a subprocess, but custom implementations
// can connect to remote claude instances.
type Transport interface {
	// Connect starts the transport and begins reading messages.
	Connect(ctx context.Context) error

	// Write sends a string (NDJSON line) to the claude process stdin.
	Write(data string) error

	// ReadMessages returns a channel that yields raw JSON messages from stdout.
	// The channel is closed when the process exits or the transport is closed.
	ReadMessages() <-chan json.RawMessage

	// Close shuts down the transport and releases resources.
	// It performs graceful shutdown: close stdin → wait → SIGTERM → wait → SIGKILL.
	Close() error

	// EndInput closes the stdin pipe, signaling EOF to the claude process.
	EndInput() error

	// IsReady returns true if the transport is connected and ready.
	IsReady() bool

	// ExitError returns the process exit error, or nil if still running or exited cleanly.
	ExitError() error
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/transport/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/transport/transport.go
git commit -m "feat: add Transport interface for claude process communication"
```

---

### Task 10: SubprocessTransport implementation

**Files:**
- Create: `internal/transport/subprocess.go`

This is the core of the SDK: spawning `claude` with the right flags, reading NDJSON from stdout, writing to stdin, and handling graceful shutdown.

- [ ] **Step 1: Write SubprocessTransport**

Create `internal/transport/subprocess.go`:

```go
package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// SubprocessConfig holds configuration for spawning the claude process.
type SubprocessConfig struct {
	CLIPath       string
	Args          []string          // Additional CLI arguments
	Cwd           string            // Working directory
	Env           map[string]string // Additional environment variables
	MaxBufferSize int               // Max line size for stdout scanner (bytes)
	StderrFunc    func(string)      // Callback for stderr lines
}

// SubprocessTransport spawns a claude CLI subprocess and communicates
// via stdin (write) and stdout (read) using NDJSON.
type SubprocessTransport struct {
	config  SubprocessConfig
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	msgCh   chan json.RawMessage
	ready   bool
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	exitErr error // Process exit error (non-zero exit code)
}

// NewSubprocessTransport creates a new SubprocessTransport.
func NewSubprocessTransport(config SubprocessConfig) *SubprocessTransport {
	if config.MaxBufferSize <= 0 {
		config.MaxBufferSize = 1 << 20 // 1MB default
	}
	return &SubprocessTransport{
		config: config,
		msgCh:  make(chan json.RawMessage, 64),
		done:   make(chan struct{}),
	}
}

// Connect spawns the claude subprocess and begins reading messages.
func (t *SubprocessTransport) Connect(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	// Build command arguments.
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
	}
	args = append(args, t.config.Args...)

	t.cmd = exec.CommandContext(ctx, t.config.CLIPath, args...)

	// Set working directory.
	if t.config.Cwd != "" {
		t.cmd.Dir = t.config.Cwd
	}

	// Set environment: inherit current env + additions.
	t.cmd.Env = os.Environ()
	for k, v := range t.config.Env {
		t.cmd.Env = append(t.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set up pipes.
	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdin pipe: %w", err)
	}
	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stdout pipe: %w", err)
	}
	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// Start the process.
	if err := t.cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("start claude: %w", err)
	}

	t.mu.Lock()
	t.ready = true
	t.mu.Unlock()

	// Read stdout in background.
	go t.readStdout()

	// Read stderr in background.
	go t.readStderr()

	// Wait for process exit in background.
	go func() {
		err := t.cmd.Wait()
		t.mu.Lock()
		t.ready = false
		t.exitErr = err
		t.mu.Unlock()
		close(t.done)
	}()

	return nil
}

// readStdout reads NDJSON lines from stdout and sends them to msgCh.
func (t *SubprocessTransport) readStdout() {
	scanner := bufio.NewScanner(t.stdout)
	scanner.Buffer(make([]byte, 0, t.config.MaxBufferSize), t.config.MaxBufferSize)

	for scanner.Scan() {
		line := scanner.Text()
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Validate it's JSON before sending.
		if !json.Valid([]byte(line)) {
			continue
		}
		t.msgCh <- json.RawMessage(line)
	}

	close(t.msgCh)
}

// readStderr reads stderr lines and calls the stderr callback.
func (t *SubprocessTransport) readStderr() {
	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if t.config.StderrFunc != nil {
			t.config.StderrFunc(line)
		}
	}
}

// Write sends a line to the claude process stdin, terminated with newline.
func (t *SubprocessTransport) Write(data string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ready || t.stdin == nil {
		return fmt.Errorf("transport not connected")
	}
	if !strings.HasSuffix(data, "\n") {
		data += "\n"
	}
	_, err := io.WriteString(t.stdin, data)
	return err
}

// ReadMessages returns the channel of parsed JSON messages from stdout.
func (t *SubprocessTransport) ReadMessages() <-chan json.RawMessage {
	return t.msgCh
}

// EndInput closes the stdin pipe, signaling EOF to the claude process.
func (t *SubprocessTransport) EndInput() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stdin != nil {
		return t.stdin.Close()
	}
	return nil
}

// IsReady returns true if the transport is connected and the process is running.
func (t *SubprocessTransport) IsReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready
}

// ExitError returns the process exit error, or nil if still running or exited cleanly.
func (t *SubprocessTransport) ExitError() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.exitErr
}

// Close performs graceful shutdown of the claude process.
// Sequence: close stdin → wait 5s → SIGTERM → wait 2s → SIGKILL.
func (t *SubprocessTransport) Close() error {
	// Close stdin to signal EOF.
	t.EndInput()

	// Wait up to 5 seconds for natural exit.
	select {
	case <-t.done:
		return nil
	case <-time.After(5 * time.Second):
	}

	// Send SIGTERM (Unix) or kill immediately (Windows).
	if t.cmd.Process != nil {
		if runtime.GOOS == "windows" {
			t.cmd.Process.Kill()
			<-t.done
			if t.cancel != nil {
				t.cancel()
			}
			return nil
		}
		t.cmd.Process.Signal(syscall.SIGTERM)
	}

	// Wait up to 2 seconds.
	select {
	case <-t.done:
		return nil
	case <-time.After(2 * time.Second):
	}

	// Force kill.
	if t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}

	<-t.done

	if t.cancel != nil {
		t.cancel()
	}

	return nil
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/transport/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add internal/transport/subprocess.go
git commit -m "feat: add SubprocessTransport for claude CLI process management

Spawns claude with --print --output-format stream-json --input-format stream-json.
Reads NDJSON from stdout, writes to stdin.
Graceful shutdown: close stdin → 5s wait → SIGTERM → 2s wait → SIGKILL."
```

---

### Task 11: Control protocol handler

**Files:**
- Create: `controlprotocol.go`

The claude CLI sends `control_request` messages on stdout for hook callbacks, tool permission checks, and SDK MCP tool calls. This handler intercepts these requests, invokes the registered callbacks, and writes responses back to the transport.

- [ ] **Step 1: Write the control protocol handler**

Create `controlprotocol.go`:

```go
package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/anthropics/claude-agent-sdk-go/internal/transport"
)

// controlHandler processes control_request messages from the claude process.
// It routes hook callbacks, tool permission checks, and SDK MCP tool calls.
type controlHandler struct {
	config      *queryConfig
	tp          transport.Transport
	mu          sync.Mutex
	mcpServers  map[string]*McpSdkServer  // SDK MCP servers keyed by server name
	callbackMap map[string]HookCallback   // callback_id → callback function (populated during initialize)
	reqCounter  atomic.Int64              // for generating request IDs
}

func newControlHandler(cfg *queryConfig, tp transport.Transport) *controlHandler {
	h := &controlHandler{
		config:      cfg,
		tp:          tp,
		mcpServers:  make(map[string]*McpSdkServer),
		callbackMap: make(map[string]HookCallback),
	}

	// Collect SDK MCP servers.
	for name, srv := range cfg.mcpServers {
		if srv.SDK != nil {
			h.mcpServers[name] = srv.SDK
		}
	}

	return h
}

// sendInitialize sends the initialize control request to register hooks and agents
// with the CLI process. This must be called after Connect() and before sending
// the first user message.
func (h *controlHandler) sendInitialize(ctx context.Context) error {
	request := map[string]any{
		"subtype": "initialize",
	}

	// Build hooks config with callback IDs.
	if len(h.config.hooks) > 0 {
		hooksConfig := map[string]any{}
		callbackIndex := 0
		for event, matchers := range h.config.hooks {
			var matcherConfigs []map[string]any
			for _, matcher := range matchers {
				var callbackIDs []string
				for _, hook := range matcher.Hooks {
					id := fmt.Sprintf("hook_%d", callbackIndex)
					h.callbackMap[id] = hook
					callbackIDs = append(callbackIDs, id)
					callbackIndex++
				}
				mc := map[string]any{
					"matcher":         matcher.Matcher,
					"hookCallbackIds": callbackIDs,
				}
				if matcher.Timeout > 0 {
					mc["timeout"] = matcher.Timeout.Milliseconds()
				}
				matcherConfigs = append(matcherConfigs, mc)
			}
			hooksConfig[string(event)] = matcherConfigs
		}
		request["hooks"] = hooksConfig
	}

	// Include agents.
	if len(h.config.agents) > 0 {
		request["agents"] = h.config.agents
	}

	// Send initialize request.
	reqID := fmt.Sprintf("sdk_init_%d", h.reqCounter.Add(1))
	msg := map[string]any{
		"type":       "control_request",
		"request_id": reqID,
		"request":    request,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal initialize: %w", err)
	}
	return h.tp.Write(string(data))
}

// controlRequest is the envelope for a control request from the CLI.
type controlRequest struct {
	Type      string          `json:"type"`       // "control_request"
	RequestID string          `json:"request_id"`
	Request   json.RawMessage `json:"request"`
}

// controlRequestPayload extracts the subtype from the inner request.
type controlRequestPayload struct {
	Subtype string `json:"subtype"` // "hook_callback", "can_use_tool", "mcp_message", etc.
}

// handleMessage checks if a raw message is a control_request and handles it.
// Returns true if the message was handled (caller should not forward it).
func (h *controlHandler) handleMessage(ctx context.Context, raw json.RawMessage) bool {
	var req controlRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return false
	}
	if req.Type != "control_request" {
		return false
	}

	var payload controlRequestPayload
	if err := json.Unmarshal(req.Request, &payload); err != nil {
		h.sendControlError(req.RequestID, err.Error())
		return true
	}

	switch payload.Subtype {
	case "hook_callback":
		go h.handleHookCallback(ctx, req.RequestID, req.Request)
	case "can_use_tool":
		go h.handleCanUseTool(ctx, req.RequestID, req.Request)
	case "mcp_message":
		go h.handleMcpMessage(ctx, req.RequestID, req.Request)
	default:
		// Unknown control request — respond with empty result to unblock.
		h.sendControlResponse(req.RequestID, map[string]any{})
	}

	return true
}

// handleHookCallback dispatches a hook callback by callback_id.
// The CLI sends the callback_id that was registered during initialize.
func (h *controlHandler) handleHookCallback(ctx context.Context, reqID string, payload json.RawMessage) {
	var req struct {
		Subtype    string    `json:"subtype"`
		CallbackID string    `json:"callback_id"`
		Input      HookInput `json:"input"`
		ToolUseID  string    `json:"tool_use_id,omitempty"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		h.sendControlResponse(reqID, map[string]any{"continue": true})
		return
	}

	callback, ok := h.callbackMap[req.CallbackID]
	if !ok {
		h.sendControlResponse(reqID, map[string]any{"continue": true})
		return
	}

	output, err := callback(ctx, req.Input, req.ToolUseID)
	if err != nil {
		h.sendControlResponse(reqID, map[string]any{
			"decision": "block",
			"reason":   err.Error(),
		})
		return
	}

	resp, _ := json.Marshal(output)
	var respMap map[string]any
	json.Unmarshal(resp, &respMap)
	h.sendControlResponse(reqID, respMap)
}

// handleCanUseTool dispatches a tool permission check to the registered callback.
func (h *controlHandler) handleCanUseTool(ctx context.Context, reqID string, payload json.RawMessage) {
	if h.config.canUseTool == nil {
		h.sendControlResponse(reqID, map[string]any{"behavior": "allow"})
		return
	}

	var req struct {
		Subtype               string           `json:"subtype"`
		ToolName              string           `json:"tool_name"`
		Input                 map[string]any   `json:"input"`
		ToolUseID             string           `json:"tool_use_id"`
		AgentID               string           `json:"agent_id,omitempty"`
		PermissionSuggestions []PermissionUpdate `json:"permission_suggestions,omitempty"`
		BlockedPath           string           `json:"blocked_path,omitempty"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		h.sendControlResponse(reqID, map[string]any{"behavior": "allow"})
		return
	}

	permCtx := ToolPermissionContext{
		ToolUseID:   req.ToolUseID,
		AgentID:     req.AgentID,
		Suggestions: req.PermissionSuggestions,
	}

	result, err := h.config.canUseTool(req.ToolName, req.Input, permCtx)
	if err != nil {
		h.sendControlResponse(reqID, map[string]any{
			"behavior": "deny",
			"message":  err.Error(),
		})
		return
	}

	resp, _ := json.Marshal(result)
	var respMap map[string]any
	json.Unmarshal(resp, &respMap)
	h.sendControlResponse(reqID, respMap)
}

// handleMcpMessage routes JSONRPC messages to in-process SDK MCP servers.
// The CLI sends mcp_message control requests containing raw JSONRPC payloads
// for methods: initialize, notifications/initialized, tools/list, tools/call.
func (h *controlHandler) handleMcpMessage(ctx context.Context, reqID string, payload json.RawMessage) {
	var req struct {
		Subtype    string          `json:"subtype"`
		ServerName string          `json:"server_name"`
		Message    json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		h.sendControlError(reqID, err.Error())
		return
	}

	server, ok := h.mcpServers[req.ServerName]
	if !ok {
		h.sendControlError(reqID, fmt.Sprintf("unknown MCP server: %s", req.ServerName))
		return
	}

	// Parse JSONRPC message.
	var rpc struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(req.Message, &rpc); err != nil {
		h.sendControlError(reqID, err.Error())
		return
	}

	var rpcResponse map[string]any

	switch rpc.Method {
	case "initialize":
		rpcResponse = map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc.ID,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities":   map[string]any{"tools": map[string]any{}},
				"serverInfo": map[string]any{
					"name":    server.Name,
					"version": server.Version,
				},
			},
		}

	case "notifications/initialized":
		rpcResponse = map[string]any{
			"jsonrpc": "2.0",
			"result":  map[string]any{},
		}

	case "tools/list":
		var tools []map[string]any
		for _, t := range server.Tools {
			toolDef := map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": json.RawMessage(t.InputSchema),
			}
			if t.Annotations != nil {
				toolDef["annotations"] = t.Annotations
			}
			tools = append(tools, toolDef)
		}
		rpcResponse = map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc.ID,
			"result":  map[string]any{"tools": tools},
		}

	case "tools/call":
		var params struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(rpc.Params, &params); err != nil {
			rpcResponse = map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"error":   map[string]any{"code": -32603, "message": err.Error()},
			}
			break
		}

		// Find the tool.
		var tool *McpTool
		for i := range server.Tools {
			if server.Tools[i].Name == params.Name {
				tool = &server.Tools[i]
				break
			}
		}
		if tool == nil {
			rpcResponse = map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"error":   map[string]any{"code": -32601, "message": fmt.Sprintf("unknown tool: %s", params.Name)},
			}
			break
		}

		result, err := tool.Handler(ctx, params.Arguments)
		if err != nil {
			rpcResponse = map[string]any{
				"jsonrpc": "2.0",
				"id":      rpc.ID,
				"result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": err.Error()}},
					"isError": true,
				},
			}
			break
		}

		resultMap, _ := json.Marshal(result)
		var resultAny map[string]any
		json.Unmarshal(resultMap, &resultAny)
		rpcResponse = map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc.ID,
			"result":  resultAny,
		}

	default:
		rpcResponse = map[string]any{
			"jsonrpc": "2.0",
			"id":      rpc.ID,
			"error":   map[string]any{"code": -32601, "message": fmt.Sprintf("method not found: %s", rpc.Method)},
		}
	}

	h.sendControlResponse(reqID, map[string]any{"mcp_response": rpcResponse})
}

// sendControlResponse writes a success control_response message to the transport.
// The response envelope follows the CLI's expected format:
// {"type": "control_response", "response": {"subtype": "success", "request_id": ..., "response": ...}}
func (h *controlHandler) sendControlResponse(reqID string, result map[string]any) {
	msg := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": reqID,
			"response":   result,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tp.Write(string(data))
}

// sendControlError writes an error control_response message to the transport.
func (h *controlHandler) sendControlError(reqID string, errMsg string) {
	msg := map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "error",
			"request_id": reqID,
			"error":      errMsg,
		},
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tp.Write(string(data))
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add controlprotocol.go
git commit -m "feat: add control protocol handler with initialize handshake and JSONRPC MCP

Routes control_request messages from the CLI to registered hook callbacks
(by callback_id), CanUseTool permission checks, and in-process SDK MCP
server tool handlers (via JSONRPC protocol: initialize, tools/list, tools/call).
Includes sendInitialize() for registering hooks and agents at startup."
```

---

### Task 12: Stream type and Query entry point

**Files:**
- Create: `sdk.go`

This is the primary public API. `Query()` spawns a subprocess, sends a prompt, and returns a `Stream` that yields messages.

- [ ] **Step 1: Write the Stream type and Query function**

Create `sdk.go`:

```go
package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/claude-agent-sdk-go/internal/clilookup"
	"github.com/anthropics/claude-agent-sdk-go/internal/transport"
)

// Stream iterates over SDK messages from the claude process.
// Usage:
//
//	stream := agentsdk.Query(ctx, "Hello", opts...)
//	defer stream.Close()
//	for stream.Next() {
//	    msg := stream.Current()
//	    // handle msg
//	}
//	if err := stream.Err(); err != nil {
//	    // handle error
//	}
type Stream struct {
	transport      transport.Transport
	controlHandler *controlHandler
	ctx            context.Context
	msgCh          <-chan json.RawMessage
	current        SDKMessage
	err            error
	result         *ResultMessage
	closed         bool
	oneShot        bool // true for Query() — closes stdin after result
}

// Next advances to the next message. Returns false when the stream is
// exhausted or an error occurred. Always check Err() after Next() returns false.
// Control protocol messages (hook callbacks, permission checks, MCP tool calls)
// are handled internally and not exposed to the caller.
func (s *Stream) Next() bool {
	for {
		if s.err != nil || s.closed {
			return false
		}

		raw, ok := <-s.msgCh
		if !ok {
			// Channel closed — process exited.
			// Check if the process exited with an error.
			if s.transport != nil {
				if exitErr := s.transport.ExitError(); exitErr != nil && s.result == nil {
					s.err = &ProcessError{ExitCode: -1, Stderr: exitErr.Error()}
				}
			}
			return false
		}

		// Route control requests through the handler (hooks, permissions, MCP tools).
		if s.controlHandler != nil && s.controlHandler.handleMessage(s.ctx, raw) {
			continue // Control request handled, get next message.
		}

		var msg SDKMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			s.err = &MessageParseError{Data: raw, Err: err}
			return false
		}

		s.current = msg

		// Capture result message for Result() convenience method.
		if result, ok := msg.AsResult(); ok {
			s.result = result
			// In one-shot mode, close stdin after receiving the result
			// so the CLI process can exit. This is deferred until now
			// because the control protocol needs stdin open for hook
			// responses and MCP tool calls during execution.
			if s.oneShot && s.transport != nil {
				s.transport.EndInput()
			}
		}

		return true
	}
}

// Current returns the current message. Valid only after Next() returns true.
func (s *Stream) Current() SDKMessage {
	return s.current
}

// Err returns the error that stopped iteration, or nil for clean finish.
func (s *Stream) Err() error {
	return s.err
}

// Close shuts down the claude process and releases resources.
func (s *Stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.transport != nil {
		return s.transport.Close()
	}
	return nil
}

// Interrupt sends an interrupt signal to the claude process.
func (s *Stream) Interrupt() error {
	req := map[string]any{
		"type":       "control_request",
		"request_id": "sdk_interrupt",
		"request": map[string]any{
			"type": "interrupt",
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return s.transport.Write(string(data))
}

// Result drains the stream and returns the final ResultMessage.
// This is a convenience that consumes all remaining messages.
func (s *Stream) Result() (*ResultMessage, error) {
	// Drain remaining messages.
	for s.Next() {
		// Just consume.
	}
	if s.err != nil {
		return nil, s.err
	}
	if s.result == nil {
		return nil, fmt.Errorf("no result message received")
	}
	return s.result, nil
}

// NewStreamFromTransport creates a Stream from an existing transport.
// This is primarily used for testing with mock transports.
func NewStreamFromTransport(tp transport.Transport) *Stream {
	return &Stream{
		transport: tp,
		ctx:       context.Background(),
		msgCh:     tp.ReadMessages(),
	}
}

// TestQueryConfig exposes queryConfig fields for external tests.
type TestQueryConfig struct {
	Model          string
	MaxTurns       int
	PermissionMode PermissionMode
	AllowedTools   []string
}

// BuildCLIArgsForTest converts a TestQueryConfig to CLI arguments for testing.
func BuildCLIArgsForTest(tc TestQueryConfig) []string {
	cfg := &queryConfig{
		model:          tc.Model,
		maxTurns:       tc.MaxTurns,
		permissionMode: tc.PermissionMode,
		allowedTools:   tc.AllowedTools,
	}
	return buildCLIArgs(cfg)
}

// Query creates a new Claude Code session and streams messages.
// It spawns a claude subprocess, sends the prompt, and returns
// a Stream that yields messages as they arrive.
func Query(ctx context.Context, prompt string, opts ...QueryOption) *Stream {
	cfg := &queryConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	cfg.applyDefaults()

	// Find CLI binary.
	cliPath, err := clilookup.FindCLI(cfg.cliPath)
	if err != nil {
		// Wrap internal error in public CLINotFoundError.
		if nf, ok := err.(*clilookup.NotFoundError); ok {
			return &Stream{err: &CLINotFoundError{SearchedPaths: nf.SearchedPaths}}
		}
		return &Stream{err: err}
	}

	// Build CLI arguments from config.
	args := buildCLIArgs(cfg)

	// Merge API key into env if set.
	env := cfg.env
	if cfg.apiKey != "" {
		if env == nil {
			env = make(map[string]string)
		}
		env["ANTHROPIC_API_KEY"] = cfg.apiKey
	}

	// Create transport.
	tp := transport.NewSubprocessTransport(transport.SubprocessConfig{
		CLIPath:       cliPath,
		Args:          args,
		Cwd:           cfg.cwd,
		Env:           env,
		MaxBufferSize: cfg.maxBufferSize,
		StderrFunc:    cfg.stderrFunc,
	})

	// Connect (start process).
	if err := tp.Connect(ctx); err != nil {
		return &Stream{err: err}
	}

	// Send prompt via stdin as a user message.
	userMsg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	}
	data, err := json.Marshal(userMsg)
	if err != nil {
		tp.Close()
		return &Stream{err: fmt.Errorf("marshal prompt: %w", err)}
	}
	if err := tp.Write(string(data)); err != nil {
		tp.Close()
		return &Stream{err: fmt.Errorf("write prompt: %w", err)}
	}

	// NOTE: Do NOT call tp.EndInput() here. The control protocol handler
	// needs stdin open to send hook responses, permission decisions, and
	// MCP tool results back to the CLI process. stdin is closed when
	// Stream.Close() is called or when the process exits naturally.

	// Create control handler for hooks, permissions, and MCP tool calls.
	handler := newControlHandler(cfg, tp)

	// Send initialize handshake to register hooks and agents with the CLI.
	if err := handler.sendInitialize(ctx); err != nil {
		tp.Close()
		return &Stream{err: fmt.Errorf("initialize: %w", err)}
	}

	return &Stream{
		transport:      tp,
		controlHandler: handler,
		ctx:            ctx,
		msgCh:          tp.ReadMessages(),
		oneShot:        true,
	}
}

// buildCLIArgs converts queryConfig to CLI arguments.
func buildCLIArgs(cfg *queryConfig) []string {
	var args []string

	// Model.
	if cfg.model != "" {
		args = append(args, "--model", cfg.model)
	}

	// Permission mode.
	if cfg.permissionMode != "" {
		args = append(args, "--permission-mode", string(cfg.permissionMode))
	}

	// Bypass permissions.
	if cfg.bypassPermissions && cfg.allowDangerouslySkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}

	// Max turns.
	if cfg.maxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", cfg.maxTurns))
	}

	// Allowed tools.
	for _, tool := range cfg.allowedTools {
		args = append(args, "--allowedTools", tool)
	}

	// Tools (explicit list).
	for _, tool := range cfg.tools {
		args = append(args, "--tools", tool)
	}

	// Tools preset (Claude Code default set).
	if cfg.toolsPreset {
		args = append(args, "--tools-preset", "claude_code")
	}

	// Disallowed tools.
	for _, tool := range cfg.disallowedTools {
		args = append(args, "--disallowedTools", tool)
	}

	// MCP servers (passed as JSON config — excludes SDK servers which are handled in-process).
	if len(cfg.mcpServers) > 0 {
		// Filter out SDK servers (handled via control protocol, not CLI args).
		cliServers := make(map[string]McpServerConfig)
		for name, srv := range cfg.mcpServers {
			if srv.SDK == nil {
				cliServers[name] = srv
			}
		}
		if len(cliServers) > 0 {
			mcpJSON, err := json.Marshal(cliServers)
			if err == nil {
				args = append(args, "--mcp-config", string(mcpJSON))
			}
		}
	}

	// Agents (subagent definitions).
	if len(cfg.agents) > 0 {
		agentsJSON, err := json.Marshal(cfg.agents)
		if err == nil {
			args = append(args, "--agents", string(agentsJSON))
		}
	}

	// System prompt variants.
	if cfg.systemPrompt != "" {
		args = append(args, "--system-prompt", cfg.systemPrompt)
	}
	if cfg.systemPromptPreset != "" {
		// Use Claude Code default prompt with appended text.
		args = append(args, "--system-prompt-preset", "claude_code")
		args = append(args, "--append-system-prompt", cfg.systemPromptPreset)
	}
	if cfg.systemPromptFile != "" {
		args = append(args, "--system-prompt-file", cfg.systemPromptFile)
	}

	// Resume session.
	if cfg.resumeSessionID != "" {
		args = append(args, "--resume", cfg.resumeSessionID)
	}
	if cfg.continueSession {
		args = append(args, "--continue")
	}
	if cfg.forkSession {
		args = append(args, "--fork-session")
	}

	// Session ID.
	if cfg.sessionID != "" {
		args = append(args, "--session-id", cfg.sessionID)
	}

	// Include partial messages.
	if cfg.includePartialMessages {
		args = append(args, "--include-partial-messages")
	}

	// Debug.
	if cfg.debug {
		args = append(args, "--debug")
	}
	if cfg.debugFile != "" {
		args = append(args, "--debug-file", cfg.debugFile)
	}

	// Additional directories.
	for _, dir := range cfg.additionalDirs {
		args = append(args, "--add-dir", dir)
	}

	// Settings sources.
	for _, source := range cfg.settingSources {
		args = append(args, "--setting-source", string(source))
	}

	// Plugins.
	for _, plugin := range cfg.plugins {
		if plugin.Path != "" {
			args = append(args, "--plugin", plugin.Path)
		}
	}

	// Session persistence.
	if cfg.persistSession != nil && !*cfg.persistSession {
		args = append(args, "--no-persist-session")
	}

	// File checkpointing.
	if cfg.fileCheckpointing {
		args = append(args, "--enable-file-checkpointing")
	}

	// Beta features.
	for _, beta := range cfg.betas {
		args = append(args, "--beta", beta)
	}

	// Effort.
	if cfg.effort != "" {
		args = append(args, "--effort", string(cfg.effort))
	}

	// Thinking.
	if cfg.thinking != nil && cfg.thinking.Type == "enabled" && cfg.thinking.BudgetTokens > 0 {
		args = append(args, "--max-thinking-tokens", fmt.Sprintf("%d", cfg.thinking.BudgetTokens))
	}

	// Output format (structured output).
	if cfg.outputFormat != nil {
		formatJSON, err := json.Marshal(cfg.outputFormat)
		if err == nil {
			args = append(args, "--output-format-json", string(formatJSON))
		}
	}

	// Max budget.
	if cfg.maxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", cfg.maxBudgetUSD))
	}

	// Fallback model.
	if cfg.fallbackModel != "" {
		args = append(args, "--fallback-model", cfg.fallbackModel)
	}

	// Task budget.
	if cfg.taskBudget != nil && cfg.taskBudget.Total > 0 {
		args = append(args, "--task-budget", fmt.Sprintf("%d", cfg.taskBudget.Total))
	}

	// User.
	if cfg.user != "" {
		args = append(args, "--user", cfg.user)
	}

	// Sandbox.
	if cfg.sandbox != nil {
		sandboxJSON, err := json.Marshal(cfg.sandbox)
		if err == nil {
			args = append(args, "--sandbox-config", string(sandboxJSON))
		}
	}

	// Permission prompt tool name.
	if cfg.permissionPromptToolName != "" {
		args = append(args, "--permission-prompt-tool-name", cfg.permissionPromptToolName)
	}

	// Tool config.
	if cfg.toolConfig != nil {
		toolConfigJSON, err := json.Marshal(cfg.toolConfig)
		if err == nil {
			args = append(args, "--tool-config", string(toolConfigJSON))
		}
	}

	// Extra args (escape hatch).
	args = append(args, cfg.extraArgs...)

	return args
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add sdk.go
git commit -m "feat: add Query() entry point and Stream message iterator

Query() spawns a claude subprocess, sends the prompt, and returns
a Stream. Stream.Next()/Current()/Err() follow the standard Go
iterator pattern. Result() drains and returns the final result."
```

---

### Task 13: Stream and Query unit tests

**Files:**
- Create: `sdk_test.go`
- Create: `internal/transport/mock.go`

Tests use a mock transport to avoid requiring the real claude CLI.

- [ ] **Step 1: Write mock transport**

Create `internal/transport/mock.go`:

```go
package transport

import (
	"context"
	"encoding/json"
	"sync"
)

// MockTransport is a test double for Transport.
type MockTransport struct {
	messages []json.RawMessage
	msgCh    chan json.RawMessage
	written  []string
	ready    bool
	mu       sync.Mutex
}

// NewMockTransport creates a MockTransport preloaded with messages.
func NewMockTransport(messages ...json.RawMessage) *MockTransport {
	return &MockTransport{
		messages: messages,
		msgCh:    make(chan json.RawMessage, len(messages)),
	}
}

func (m *MockTransport) Connect(_ context.Context) error {
	m.mu.Lock()
	m.ready = true
	m.mu.Unlock()
	for _, msg := range m.messages {
		m.msgCh <- msg
	}
	close(m.msgCh)
	return nil
}

func (m *MockTransport) Write(data string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.written = append(m.written, data)
	return nil
}

func (m *MockTransport) ReadMessages() <-chan json.RawMessage {
	return m.msgCh
}

func (m *MockTransport) Close() error {
	m.mu.Lock()
	m.ready = false
	m.mu.Unlock()
	return nil
}

func (m *MockTransport) EndInput() error { return nil }

func (m *MockTransport) ExitError() error { return nil }

func (m *MockTransport) IsReady() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ready
}

// Written returns all strings written to the transport.
func (m *MockTransport) Written() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string{}, m.written...)
}
```

- [ ] **Step 2: Write Stream tests**

Create `sdk_test.go`:

```go
package agentsdk_test

import (
	"encoding/json"
	"testing"

	agentsdk "github.com/anthropics/claude-agent-sdk-go"
	"github.com/anthropics/claude-agent-sdk-go/internal/transport"
)

func TestStream_SingleAssistantMessage(t *testing.T) {
	msg := json.RawMessage(`{"type":"assistant","content":[{"type":"text","text":"Hello!"}],"model":"claude-sonnet-4-6","session_id":"s1","uuid":"u1"}`)
	result := json.RawMessage(`{"type":"result","subtype":"success","result":"Hello!","session_id":"s1","is_error":false,"num_turns":1}`)

	mock := transport.NewMockTransport(msg, result)
	mock.Connect(nil)

	stream := agentsdk.NewStreamFromTransport(mock)
	defer stream.Close()

	// First message: assistant.
	if !stream.Next() {
		t.Fatal("expected Next() to return true for assistant message")
	}
	cur := stream.Current()
	if cur.Type != "assistant" {
		t.Errorf("expected type assistant, got %s", cur.Type)
	}
	assistant, ok := cur.AsAssistant()
	if !ok {
		t.Fatal("AsAssistant returned false")
	}
	if len(assistant.Content) == 0 {
		t.Fatal("expected content blocks")
	}
	text, ok := assistant.Content[0].AsText()
	if !ok {
		t.Fatal("expected text block")
	}
	if text.Text != "Hello!" {
		t.Errorf("expected 'Hello!', got %q", text.Text)
	}

	// Second message: result.
	if !stream.Next() {
		t.Fatal("expected Next() to return true for result message")
	}
	if stream.Current().Type != "result" {
		t.Errorf("expected type result, got %s", stream.Current().Type)
	}

	// Stream exhausted.
	if stream.Next() {
		t.Fatal("expected Next() to return false after stream ends")
	}
	if stream.Err() != nil {
		t.Fatalf("unexpected error: %v", stream.Err())
	}
}

func TestStream_Result(t *testing.T) {
	msg := json.RawMessage(`{"type":"assistant","content":[{"type":"text","text":"Hi"}],"session_id":"s1"}`)
	result := json.RawMessage(`{"type":"result","subtype":"success","result":"Hi","session_id":"s1","is_error":false,"num_turns":1,"total_cost_usd":0.01}`)

	mock := transport.NewMockTransport(msg, result)
	mock.Connect(nil)

	stream := agentsdk.NewStreamFromTransport(mock)
	defer stream.Close()

	res, err := stream.Result()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Result != "Hi" {
		t.Errorf("expected 'Hi', got %q", res.Result)
	}
	if res.TotalCostUSD == nil || *res.TotalCostUSD != 0.01 {
		t.Errorf("expected cost 0.01, got %v", res.TotalCostUSD)
	}
}

func TestStream_InvalidJSON(t *testing.T) {
	msg := json.RawMessage(`not json`)

	mock := transport.NewMockTransport(msg)
	mock.Connect(nil)

	stream := agentsdk.NewStreamFromTransport(mock)
	defer stream.Close()

	if stream.Next() {
		t.Fatal("expected Next() to return false on invalid JSON")
	}
	if stream.Err() == nil {
		t.Fatal("expected parse error")
	}
}

func TestStream_EmptyStream(t *testing.T) {
	mock := transport.NewMockTransport()
	mock.Connect(nil)

	stream := agentsdk.NewStreamFromTransport(mock)
	defer stream.Close()

	if stream.Next() {
		t.Fatal("expected Next() to return false on empty stream")
	}
	if stream.Err() != nil {
		t.Fatalf("unexpected error: %v", stream.Err())
	}
}

func TestStream_ToolUseBlock(t *testing.T) {
	msg := json.RawMessage(`{"type":"assistant","content":[{"type":"tool_use","id":"tu1","name":"Read","input":{"file_path":"/tmp/x"}}],"session_id":"s1"}`)

	mock := transport.NewMockTransport(msg)
	mock.Connect(nil)

	stream := agentsdk.NewStreamFromTransport(mock)
	defer stream.Close()

	if !stream.Next() {
		t.Fatal("expected message")
	}
	assistant, ok := stream.Current().AsAssistant()
	if !ok {
		t.Fatal("expected assistant message")
	}
	toolUse, ok := assistant.Content[0].AsToolUse()
	if !ok {
		t.Fatal("expected tool_use block")
	}
	if toolUse.Name != "Read" {
		t.Errorf("expected tool name 'Read', got %q", toolUse.Name)
	}
	if toolUse.Input["file_path"] != "/tmp/x" {
		t.Errorf("expected file_path '/tmp/x', got %v", toolUse.Input["file_path"])
	}
}

func TestStream_RateLimitEvent(t *testing.T) {
	msg := json.RawMessage(`{"type":"rate_limit","rate_limit_info":{"status":"allowed_warning","resets_at":"2026-03-31T12:00:00Z","rate_limit_type":"five_hour","utilization":0.85},"session_id":"s1","uuid":"u1"}`)

	mock := transport.NewMockTransport(msg)
	mock.Connect(nil)

	stream := agentsdk.NewStreamFromTransport(mock)
	defer stream.Close()

	if !stream.Next() {
		t.Fatal("expected message")
	}
	rl, ok := stream.Current().AsRateLimit()
	if !ok {
		t.Fatal("expected rate_limit message")
	}
	if rl.RateLimitInfo.Status != "allowed_warning" {
		t.Errorf("expected status 'allowed_warning', got %q", rl.RateLimitInfo.Status)
	}
	if rl.RateLimitInfo.Utilization != 0.85 {
		t.Errorf("expected utilization 0.85, got %v", rl.RateLimitInfo.Utilization)
	}
}

func TestBuildCLIArgs_BasicOptions(t *testing.T) {
	args := agentsdk.BuildCLIArgsForTest(agentsdk.TestQueryConfig{
		Model:          "opus",
		MaxTurns:       10,
		PermissionMode: agentsdk.PermissionBypassAll,
		AllowedTools:   []string{"Read", "Write"},
	})

	want := map[string]bool{
		"--model":           true,
		"opus":              true,
		"--max-turns":       true,
		"10":                true,
		"--permission-mode": true,
		"--allowedTools":    true,
		"Read":              true,
		"Write":             true,
	}
	for _, arg := range args {
		delete(want, arg)
	}
	// permission mode value
	delete(want, string(agentsdk.PermissionBypassAll))

	for missing := range want {
		t.Errorf("expected arg %q in CLI args", missing)
	}
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./... -v`
Expected: All PASS.

- [ ] **Step 4: Commit**

```bash
git add sdk_test.go internal/transport/mock.go
git commit -m "test: add Stream unit tests with mock transport

Tests message iteration, Result() drain, invalid JSON handling,
empty streams, tool_use content blocks, and CLI arg building."
```

---

### Task 14: Interactive Client

**Files:**
- Create: `client.go`

The Client maintains a persistent subprocess for multi-turn conversations.

- [ ] **Step 1: Write the Client type**

Create `client.go`:

```go
package agentsdk

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/anthropics/claude-agent-sdk-go/internal/clilookup"
	"github.com/anthropics/claude-agent-sdk-go/internal/transport"
)

// Client maintains a persistent connection to a claude subprocess
// for multi-turn conversations.
type Client struct {
	config         queryConfig
	transport      transport.Transport
	controlHandler *controlHandler
	msgCh          chan SDKMessage
	cancel         context.CancelFunc
	ctx            context.Context
	closed         atomic.Bool
	mu             sync.Mutex
	reqID          atomic.Int64

	// Control protocol response routing.
	pendingMu sync.Mutex
	pending   map[string]chan json.RawMessage // request_id → response channel
}

// NewClient creates a new interactive Client with the given options.
func NewClient(opts ...QueryOption) *Client {
	cfg := queryConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	cfg.applyDefaults()

	return &Client{
		config:  cfg,
		msgCh:   make(chan SDKMessage, 64),
		pending: make(map[string]chan json.RawMessage),
	}
}

// Connect starts the claude subprocess and begins reading messages.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cliPath, err := clilookup.FindCLI(c.config.cliPath)
	if err != nil {
		return err
	}

	args := buildCLIArgs(&c.config)

	// Merge API key into env if set.
	env := c.config.env
	if c.config.apiKey != "" {
		if env == nil {
			env = make(map[string]string)
		}
		env["ANTHROPIC_API_KEY"] = c.config.apiKey
	}

	tp := transport.NewSubprocessTransport(transport.SubprocessConfig{
		CLIPath:       cliPath,
		Args:          args,
		Cwd:           c.config.cwd,
		Env:           env,
		MaxBufferSize: c.config.maxBufferSize,
		StderrFunc:    c.config.stderrFunc,
	})

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	if err := tp.Connect(ctx); err != nil {
		cancel()
		return err
	}

	c.transport = tp
	c.ctx = ctx

	// Create control handler for hooks, permissions, and MCP tool calls.
	c.controlHandler = newControlHandler(&c.config, tp)

	// Send initialize handshake to register hooks and agents with the CLI.
	if err := c.controlHandler.sendInitialize(ctx); err != nil {
		cancel()
		tp.Close()
		return fmt.Errorf("initialize: %w", err)
	}

	// Read messages from transport, route control requests/responses, forward others.
	go func() {
		for raw := range tp.ReadMessages() {
			// Route control requests (hook callbacks, permission checks, MCP tool calls).
			if c.controlHandler.handleMessage(ctx, raw) {
				continue
			}

			// Check if this is a control response (for Client-initiated requests).
			var envelope struct {
				Type      string `json:"type"`
				RequestID string `json:"request_id"`
			}
			if json.Unmarshal(raw, &envelope) == nil && envelope.Type == "control_response" {
				c.pendingMu.Lock()
				if ch, ok := c.pending[envelope.RequestID]; ok {
					ch <- raw
					delete(c.pending, envelope.RequestID)
				}
				c.pendingMu.Unlock()
				continue
			}

			var msg SDKMessage
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}
			c.msgCh <- msg
		}
		close(c.msgCh)
	}()

	return nil
}

// Send sends a user message to the claude process.
func (c *Client) Send(ctx context.Context, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport == nil || !c.transport.IsReady() {
		return fmt.Errorf("client not connected")
	}

	userMsg := map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": prompt,
		},
	}
	data, err := json.Marshal(userMsg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}
	return c.transport.Write(string(data))
}

// Messages returns a channel that yields SDKMessages as they arrive.
func (c *Client) Messages() <-chan SDKMessage {
	return c.msgCh
}

// Interrupt sends an interrupt signal to the claude process.
func (c *Client) Interrupt() error {
	return c.sendControlRequest("interrupt", nil)
}

// SetModel changes the model at runtime.
func (c *Client) SetModel(model string) error {
	return c.sendControlRequest("set_model", map[string]any{"model": model})
}

// SetPermissionMode changes the permission mode at runtime.
func (c *Client) SetPermissionMode(mode PermissionMode) error {
	return c.sendControlRequest("set_permission_mode", map[string]any{"mode": string(mode)})
}

// SetMaxThinkingTokens changes the max thinking tokens at runtime.
func (c *Client) SetMaxThinkingTokens(tokens int) error {
	return c.sendControlRequest("set_max_thinking_tokens", map[string]any{"max_thinking_tokens": tokens})
}

// ContextUsage contains context window usage information.
type ContextUsage struct {
	TotalTokens          int                `json:"totalTokens"`
	MaxTokens            int                `json:"maxTokens"`
	Percentage           float64            `json:"percentage"` // 0–100
	Model                string             `json:"model"`
	IsAutoCompactEnabled bool               `json:"isAutoCompactEnabled"`
	Categories           []ContextCategory  `json:"categories,omitempty"`  // Per-category token breakdown
	McpTools             []string           `json:"mcpTools,omitempty"`    // Active MCP tools
	Agents               []string           `json:"agents,omitempty"`     // Active agents
	MemoryFiles          []string           `json:"memoryFiles,omitempty"` // Loaded memory files
}

// ContextCategory is a single category in the context usage breakdown.
type ContextCategory struct {
	Name   string `json:"name"`   // e.g., "system", "conversation", "tools"
	Tokens int    `json:"tokens"` // Token count for this category
	Color  string `json:"color"`  // Display color hint
}

// GetContextUsage returns context window usage information.
func (c *Client) GetContextUsage(ctx context.Context) (*ContextUsage, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "get_context_usage", nil)
	if err != nil {
		return nil, err
	}
	var usage ContextUsage
	if err := json.Unmarshal(raw, &usage); err != nil {
		return nil, fmt.Errorf("parse context usage: %w", err)
	}
	return &usage, nil
}

// McpStatus requests the status of all MCP servers.
func (c *Client) McpStatus(ctx context.Context) ([]McpServerStatus, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "mcp_status", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		McpServers []McpServerStatus `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse mcp status: %w", err)
	}
	return resp.McpServers, nil
}

// ReconnectMcpServer reconnects a failed MCP server by name.
func (c *Client) ReconnectMcpServer(serverName string) error {
	return c.sendControlRequest("reconnect_mcp_server", map[string]any{"server_name": serverName})
}

// ToggleMcpServer enables or disables an MCP server at runtime.
func (c *Client) ToggleMcpServer(serverName string, enabled bool) error {
	return c.sendControlRequest("toggle_mcp_server", map[string]any{
		"server_name": serverName,
		"enabled":     enabled,
	})
}

// SetMcpServers dynamically replaces all MCP server configurations.
func (c *Client) SetMcpServers(servers map[string]McpServerConfig) error {
	serversJSON, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("marshal servers: %w", err)
	}
	var serversMap map[string]any
	json.Unmarshal(serversJSON, &serversMap)
	return c.sendControlRequest("set_mcp_servers", map[string]any{"servers": serversMap})
}

// StopTask stops a background task.
func (c *Client) StopTask(taskID string) error {
	return c.sendControlRequest("stop_task", map[string]any{"task_id": taskID})
}

// RewindFiles reverts file changes to the state at a given user message.
// Requires WithFileCheckpointing to be enabled.
func (c *Client) RewindFiles(userMessageID string) error {
	return c.sendControlRequest("rewind_files", map[string]any{"user_message_id": userMessageID})
}

// ServerInfo contains information about the claude server's capabilities.
type ServerInfo struct {
	Commands    []string       `json:"commands,omitempty"`
	OutputStyle string         `json:"outputStyle,omitempty"`
	Extra       map[string]any `json:"-"` // Additional fields
}

// GetServerInfo returns information about the claude server's capabilities.
func (c *Client) GetServerInfo(ctx context.Context) (*ServerInfo, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "get_server_info", nil)
	if err != nil {
		return nil, err
	}
	var info ServerInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("parse server info: %w", err)
	}
	return &info, nil
}

// PromptSuggestion requests prompt suggestions based on the current context.
func (c *Client) PromptSuggestion(ctx context.Context) ([]string, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "prompt_suggestion", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Suggestions []string `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse prompt suggestions: %w", err)
	}
	return resp.Suggestions, nil
}

// EnableChannel activates an MCP server channel by name.
func (c *Client) EnableChannel(serverName, channel string) error {
	return c.sendControlRequest("enable_channel", map[string]any{
		"server_name": serverName,
		"channel":     channel,
	})
}

// RuntimeSettings contains the resolved runtime settings from the claude process.
type RuntimeSettings struct {
	Model  string         `json:"model"`
	Effort string         `json:"effort"`
	Extra  map[string]any `json:"-"` // Additional resolved settings
}

// GetSettings returns the applied runtime settings with resolved model and effort values.
func (c *Client) GetSettings(ctx context.Context) (*RuntimeSettings, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "get_settings", nil)
	if err != nil {
		return nil, err
	}
	var settings RuntimeSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("parse settings: %w", err)
	}
	return &settings, nil
}

// ReloadPluginsResult contains refreshed plugin state after reloading.
type ReloadPluginsResult struct {
	Commands   []string          `json:"commands,omitempty"`
	Agents     map[string]any    `json:"agents,omitempty"`
	McpServers []McpServerStatus `json:"mcpServers,omitempty"`
}

// ReloadPlugins reloads all plugins and returns refreshed commands, agents, and MCP servers.
func (c *Client) ReloadPlugins(ctx context.Context) (*ReloadPluginsResult, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "reload_plugins", nil)
	if err != nil {
		return nil, err
	}
	var result ReloadPluginsResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parse reload plugins: %w", err)
	}
	return &result, nil
}

// Close shuts down the claude subprocess and releases resources.
func (c *Client) Close() error {
	if c.closed.Swap(true) {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}
	if c.transport != nil {
		return c.transport.Close()
	}
	return nil
}

// sendControlRequest sends a fire-and-forget control request to the claude process.
func (c *Client) sendControlRequest(requestType string, params map[string]any) error {
	_, err := c.writeControlRequest(requestType, params)
	return err
}

// sendControlRequestWithResponse sends a control request and waits for the response.
func (c *Client) sendControlRequestWithResponse(ctx context.Context, requestType string, params map[string]any) (json.RawMessage, error) {
	reqID, err := c.writeControlRequest(requestType, params)
	if err != nil {
		return nil, err
	}

	// Register response channel.
	ch := make(chan json.RawMessage, 1)
	c.pendingMu.Lock()
	c.pending[reqID] = ch
	c.pendingMu.Unlock()

	// Wait for response or context cancellation.
	select {
	case raw := <-ch:
		return raw, nil
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, reqID)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	}
}

// writeControlRequest marshals and writes a control request, returning its ID.
func (c *Client) writeControlRequest(requestType string, params map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.transport == nil || !c.transport.IsReady() {
		return "", fmt.Errorf("client not connected")
	}

	reqID := fmt.Sprintf("sdk_%d", c.reqID.Add(1))
	request := map[string]any{"type": requestType}
	for k, v := range params {
		request[k] = v
	}

	msg := map[string]any{
		"type":       "control_request",
		"request_id": reqID,
		"request":    request,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return reqID, c.transport.Write(string(data))
}

// --- V2 Preview API (unstable — may change) ---
//
// These methods mirror the TypeScript SDK's unstable_v2_* functions.
// They provide a session-oriented interface closer to the Python SDK's
// ClaudeSDKClient pattern. The underlying implementation uses the same
// control protocol as the V1 API.

// V2Session represents a session handle returned by V2 API methods.
type V2Session struct {
	ID     string `json:"id"`
	client *Client
}

// UnstableV2CreateSession creates a new session and returns a session handle.
// This is equivalent to NewClient + Connect with a fresh session.
func UnstableV2CreateSession(ctx context.Context, opts ...QueryOption) (*V2Session, error) {
	client := NewClient(opts...)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}

	// Wait for init message to get session ID.
	for msg := range client.Messages() {
		if sys, ok := msg.AsSystem(); ok && sys.Subtype == "init" {
			return &V2Session{
				ID:     sys.SessionID,
				client: client,
			}, nil
		}
	}
	return nil, fmt.Errorf("no init message received")
}

// UnstableV2ResumeSession resumes an existing session by ID.
func UnstableV2ResumeSession(ctx context.Context, sessionID string, opts ...QueryOption) (*V2Session, error) {
	opts = append(opts, WithResume(sessionID))
	client := NewClient(opts...)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	return &V2Session{
		ID:     sessionID,
		client: client,
	}, nil
}

// Send sends a prompt to the session and returns a Stream of response messages.
func (s *V2Session) Send(ctx context.Context, prompt string) (<-chan SDKMessage, error) {
	if err := s.client.Send(ctx, prompt); err != nil {
		return nil, err
	}
	return s.client.Messages(), nil
}

// Close closes the V2 session.
func (s *V2Session) Close() error {
	return s.client.Close()
}

// UnstableV2Prompt is a one-shot convenience that creates a session, sends
// one message, waits for the result, and returns the final ResultMessage.
// This is equivalent to the TypeScript SDK's unstable_v2_prompt().
func UnstableV2Prompt(ctx context.Context, prompt string, opts ...QueryOption) (*ResultMessage, error) {
	stream := Query(ctx, prompt, opts...)
	defer stream.Close()
	return stream.Result()
}

// --- Client inspection methods ---
//
// These methods correspond to the TypeScript SDK's Query interface
// inspection methods (supportedCommands, supportedModels, etc.).

// SlashCommand describes an available slash command.
type SlashCommand struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argumentHint,omitempty"`
}

// SupportedCommands returns the list of available slash commands.
func (c *Client) SupportedCommands(ctx context.Context) ([]SlashCommand, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "supported_commands", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Commands []SlashCommand `json:"commands"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse supported commands: %w", err)
	}
	return resp.Commands, nil
}

// ModelInfo describes an available model.
type ModelInfo struct {
	DisplayName              string   `json:"displayName"`
	Description              string   `json:"description,omitempty"`
	SupportsEffort           bool     `json:"supportsEffort"`
	SupportedEffortLevels    []string `json:"supportedEffortLevels,omitempty"`
	SupportsAdaptiveThinking bool     `json:"supportsAdaptiveThinking"`
	SupportsFastMode         bool     `json:"supportsFastMode"`
}

// SupportedModels returns the list of available models with metadata.
func (c *Client) SupportedModels(ctx context.Context) ([]ModelInfo, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "supported_models", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse supported models: %w", err)
	}
	return resp.Models, nil
}

// AgentInfo describes an available agent.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
}

// SupportedAgents returns the list of available agents.
func (c *Client) SupportedAgents(ctx context.Context) ([]AgentInfo, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "supported_agents", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Agents []AgentInfo `json:"agents"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("parse supported agents: %w", err)
	}
	return resp.Agents, nil
}

// AccountInfo contains account-level information.
type AccountInfo struct {
	OrganizationID   string `json:"organizationId,omitempty"`
	OrganizationName string `json:"organizationName,omitempty"`
	AccountUUID      string `json:"accountUuid,omitempty"`
	EmailAddress     string `json:"emailAddress,omitempty"`
}

// GetAccountInfo returns account-level information.
func (c *Client) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	raw, err := c.sendControlRequestWithResponse(ctx, "account_info", nil)
	if err != nil {
		return nil, err
	}
	var info AccountInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		return nil, fmt.Errorf("parse account info: %w", err)
	}
	return &info, nil
}

// SeedReadState seeds the file read state for change tracking.
// This establishes a known mtime for a file so that future reads can detect changes.
func (c *Client) SeedReadState(path string, mtime int64) error {
	return c.sendControlRequest("seed_read_state", map[string]any{
		"path":  path,
		"mtime": mtime,
	})
}

// ApplyFlagSettings applies runtime settings programmatically.
// This is equivalent to the TypeScript SDK's applyFlagSettings() method.
func (c *Client) ApplyFlagSettings(settings map[string]any) error {
	return c.sendControlRequest("apply_flag_settings", map[string]any{
		"settings": settings,
	})
}

// FastModeState represents the fast mode toggle state.
type FastModeState string

const (
	FastModeOff      FastModeState = "off"
	FastModeOn       FastModeState = "on"
	FastModeCooldown FastModeState = "cooldown"
)

// SetFastMode toggles fast mode on or off.
func (c *Client) SetFastMode(enabled bool) error {
	return c.sendControlRequest("set_fast_mode", map[string]any{
		"enabled": enabled,
	})
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add client.go
git commit -m "feat: add interactive Client for multi-turn conversations

Client.Connect() starts a persistent claude subprocess.
Client.Send() sends user messages. Client.Messages() yields responses.
Supports runtime control: Interrupt, SetModel, SetPermissionMode, McpStatus,
SupportedCommands, SupportedModels, SupportedAgents, AccountInfo, FastMode."
```

---

### Task 15: Session management

**Files:**
- Create: `session.go`

Session management functions use the `claude` CLI's session commands, not the subprocess protocol. They run `claude sessions list/get/delete` and parse the output.

- [ ] **Step 1: Write session management functions**

Create `session.go`:

```go
package agentsdk

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/anthropics/claude-agent-sdk-go/internal/clilookup"
)

// SessionInfo contains metadata about a claude session.
// Timestamps are milliseconds since epoch, matching the TS/Python SDK conventions.
type SessionInfo struct {
	ID           string `json:"id"`
	Summary      string `json:"summary,omitempty"`
	LastModified int64  `json:"last_modified"`           // Milliseconds since epoch
	FileSize     int64  `json:"file_size,omitempty"`
	CustomTitle  string `json:"custom_title,omitempty"`
	FirstPrompt  string `json:"first_prompt,omitempty"`
	GitBranch    string `json:"git_branch,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
	Tag          string `json:"tag,omitempty"`
	CreatedAt    *int64 `json:"created_at,omitempty"`    // Milliseconds since epoch
}

// SessionMessage is a single message within a session.
type SessionMessage struct {
	Role    string `json:"role"` // "user", "assistant", "system"
	Content any    `json:"content"`
}

// ListSessionsOptions configures the list sessions command.
type ListSessionsOptions struct {
	CLIPath          string // Explicit path to claude binary
	Cwd              string // Working directory context
	Limit            int    // Maximum number of sessions to return (0 = no limit)
	Offset           int    // Number of sessions to skip
	IncludeWorktrees bool   // Include sessions from git worktrees
}

// GetSessionInfoOptions configures the get session info command.
type GetSessionInfoOptions struct {
	CLIPath string
	Cwd     string
}

// GetSessionMessagesOptions configures the get session messages command.
type GetSessionMessagesOptions struct {
	CLIPath              string
	Cwd                  string
	Limit                int  // Maximum number of messages to return (0 = no limit)
	Offset               int  // Number of messages to skip
	IncludeSystemMessages bool // Include system messages in output
	IncludeHookEvents     bool // Include hook lifecycle events in output
}

// SessionMutationOptions configures session mutation commands.
type SessionMutationOptions struct {
	CLIPath string
	Cwd     string
}

// ListSessions returns all available sessions.
func ListSessions(opts *ListSessionsOptions) ([]SessionInfo, error) {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return nil, err
	}

	args := []string{"sessions", "list", "--output-format", "json"}
	if opts != nil {
		if opts.Limit > 0 {
			args = append(args, "--limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			args = append(args, "--offset", fmt.Sprintf("%d", opts.Offset))
		}
		if opts.IncludeWorktrees {
			args = append(args, "--include-worktrees")
		}
	}

	out, err := exec.Command(cliPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var sessions []SessionInfo
	if err := json.Unmarshal(out, &sessions); err != nil {
		return nil, fmt.Errorf("parse sessions: %w", err)
	}
	return sessions, nil
}

// GetSessionInfo returns metadata for a specific session.
func GetSessionInfo(sessionID string, opts *GetSessionInfoOptions) (*SessionInfo, error) {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return nil, err
	}

	out, err := exec.Command(cliPath, "sessions", "get", sessionID, "--output-format", "json").Output()
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	var info SessionInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return &info, nil
}

// GetSessionMessages returns all messages in a session.
func GetSessionMessages(sessionID string, opts *GetSessionMessagesOptions) ([]SessionMessage, error) {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return nil, err
	}

	args := []string{"sessions", "messages", sessionID, "--output-format", "json"}
	if opts != nil {
		if opts.Limit > 0 {
			args = append(args, "--limit", fmt.Sprintf("%d", opts.Limit))
		}
		if opts.Offset > 0 {
			args = append(args, "--offset", fmt.Sprintf("%d", opts.Offset))
		}
		if opts.IncludeSystemMessages {
			args = append(args, "--include-system-messages")
		}
		if opts.IncludeHookEvents {
			args = append(args, "--include-hook-events")
		}
	}

	out, err := exec.Command(cliPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("get session messages: %w", err)
	}

	var messages []SessionMessage
	if err := json.Unmarshal(out, &messages); err != nil {
		return nil, fmt.Errorf("parse messages: %w", err)
	}
	return messages, nil
}

// RenameSession renames a session.
func RenameSession(sessionID, title string, opts *SessionMutationOptions) error {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return err
	}

	if out, err := exec.Command(cliPath, "sessions", "rename", sessionID, title).CombinedOutput(); err != nil {
		return fmt.Errorf("rename session: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// TagSession sets or clears the tag on a session.
func TagSession(sessionID string, tag *string, opts *SessionMutationOptions) error {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return err
	}

	args := []string{"sessions", "tag", sessionID}
	if tag != nil {
		args = append(args, *tag)
	} else {
		args = append(args, "--clear")
	}

	if out, err := exec.Command(cliPath, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tag session: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// DeleteSession deletes a session.
func DeleteSession(sessionID string, opts *SessionMutationOptions) error {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return err
	}

	if out, err := exec.Command(cliPath, "sessions", "delete", sessionID).CombinedOutput(); err != nil {
		return fmt.Errorf("delete session: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

// ForkSessionResult contains the result of forking a session.
type ForkSessionResult struct {
	SessionID string `json:"session_id"`
}

// ForkSession creates a new session by forking an existing one.
// If upToMessageID is non-empty, only messages up to that ID are included.
// If title is non-empty, the forked session is given that title.
func ForkSession(sessionID string, opts *SessionMutationOptions, upToMessageID, title string) (*ForkSessionResult, error) {
	var explicitPath string
	if opts != nil {
		explicitPath = opts.CLIPath
	}
	cliPath, err := findCLIForSession(explicitPath)
	if err != nil {
		return nil, err
	}

	args := []string{"sessions", "fork", sessionID, "--output-format", "json"}
	if upToMessageID != "" {
		args = append(args, "--up-to", upToMessageID)
	}
	if title != "" {
		args = append(args, "--title", title)
	}

	out, err := exec.Command(cliPath, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("fork session: %w", err)
	}

	var result ForkSessionResult
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("parse fork result: %w", err)
	}
	return &result, nil
}

// findCLIForSession resolves the CLI path, using the provided override or
// searching standard locations.
func findCLIForSession(cliPath string) (string, error) {
	return clilookup.FindCLI(cliPath)
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add session.go
git commit -m "feat: add session management (list, get, messages, rename, tag, delete, fork)"
```

---

### Task 16: Basic example

**Files:**
- Create: `examples/basic/main.go`

- [ ] **Step 1: Write the basic example**

Create `examples/basic/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"

	agentsdk "github.com/anthropics/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	stream := agentsdk.Query(ctx, "What is 2+2? Reply with just the number.",
		agentsdk.WithMaxTurns(1),
	)
	defer stream.Close()

	for stream.Next() {
		msg := stream.Current()

		switch msg.Type {
		case "assistant":
			assistant, _ := msg.AsAssistant()
			for _, block := range assistant.Content {
				if text, ok := block.AsText(); ok {
					fmt.Print(text.Text)
				}
			}
		case "result":
			result, _ := msg.AsResult()
			fmt.Printf("\n\n--- Done (turns: %d, cost: $%.4f) ---\n",
				result.NumTurns,
				valueOrZero(result.TotalCostUSD),
			)
		}
	}

	if err := stream.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func valueOrZero(v *float64) float64 {
	if v != nil {
		return *v
	}
	return 0
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./examples/basic/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add examples/basic/main.go
git commit -m "docs: add basic one-shot query example"
```

---

### Task 17: Hooks example

**Files:**
- Create: `examples/hooks/main.go`

- [ ] **Step 1: Write the hooks example**

Create `examples/hooks/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"os"
	"regexp"

	agentsdk "github.com/anthropics/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()

	// Define hooks: log tool uses, auto-approve safe tools, block dangerous ones.
	hooks := map[agentsdk.HookEvent][]agentsdk.HookMatcher{
		agentsdk.HookPreToolUse: {
			{
				// Auto-approve read-only tools.
				Matcher: "Read|Glob|Grep",
				Hooks: []agentsdk.HookCallback{
					func(ctx context.Context, input agentsdk.HookInput, toolUseID string) (agentsdk.HookOutput, error) {
						fmt.Printf("[Hook] Auto-approving tool: %s\n", input.ToolName)
						return agentsdk.HookOutput{
							HookSpecificOutput: &agentsdk.HookSpecificOutput{
								HookEventName:      "PreToolUse",
								PermissionDecision: "allow",
							},
						}, nil
					},
				},
			},
			{
				// Block Bash commands containing "rm".
				Matcher: "Bash",
				Hooks: []agentsdk.HookCallback{
					func(ctx context.Context, input agentsdk.HookInput, toolUseID string) (agentsdk.HookOutput, error) {
						if cmd, ok := input.ToolInput["command"].(string); ok {
							if matched, _ := regexp.MatchString(`\brm\b`, cmd); matched {
								fmt.Printf("[Hook] BLOCKED dangerous command: %s\n", cmd)
								return agentsdk.HookOutput{
									Decision: "block",
									Reason:   "rm commands are not allowed",
								}, nil
							}
						}
						return agentsdk.HookOutput{
							HookSpecificOutput: &agentsdk.HookSpecificOutput{
								HookEventName:      "PreToolUse",
								PermissionDecision: "allow",
							},
						}, nil
					},
				},
			},
		},
		agentsdk.HookPostToolUse: {
			{
				Hooks: []agentsdk.HookCallback{
					func(ctx context.Context, input agentsdk.HookInput, toolUseID string) (agentsdk.HookOutput, error) {
						fmt.Printf("[Hook] Tool completed: %s\n", input.ToolName)
						return agentsdk.HookOutput{Continue: true}, nil
					},
				},
			},
		},
	}

	stream := agentsdk.Query(ctx, "List the Go files in the current directory",
		agentsdk.WithMaxTurns(5),
		agentsdk.WithHooks(hooks),
	)
	defer stream.Close()

	for stream.Next() {
		msg := stream.Current()

		switch msg.Type {
		case "assistant":
			assistant, _ := msg.AsAssistant()
			for _, block := range assistant.Content {
				if text, ok := block.AsText(); ok {
					fmt.Print(text.Text)
				}
				if toolUse, ok := block.AsToolUse(); ok {
					fmt.Printf("\n[Tool: %s]\n", toolUse.Name)
				}
			}
		case "result":
			result, _ := msg.AsResult()
			fmt.Printf("\n\n--- Done (turns: %d) ---\n", result.NumTurns)
		}
	}

	if err := stream.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./examples/hooks/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add examples/hooks/main.go
git commit -m "docs: add hooks example with tool logging"
```

---

### Task 18: MCP server example

**Files:**
- Create: `examples/mcp/main.go`

- [ ] **Step 1: Write the MCP server example**

Create `examples/mcp/main.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	agentsdk "github.com/anthropics/claude-agent-sdk-go"
)

type AddInput struct {
	A float64 `json:"a"`
	B float64 `json:"b"`
}

func main() {
	ctx := context.Background()

	// Define an in-process MCP server with a simple "add" tool.
	addTool := agentsdk.Tool("add", "Add two numbers together", func(ctx context.Context, input AddInput) (*agentsdk.McpToolResult, error) {
		result := input.A + input.B
		return &agentsdk.McpToolResult{
			Content: []agentsdk.McpToolContent{
				{Type: "text", Text: fmt.Sprintf("%g", result)},
			},
		}, nil
	})

	sdkServer := &agentsdk.McpSdkServer{
		Name:    "math-tools",
		Version: "1.0.0",
		Tools:   []agentsdk.McpTool{addTool},
	}

	stream := agentsdk.Query(ctx, "What is 123.45 + 678.90? Use the add tool.",
		agentsdk.WithMaxTurns(3),
		agentsdk.WithMcpServers(map[string]agentsdk.McpServerConfig{
			"math": {SDK: sdkServer},
		}),
	)
	defer stream.Close()

	for stream.Next() {
		msg := stream.Current()
		if assistant, ok := msg.AsAssistant(); ok {
			for _, block := range assistant.Content {
				if text, ok := block.AsText(); ok {
					fmt.Print(text.Text)
				}
			}
		}
	}

	if err := stream.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	_ = json.Marshal // suppress unused import warning in plan
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./examples/mcp/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add examples/mcp/main.go
git commit -m "docs: add MCP in-process server example with add tool"
```

---

### Task 19: Interactive client example

**Files:**
- Create: `examples/interactive/main.go`

- [ ] **Step 1: Write the interactive client example**

Create `examples/interactive/main.go`:

```go
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	agentsdk "github.com/anthropics/claude-agent-sdk-go"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := agentsdk.NewClient(
		agentsdk.WithMaxTurns(10),
		agentsdk.WithPermissionMode(agentsdk.PermissionBypassAll),
		agentsdk.WithAllowDangerouslySkipPermissions(),
	)

	if err := client.Connect(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Connect error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	// Print messages in background.
	go func() {
		for msg := range client.Messages() {
			switch msg.Type {
			case "assistant":
				assistant, _ := msg.AsAssistant()
				for _, block := range assistant.Content {
					if text, ok := block.AsText(); ok {
						fmt.Print(text.Text)
					}
				}
			case "result":
				fmt.Println("\n--- Turn complete ---")
			}
		}
	}()

	// Read user input.
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("\nYou: ")
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			fmt.Print("You: ")
			continue
		}
		if text == "/quit" {
			break
		}

		if err := client.Send(ctx, text); err != nil {
			fmt.Fprintf(os.Stderr, "Send error: %v\n", err)
			break
		}
		fmt.Print("\nYou: ")
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./examples/interactive/`
Expected: Clean build.

- [ ] **Step 3: Commit**

```bash
git add examples/interactive/main.go
git commit -m "docs: add interactive multi-turn client example"
```

---

### Task 20: End-to-end verification

**Files:** None (verification only)

- [ ] **Step 1: Build the full project**

Run: `go build ./...`
Expected: Clean build, no errors.

- [ ] **Step 2: Run all tests**

Run: `go test ./... -v`
Expected: All PASS.

- [ ] **Step 3: Verify public API surface**

Check that the following are exported and usable:

```
Functions: Query(), NewClient(), NewStreamFromTransport(), BuildCLIArgsForTest(),
           ListSessions(), GetSessionInfo(), GetSessionMessages(),
           RenameSession(), TagSession(), DeleteSession(),
           ForkSession(), Tool[T](), CreateSdkMcpServer(), WithAnnotations(),
           UnstableV2CreateSession(), UnstableV2ResumeSession(), UnstableV2Prompt()
Types:     Stream, Client, SDKMessage, ContentBlock, QueryOption,
           UserMessage, AssistantMessage, SystemMessage, ResultMessage,
           ToolResultMessage,
           StreamEvent, RateLimitEvent, RateLimitInfo, ContextUsage, ContextCategory,
           ServerInfo, RuntimeSettings, ReloadPluginsResult,
           TaskStartedMessage, TaskProgressMessage, TaskNotificationMessage, TaskUsage,
           TextBlock, ThinkingBlock, ToolUseBlock, ToolResultBlock,
           McpServerConfig, McpSdkServer, McpTool, McpToolResult, McpToolContent,
           McpServerStatus, McpServerInfo, McpToolInfo, McpProxyServerConfig,
           ToolAnnotations, ToolOption,
           HookEvent (27 constants), HookMatcher, HookCallback, HookInput, HookOutput, HookSpecificOutput,
           AgentDefinition, PermissionMode, Effort, ThinkingConfig,
           PermissionResult, PermissionUpdate, PermissionRule, PermissionDenial,
           ToolPermissionContext, CanUseToolFunc, ForkSessionResult, TaskBudget,
           SandboxSettings, SandboxNetworkConfig, SandboxIgnoreViolations,
           ToolConfig, AskUserQuestionConfig, PreviewFormat,
           SlashCommand, ModelInfo, AgentInfo, AccountInfo, FastModeState,
           V2Session,
           TestQueryConfig
Interfaces: SDKError
Client methods: Connect, Send, Messages, Close, Interrupt,
           SetModel, SetPermissionMode, SetMaxThinkingTokens, SetFastMode,
           GetContextUsage, McpStatus, ReconnectMcpServer, ToggleMcpServer, SetMcpServers,
           StopTask, RewindFiles, GetServerInfo, PromptSuggestion, EnableChannel,
           GetSettings, ReloadPlugins,
           SupportedCommands, SupportedModels, SupportedAgents, GetAccountInfo,
           SeedReadState, ApplyFlagSettings
Options:   WithModel, WithFallbackModel, WithMaxTurns, WithCwd, WithEnv, WithAPIKey, WithAllowedTools,
           WithTools, WithToolsPreset, WithDisallowedTools,
           WithPermissionMode, WithBypassPermissions, WithAllowDangerouslySkipPermissions,
           WithCanUseTool, WithPermissionPromptToolName,
           WithSystemPrompt, WithSystemPromptPreset, WithSystemPromptFile,
           WithMcpServers, WithHooks, WithAgents, WithMaxBudgetUSD,
           WithIncludePartialMessages, WithResume, WithContinue, WithForkSession,
           WithSessionID, WithDebug, WithDebugFile,
           WithPersistSession, WithBetas, WithEffort, WithThinking,
           WithTaskBudget, WithSandbox, WithUser, WithExtraArgs,
           WithFileCheckpointing, WithProcessTimeout, WithSettingSources,
           WithPlugins, WithAdditionalDirectories, WithStderr,
           WithMaxBufferSize, WithCLIPath, WithOutputFormat, WithToolConfig, ...
Errors:    SDKError (interface), CLINotFoundError, CLIConnectionError,
           ProcessError, JSONDecodeError, MessageParseError
```

- [ ] **Step 4: Final commit (if any cleanup needed)**

```bash
git status
# If clean, skip. Otherwise:
git add -A
git commit -m "chore: cleanup after Phase 1 Go Agent SDK implementation"
```

---

## Summary

Phase 1 delivers:
- **Core API**: `Query()` for one-shot streaming, `Client` for multi-turn sessions with bidirectional control protocol. `Query()` keeps stdin open during execution so control protocol responses (hooks, permissions, MCP) can flow back, only closing stdin after the result message.
- **Control protocol**: `controlHandler` sends an `initialize` handshake at startup to register hooks (with `callback_id` routing) and agents with the CLI. Routes `hook_callback` (by `callback_id`), `can_use_tool` permission checks, and `mcp_message` JSONRPC requests from the CLI process back to Go handlers. Control response envelope uses nested format: `{type: "control_response", response: {subtype: "success", request_id, response}}`.
- **Message types**: `SDKMessage` tagged union with `AsUser/AsAssistant/AsSystem/AsResult/AsToolResult/AsStreamEvent/AsRateLimit/AsTaskStarted/AsTaskProgress/AsTaskNotification`; `AssistantMessage` includes `Error` field; `SystemMessage` includes `Data` payload; `ResultMessage` includes `StructuredOutput`, `PermissionDenials`, `Errors`, `ModelUsage`, and `"paused"` subtype; `ToolResultMessage` for top-level tool results (distinct from `ToolResultBlock` content block); `RateLimitEvent` with status, resets_at, utilization, and overage fields; `TaskStartedMessage`, `TaskProgressMessage`, `TaskNotificationMessage` for background task/subagent tracking
- **Content blocks**: `ContentBlock` tagged union with `AsText/AsThinking/AsToolUse/AsToolResult`
- **Options**: 45+ functional options covering model, API key, tools (explicit list + preset), tool config (`ToolConfig` with `AskUserQuestion.PreviewFormat`), permissions (including `WithPermissionPromptToolName`), system prompt (string, preset+append, file), MCP, hooks, agents, betas, session persistence, fork, effort, thinking, structured output, task budget, sandbox, user identity, file checkpointing, process timeout, settings sources, plugins, debug file, extra args
- **Permissions**: `PermissionResult` with allow/deny/ask + input modification + dynamic `UpdatedPermissions` + `Suggestions` via `PermissionUpdate`/`PermissionRule` types; `CanUseToolFunc` callback with `ToolPermissionContext`; 5 permission modes (default, acceptEdits, plan, bypassPermissions, dontAsk)
- **Transport**: `SubprocessTransport` spawning `claude --print --output-format stream-json --input-format stream-json`; `ExitError()` method propagates process crash errors to `Stream`
- **CLI discovery**: Searches `$PATH`, `~/.claude/local/claude`, standard install locations
- **Session management**: List (with limit/offset/worktrees), get (with `IncludeSystemMessages`/`IncludeHookEvents`), rename, tag, delete, fork sessions via CLI commands; `SessionInfo` with full fields (summary, file_size, first_prompt, git_branch, cwd)
- **Error types**: `SDKError` common interface, `CLINotFoundError`, `CLIConnectionError`, `ProcessError`, `JSONDecodeError`, `MessageParseError`. Process exit errors are captured and surfaced via `Stream.Err()`
- **Hook types**: All 18 hook events (`SessionStart`, `SessionEnd`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `UserPromptSubmit`, `Stop`, `SubagentStart`, `SubagentStop`, `PreCompact`, `Notification`, `PermissionRequest`, `Setup`, `TeammateIdle`, `TaskCompleted`, `ConfigChange`, `WorktreeCreate`, `WorktreeRemove`) with `HookMatcher.Timeout`; rich `HookInput` (common + event-specific fields including `Source` for SessionStart, `ExitReason` for SessionEnd, `IdleDurationMs`/`TeammateID` for TeammateIdle, `TaskID`/`TaskStatus`/`OutputFile` for TaskCompleted, `ConfigPath`/`ChangeType` for ConfigChange, `WorktreePath`/`WorktreeBranch` for Worktree events) and `HookOutput` with `HookSpecificOutput` (permission decisions, input modification, system message injection, `UpdatedMCPToolOutput`); `HookOutput` supports `Async`/`AsyncTimeout` for fire-and-forget hooks and `StopReason` for stop control
- **MCP types**: Stdio, SSE, HTTP, and in-process SDK MCP server configs with consistent `type` field in JSON marshal; `CreateSdkMcpServer()` constructor; `ToolAnnotations` (readOnly, destructive, openWorld); `Tool[T]()` with `WithAnnotations()` option; SDK servers filtered from CLI args (handled in-process via JSONRPC protocol: `initialize`, `tools/list`, `tools/call`, `notifications/initialized`); runtime management via `Client` (reconnect, toggle, set servers, status with response routing)
- **Client control**: `Interrupt`, `SetModel`, `SetPermissionMode`, `SetMaxThinkingTokens`, `GetContextUsage` (with per-category breakdown via `ContextCategory`), `GetServerInfo`, `GetSettings` (resolved model/effort via `RuntimeSettings`), `PromptSuggestion`, `EnableChannel`, `ReloadPlugins` (returns `ReloadPluginsResult`), `McpStatus`, `ReconnectMcpServer`, `ToggleMcpServer`, `SetMcpServers`, `StopTask`, `RewindFiles`
- **Schema generation**: Reflection-based JSON schema from Go structs for `Tool[T]()` generic helper; handles pointers, unsigned ints, maps, slices, interfaces
- **Agent definitions**: Subagent configuration with `McpServers`, `Skills`, `InitialPrompt`, and `Memory` fields
- **Sandbox**: `SandboxSettings` with `IgnoreViolations` and `EnableWeakerNestedSandbox` (aligned with Python SDK)
- **CLI discovery**: Uses `npm config get prefix` (not deprecated `npm bin -g`) for npm global binary lookup; platform-aware process shutdown (`SIGTERM` on Unix, `Kill` on Windows)
- **V2 Preview API**: `UnstableV2CreateSession()` and `UnstableV2ResumeSession()` return a `V2Session` handle with `Send()` for session-oriented usage (wraps `Client` internally); marked unstable — API may change
- **CLI arg mapping**: Complete mapping from all `queryConfig` fields to CLI arguments including agents, tools/preset, system prompt variants, fork, sandbox, plugins, settings sources, debug file, permission prompt tool name, tool config
- **Examples**: Basic query, hooks (with PreToolUse/PostToolUse callbacks), MCP tools, interactive client
- **Tests**: Unit tests in separate `agentsdk_test` package with mock transport (implementing `ExitError()`) covering Stream iteration, content block parsing, rate limit events, CLI arg building

**Dependencies**: Zero external dependencies (stdlib only).

**Next phase**: Phase 2 (Agent Cards + Discovery) adds server-side agent registration and discovery in the agentserver repo.
