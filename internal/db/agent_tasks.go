package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/lib/pq"
)

// AgentTask represents a delegated task.
type AgentTask struct {
	ID              string
	WorkspaceID     string
	RequesterID     sql.NullString
	TargetID        string
	SessionID       sql.NullString
	Skill           sql.NullString
	Prompt          string
	SystemContext   sql.NullString
	Status          string
	ResultJSON      *json.RawMessage
	FailureReason   sql.NullString
	TimeoutSeconds  int
	DelegationChain []string
	TotalCostUSD    *float64
	NumTurns        int
	CreatedAt       time.Time
	AcceptedAt      sql.NullTime
	CompletedAt     sql.NullTime
}

func (db *DB) CreateAgentTask(t *AgentTask) error {
	if t.DelegationChain == nil {
		t.DelegationChain = []string{}
	}
	_, err := db.Exec(
		`INSERT INTO agent_tasks (id, workspace_id, requester_id, target_id, session_id, skill, prompt, system_context, timeout_seconds, delegation_chain)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		t.ID, t.WorkspaceID, t.RequesterID, t.TargetID, t.SessionID, t.Skill, t.Prompt, t.SystemContext, t.TimeoutSeconds, pq.Array(t.DelegationChain),
	)
	return err
}

func (db *DB) GetAgentTask(id string) (*AgentTask, error) {
	t := &AgentTask{}
	var chain pq.StringArray
	err := db.QueryRow(
		`SELECT id, workspace_id, requester_id, target_id, session_id, skill, prompt, system_context,
		        status, result_json, failure_reason, timeout_seconds, delegation_chain, total_cost_usd,
		        num_turns, created_at, accepted_at, completed_at
		 FROM agent_tasks WHERE id = $1`, id,
	).Scan(&t.ID, &t.WorkspaceID, &t.RequesterID, &t.TargetID, &t.SessionID, &t.Skill, &t.Prompt, &t.SystemContext,
		&t.Status, &t.ResultJSON, &t.FailureReason, &t.TimeoutSeconds, &chain, &t.TotalCostUSD,
		&t.NumTurns, &t.CreatedAt, &t.AcceptedAt, &t.CompletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.DelegationChain = chain
	return t, nil
}

func (db *DB) ListAgentTasksByWorkspace(workspaceID string, limit int) ([]AgentTask, error) {
	rows, err := db.Query(
		`SELECT id, workspace_id, requester_id, target_id, session_id, skill, prompt, system_context,
		        status, result_json, failure_reason, timeout_seconds, delegation_chain, total_cost_usd,
		        num_turns, created_at, accepted_at, completed_at
		 FROM agent_tasks WHERE workspace_id = $1 ORDER BY created_at DESC LIMIT $2`,
		workspaceID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []AgentTask
	for rows.Next() {
		var t AgentTask
		var chain pq.StringArray
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.RequesterID, &t.TargetID, &t.SessionID, &t.Skill, &t.Prompt, &t.SystemContext,
			&t.Status, &t.ResultJSON, &t.FailureReason, &t.TimeoutSeconds, &chain, &t.TotalCostUSD,
			&t.NumTurns, &t.CreatedAt, &t.AcceptedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		t.DelegationChain = chain
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (db *DB) UpdateAgentTaskStatus(id, status string) error {
	q := `UPDATE agent_tasks SET status = $2, updated_at = NOW()`
	switch status {
	case "assigned", "running":
		q += `, accepted_at = COALESCE(accepted_at, NOW())`
	case "completed", "failed", "cancelled":
		q += `, completed_at = NOW()`
	}
	q += ` WHERE id = $1`
	_, err := db.Exec(q, id, status)
	return err
}

func (db *DB) UpdateAgentTaskResult(id string, resultJSON json.RawMessage, totalCostUSD *float64, numTurns int) error {
	_, err := db.Exec(
		`UPDATE agent_tasks SET result_json = $2, total_cost_usd = $3, num_turns = $4, status = 'completed', completed_at = NOW()
		 WHERE id = $1`,
		id, resultJSON, totalCostUSD, numTurns,
	)
	return err
}

func (db *DB) FailAgentTask(id, reason string) error {
	_, err := db.Exec(
		`UPDATE agent_tasks SET status = 'failed', failure_reason = $2, completed_at = NOW()
		 WHERE id = $1`,
		id, reason,
	)
	return err
}

// ListPendingAgentTasksByTarget returns pending tasks assigned to a target sandbox.
// Also atomically marks them as 'assigned' to prevent double-pickup.
func (db *DB) ListPendingAgentTasksByTarget(targetID string, limit int) ([]AgentTask, error) {
	rows, err := db.Query(
		`UPDATE agent_tasks SET status = 'assigned', accepted_at = NOW(), updated_at = NOW()
		 WHERE id IN (
		   SELECT id FROM agent_tasks
		   WHERE target_id = $1 AND status = 'pending'
		   ORDER BY created_at ASC LIMIT $2
		   FOR UPDATE SKIP LOCKED
		 ) RETURNING id, workspace_id, requester_id, target_id, session_id, skill, prompt, system_context,
		   status, result_json, failure_reason, timeout_seconds, delegation_chain, total_cost_usd,
		   num_turns, created_at, accepted_at, completed_at`,
		targetID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []AgentTask
	for rows.Next() {
		var t AgentTask
		var chain pq.StringArray
		if err := rows.Scan(&t.ID, &t.WorkspaceID, &t.RequesterID, &t.TargetID, &t.SessionID, &t.Skill, &t.Prompt, &t.SystemContext,
			&t.Status, &t.ResultJSON, &t.FailureReason, &t.TimeoutSeconds, &chain, &t.TotalCostUSD,
			&t.NumTurns, &t.CreatedAt, &t.AcceptedAt, &t.CompletedAt); err != nil {
			return nil, err
		}
		t.DelegationChain = chain
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}
