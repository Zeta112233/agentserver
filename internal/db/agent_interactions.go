package db

import (
	"encoding/json"
	"log"
	"time"
)

// AgentInteraction is an audit trail entry for multi-agent activity.
type AgentInteraction struct {
	ID          int64
	WorkspaceID string
	ActorID     *string
	Action      string // "task_created", "task_status_changed", "task_cancelled", "message_sent"
	TargetID    string
	TargetType  string // "task" or "message"
	DetailJSON  json.RawMessage
	CreatedAt   time.Time
}

// LogInteraction inserts an audit entry. Errors are logged but not returned (fire-and-forget).
func (db *DB) LogInteraction(i *AgentInteraction) {
	_, err := db.Exec(
		`INSERT INTO agent_interactions (workspace_id, actor_id, action, target_id, target_type, detail_json)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		i.WorkspaceID, i.ActorID, i.Action, i.TargetID, i.TargetType, i.DetailJSON,
	)
	if err != nil {
		log.Printf("audit log: %v", err)
	}
}

func (db *DB) ListInteractions(workspaceID string, limit, offset int) ([]AgentInteraction, error) {
	rows, err := db.Query(
		`SELECT id, workspace_id, actor_id, action, target_id, target_type, detail_json, created_at
		 FROM agent_interactions WHERE workspace_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		workspaceID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []AgentInteraction
	for rows.Next() {
		var i AgentInteraction
		if err := rows.Scan(&i.ID, &i.WorkspaceID, &i.ActorID, &i.Action, &i.TargetID, &i.TargetType, &i.DetailJSON, &i.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
