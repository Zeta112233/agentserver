package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type LLMModel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type WorkspaceLLMConfig struct {
	WorkspaceID string
	BaseURL     string
	APIKey      string
	Models      []LLMModel
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (db *DB) GetWorkspaceLLMConfig(workspaceID string) (*WorkspaceLLMConfig, error) {
	c := &WorkspaceLLMConfig{}
	var modelsJSON []byte
	err := db.QueryRow(
		`SELECT workspace_id, base_url, api_key, models, created_at, updated_at
		 FROM workspace_llm_config WHERE workspace_id = $1`,
		workspaceID,
	).Scan(&c.WorkspaceID, &c.BaseURL, &c.APIKey, &modelsJSON, &c.CreatedAt, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workspace llm config: %w", err)
	}
	if err := json.Unmarshal(modelsJSON, &c.Models); err != nil {
		return nil, fmt.Errorf("get workspace llm config: unmarshal models: %w", err)
	}
	return c, nil
}

func (db *DB) SetWorkspaceLLMConfig(workspaceID, baseURL, apiKey string, models []LLMModel) error {
	modelsJSON, err := json.Marshal(models)
	if err != nil {
		return fmt.Errorf("set workspace llm config: marshal models: %w", err)
	}
	_, err = db.Exec(
		`INSERT INTO workspace_llm_config (workspace_id, base_url, api_key, models, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (workspace_id) DO UPDATE SET
		   base_url = EXCLUDED.base_url,
		   api_key = EXCLUDED.api_key,
		   models = EXCLUDED.models,
		   updated_at = NOW()`,
		workspaceID, baseURL, apiKey, modelsJSON,
	)
	if err != nil {
		return fmt.Errorf("set workspace llm config: %w", err)
	}
	return nil
}

func (db *DB) DeleteWorkspaceLLMConfig(workspaceID string) error {
	_, err := db.Exec("DELETE FROM workspace_llm_config WHERE workspace_id = $1", workspaceID)
	if err != nil {
		return fmt.Errorf("delete workspace llm config: %w", err)
	}
	return nil
}
