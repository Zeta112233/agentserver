package server

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/agentserver/agentserver/internal/db"
)

// handleListAgentCards returns all agent cards in a workspace.
// GET /api/workspaces/{wid}/agents
func (s *Server) handleListAgentCards(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "wid")
	cards, err := s.DB.ListAgentCardsByWorkspace(wid)
	if err != nil {
		log.Printf("list agent cards: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if cards == nil {
		cards = []db.AgentCard{}
	}

	type cardResponse struct {
		AgentID     string          `json:"agent_id"`
		DisplayName string          `json:"display_name"`
		Description string          `json:"description"`
		AgentType   string          `json:"agent_type"`
		Status      string          `json:"status"`
		Card        json.RawMessage `json:"card"`
		Version     int             `json:"version"`
	}

	result := make([]cardResponse, len(cards))
	for i, c := range cards {
		result[i] = cardResponse{
			AgentID:     c.SandboxID,
			DisplayName: c.DisplayName,
			Description: c.Description,
			AgentType:   c.AgentType,
			Status:      c.AgentStatus,
			Card:        c.CardJSON,
			Version:     c.Version,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// handleGetAgentCard returns a single agent card.
// GET /api/agents/{sandboxId}
func (s *Server) handleGetAgentCard(w http.ResponseWriter, r *http.Request) {
	sandboxID := chi.URLParam(r, "sandboxId")
	card, err := s.DB.GetAgentCard(sandboxID)
	if err != nil {
		log.Printf("get agent card: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if card == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"agent_id":     card.SandboxID,
		"display_name": card.DisplayName,
		"description":  card.Description,
		"agent_type":   card.AgentType,
		"status":       card.AgentStatus,
		"card":         card.CardJSON,
		"version":      card.Version,
	})
}

// handleRegisterAgentCard registers or updates an agent card.
// POST /api/agent/discovery/cards
// Auth: proxy_token (agent self-registration)
func (s *Server) handleRegisterAgentCard(w http.ResponseWriter, r *http.Request) {
	// Authenticate via proxy_token.
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}

	sbx, err := s.DB.GetSandboxByProxyToken(token)
	if err != nil || sbx == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		DisplayName string          `json:"display_name"`
		Description string          `json:"description"`
		AgentType   string          `json:"agent_type"`
		Card        json.RawMessage `json:"card"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = sbx.Name
	}
	if req.AgentType == "" {
		req.AgentType = "claudecode"
	}
	if len(req.Card) == 0 {
		req.Card = json.RawMessage("{}")
	}

	card := &db.AgentCard{
		SandboxID:   sbx.ID,
		WorkspaceID: sbx.WorkspaceID,
		AgentType:   req.AgentType,
		DisplayName: req.DisplayName,
		Description: req.Description,
		CardJSON:    req.Card,
		AgentStatus: "available",
	}

	if err := s.DB.UpsertAgentCard(card); err != nil {
		log.Printf("register agent card: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
