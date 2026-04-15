package agentsdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AgentCard describes a discovered agent in the workspace.
type AgentCard struct {
	AgentID     string          `json:"agent_id"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	AgentType   string          `json:"agent_type"`
	Status      string          `json:"status"` // "available", "busy", "offline"
	Card        json.RawMessage `json:"card"`
	Version     int             `json:"version"`
}

// DelegateTaskRequest is the request body for delegating a task to another agent.
type DelegateTaskRequest struct {
	TargetID       string   `json:"target_id"`
	Prompt         string   `json:"prompt"`
	Skill          string   `json:"skill,omitempty"`
	SystemContext  string   `json:"system_context,omitempty"`
	MaxTurns       int      `json:"max_turns,omitempty"`
	MaxBudgetUSD   float64  `json:"max_budget_usd,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
	DelegationChain []string `json:"delegation_chain,omitempty"`
}

// DelegateTaskResponse is the response from creating a delegated task.
type DelegateTaskResponse struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

// TaskInfo describes the status of a delegated task.
type TaskInfo struct {
	TaskID        string          `json:"task_id"`
	WorkspaceID   string          `json:"workspace_id"`
	RequesterID   string          `json:"requester_id"`
	TargetID      string          `json:"target_id"`
	Prompt        string          `json:"prompt"`
	Status        string          `json:"status"` // pending, assigned, running, completed, failed, cancelled
	Skill         string          `json:"skill,omitempty"`
	SessionID     string          `json:"session_id,omitempty"`
	NumTurns      int             `json:"num_turns,omitempty"`
	TotalCostUSD  float64         `json:"total_cost_usd,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	FailureReason string          `json:"failure_reason,omitempty"`
	Output        string          `json:"output,omitempty"`
	CreatedAt     string          `json:"created_at"`
	CompletedAt   string          `json:"completed_at,omitempty"`
}

// SendMessageRequest is the request body for sending a message to another agent.
type SendMessageRequest struct {
	To      string `json:"to"`
	Text    string `json:"text"`
	MsgType string `json:"msg_type,omitempty"` // defaults to "text"
}

// SendMessageResponse is the response from sending a message.
type SendMessageResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

// InboxMessage is a message received from another agent.
type InboxMessage struct {
	ID        string `json:"id"`
	From      string `json:"from"`
	Text      string `json:"text"`
	MsgType   string `json:"msg_type"`
	CreatedAt string `json:"created_at"`
}

// DiscoverAgents returns all agents in the same workspace.
func (c *Client) DiscoverAgents(ctx context.Context) ([]AgentCard, error) {
	resp, err := c.agentAPI(ctx, http.MethodGet, "/api/agent/discovery/agents", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("discover agents failed (%d): %s", resp.StatusCode, body)
	}

	var cards []AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&cards); err != nil {
		return nil, fmt.Errorf("decode agents: %w", err)
	}
	return cards, nil
}

// DelegateTask creates a task targeting another agent in the same workspace.
func (c *Client) DelegateTask(ctx context.Context, req DelegateTaskRequest) (*DelegateTaskResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.agentAPI(ctx, http.MethodPost, "/api/agent/tasks", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("delegate task failed (%d): %s", resp.StatusCode, respBody)
	}

	var result DelegateTaskResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// GetTask retrieves the status of a task. Set includeOutput to true to include
// the full task output from session events.
func (c *Client) GetTask(ctx context.Context, taskID string, includeOutput bool) (*TaskInfo, error) {
	path := "/api/agent/tasks/" + taskID
	if includeOutput {
		path += "?include_output=true"
	}

	resp, err := c.agentAPI(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get task failed (%d): %s", resp.StatusCode, body)
	}

	var info TaskInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode task: %w", err)
	}
	return &info, nil
}

// WaitForTask polls a task until it reaches a terminal status (completed, failed,
// or cancelled) or the context is cancelled. Returns the final task info.
func (c *Client) WaitForTask(ctx context.Context, taskID string, pollInterval time.Duration) (*TaskInfo, error) {
	if pollInterval <= 0 {
		pollInterval = 3 * time.Second
	}
	for {
		info, err := c.GetTask(ctx, taskID, true)
		if err != nil {
			return nil, err
		}
		switch info.Status {
		case "completed", "failed", "cancelled":
			return info, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}
}

// SendMessage sends an async message to another agent's mailbox.
func (c *Client) SendMessage(ctx context.Context, req SendMessageRequest) (*SendMessageResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := c.agentAPI(ctx, http.MethodPost, "/api/agent/mailbox/send", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("send message failed (%d): %s", resp.StatusCode, respBody)
	}

	var result SendMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}

// ReadInbox retrieves unread messages from this agent's mailbox.
// Messages are marked as read upon retrieval. Limit defaults to 10.
func (c *Client) ReadInbox(ctx context.Context, limit int) ([]InboxMessage, error) {
	path := "/api/agent/mailbox/inbox"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}

	resp, err := c.agentAPI(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("read inbox failed (%d): %s", resp.StatusCode, body)
	}

	var messages []InboxMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decode messages: %w", err)
	}
	return messages, nil
}

// agentAPI makes an authenticated HTTP request to the agentserver using the proxy token.
func (c *Client) agentAPI(ctx context.Context, method, path string, body []byte) (*http.Response, error) {
	if c.reg == nil {
		return nil, fmt.Errorf("not registered: call Register() or SetRegistration() first")
	}

	url := strings.TrimRight(c.config.ServerURL, "/") + path
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.reg.ProxyToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return http.DefaultClient.Do(req)
}
