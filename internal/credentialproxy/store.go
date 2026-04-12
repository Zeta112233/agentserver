package credentialproxy

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "github.com/lib/pq"
)

// BindingRow represents a credential binding row from the database.
type BindingRow struct {
	ID          string
	WorkspaceID string
	Kind        string
	DisplayName string
	ServerURL   string
	PublicMeta  json.RawMessage
	AuthType    string
	AuthBlob    []byte
	IsDefault   bool
}

// Store provides read-only access to credential_bindings.
type Store struct {
	db *sql.DB
}

// NewStore connects to PostgreSQL. No migrations are run (agentserver owns the schema).
func NewStore(databaseURL string) (*Store, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks the database connection.
func (s *Store) Ping() error {
	return s.db.Ping()
}

const bindingColumns = `id, workspace_id, kind, display_name, server_url, public_meta, auth_type, auth_blob, is_default`

func scanBindingRow(scanner interface{ Scan(...interface{}) error }) (*BindingRow, error) {
	b := &BindingRow{}
	err := scanner.Scan(&b.ID, &b.WorkspaceID, &b.Kind, &b.DisplayName, &b.ServerURL, &b.PublicMeta, &b.AuthType, &b.AuthBlob, &b.IsDefault)
	return b, err
}

// GetBinding fetches a single binding by (workspace_id, kind, id).
func (s *Store) GetBinding(workspaceID, kind, bindingID string) (*BindingRow, error) {
	b, err := scanBindingRow(s.db.QueryRow(
		`SELECT `+bindingColumns+` FROM credential_bindings WHERE workspace_id = $1 AND kind = $2 AND id = $3`,
		workspaceID, kind, bindingID,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get binding: %w", err)
	}
	return b, nil
}

// ListBindings returns all bindings for a (workspace, kind) pair.
func (s *Store) ListBindings(workspaceID, kind string) ([]*BindingRow, error) {
	rows, err := s.db.Query(
		`SELECT `+bindingColumns+` FROM credential_bindings WHERE workspace_id = $1 AND kind = $2 ORDER BY created_at`,
		workspaceID, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	defer rows.Close()

	var result []*BindingRow
	for rows.Next() {
		b, err := scanBindingRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan binding: %w", err)
		}
		result = append(result, b)
	}
	return result, rows.Err()
}
