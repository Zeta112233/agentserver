package db

import (
	"database/sql"
	"time"
)

// AgentMessage represents a mailbox message between agents.
type AgentMessage struct {
	ID          string
	WorkspaceID string
	FromID      string
	ToID        string
	Text        string
	MsgType     string
	CreatedAt   time.Time
	ReadAt      sql.NullTime
}

func (db *DB) SendMessage(msg *AgentMessage) error {
	_, err := db.Exec(
		`INSERT INTO agent_mailbox (id, workspace_id, from_id, to_id, text, msg_type)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		msg.ID, msg.WorkspaceID, msg.FromID, msg.ToID, msg.Text, msg.MsgType,
	)
	return err
}

// ReadInbox returns unread messages for the given sandbox, atomically marking them as read.
func (db *DB) ReadInbox(toID string, limit int) ([]AgentMessage, error) {
	rows, err := db.Query(
		`UPDATE agent_mailbox SET read_at = NOW()
		 WHERE id IN (
		   SELECT id FROM agent_mailbox
		   WHERE to_id = $1 AND read_at IS NULL
		   ORDER BY created_at ASC LIMIT $2
		   FOR UPDATE SKIP LOCKED
		 ) RETURNING id, workspace_id, from_id, to_id, text, msg_type, created_at, read_at`,
		toID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []AgentMessage
	for rows.Next() {
		var m AgentMessage
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.FromID, &m.ToID, &m.Text, &m.MsgType, &m.CreatedAt, &m.ReadAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}
