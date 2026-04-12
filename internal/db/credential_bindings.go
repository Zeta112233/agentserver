package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// CredentialBinding represents a row in the credential_bindings table.
type CredentialBinding struct {
	ID          string
	WorkspaceID string
	Kind        string
	DisplayName string
	ServerURL   string
	PublicMeta  json.RawMessage
	AuthType    string
	AuthBlob    []byte
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CredentialBindingMeta is a credential binding without the encrypted auth blob.
type CredentialBindingMeta struct {
	ID          string
	WorkspaceID string
	Kind        string
	DisplayName string
	ServerURL   string
	PublicMeta  json.RawMessage
	AuthType    string
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const credBindingColumns = `id, workspace_id, kind, display_name, server_url, public_meta, auth_type, auth_blob, is_default, created_at, updated_at`
const credBindingMetaColumns = `id, workspace_id, kind, display_name, server_url, public_meta, auth_type, is_default, created_at, updated_at`

func scanCredentialBinding(scanner interface{ Scan(...interface{}) error }) (*CredentialBinding, error) {
	b := &CredentialBinding{}
	err := scanner.Scan(&b.ID, &b.WorkspaceID, &b.Kind, &b.DisplayName, &b.ServerURL, &b.PublicMeta, &b.AuthType, &b.AuthBlob, &b.IsDefault, &b.CreatedAt, &b.UpdatedAt)
	return b, err
}

func scanCredentialBindingMeta(scanner interface{ Scan(...interface{}) error }) (*CredentialBindingMeta, error) {
	b := &CredentialBindingMeta{}
	err := scanner.Scan(&b.ID, &b.WorkspaceID, &b.Kind, &b.DisplayName, &b.ServerURL, &b.PublicMeta, &b.AuthType, &b.IsDefault, &b.CreatedAt, &b.UpdatedAt)
	return b, err
}

func (db *DB) CreateCredentialBinding(b *CredentialBinding) error {
	_, err := db.Exec(
		`INSERT INTO credential_bindings (id, workspace_id, kind, display_name, server_url, public_meta, auth_type, auth_blob, is_default)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		b.ID, b.WorkspaceID, b.Kind, b.DisplayName, b.ServerURL, b.PublicMeta, b.AuthType, b.AuthBlob, b.IsDefault,
	)
	if err != nil {
		return fmt.Errorf("create credential binding: %w", err)
	}
	return nil
}

func (db *DB) GetCredentialBinding(id string) (*CredentialBinding, error) {
	b, err := scanCredentialBinding(db.QueryRow(
		`SELECT `+credBindingColumns+` FROM credential_bindings WHERE id = $1`, id,
	))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get credential binding: %w", err)
	}
	return b, nil
}

func (db *DB) ListCredentialBindings(workspaceID, kind string) ([]*CredentialBinding, error) {
	rows, err := db.Query(
		`SELECT `+credBindingColumns+` FROM credential_bindings WHERE workspace_id = $1 AND kind = $2 ORDER BY created_at`,
		workspaceID, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("list credential bindings: %w", err)
	}
	defer rows.Close()

	var result []*CredentialBinding
	for rows.Next() {
		b, err := scanCredentialBinding(rows)
		if err != nil {
			return nil, fmt.Errorf("scan credential binding: %w", err)
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

// ListCredentialBindingsMeta returns bindings without the encrypted auth blob.
func (db *DB) ListCredentialBindingsMeta(workspaceID, kind string) ([]*CredentialBindingMeta, error) {
	rows, err := db.Query(
		`SELECT `+credBindingMetaColumns+` FROM credential_bindings WHERE workspace_id = $1 AND kind = $2 ORDER BY created_at`,
		workspaceID, kind,
	)
	if err != nil {
		return nil, fmt.Errorf("list credential binding metas: %w", err)
	}
	defer rows.Close()

	var result []*CredentialBindingMeta
	for rows.Next() {
		b, err := scanCredentialBindingMeta(rows)
		if err != nil {
			return nil, fmt.Errorf("scan credential binding meta: %w", err)
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

func (db *DB) DeleteCredentialBinding(id string) error {
	_, err := db.Exec(`DELETE FROM credential_bindings WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete credential binding: %w", err)
	}
	return nil
}

// SetCredentialBindingDefault atomically switches the default binding for a (workspace, kind) pair.
func (db *DB) SetCredentialBindingDefault(workspaceID, kind, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Clear existing default.
	if _, err := tx.Exec(
		`UPDATE credential_bindings SET is_default = FALSE, updated_at = NOW()
		 WHERE workspace_id = $1 AND kind = $2 AND is_default = TRUE`,
		workspaceID, kind,
	); err != nil {
		return fmt.Errorf("clear default: %w", err)
	}

	// Set new default.
	res, err := tx.Exec(
		`UPDATE credential_bindings SET is_default = TRUE, updated_at = NOW()
		 WHERE id = $1 AND workspace_id = $2 AND kind = $3`,
		id, workspaceID, kind,
	)
	if err != nil {
		return fmt.Errorf("set default: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("credential binding %s not found in workspace %s kind %s", id, workspaceID, kind)
	}

	return tx.Commit()
}

func (db *DB) CountCredentialBindings(workspaceID, kind string) (int, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM credential_bindings WHERE workspace_id = $1 AND kind = $2`,
		workspaceID, kind,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count credential bindings: %w", err)
	}
	return count, nil
}
