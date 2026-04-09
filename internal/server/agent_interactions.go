package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/agentserver/agentserver/internal/db"
)

// handleListInteractions returns the audit trail for a workspace.
// GET /api/workspaces/{wid}/agent-interactions
func (s *Server) handleListInteractions(w http.ResponseWriter, r *http.Request) {
	wid := chi.URLParam(r, "wid")
	if _, ok := s.requireWorkspaceMember(w, r, wid); !ok {
		return
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if o := r.URL.Query().Get("offset"); o != "" {
		if n, err := strconv.Atoi(o); err == nil && n >= 0 {
			offset = n
		}
	}

	items, err := s.DB.ListInteractions(wid, limit, offset)
	if err != nil {
		log.Printf("list interactions: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if items == nil {
		items = []db.AgentInteraction{}
	}

	type interactionResponse struct {
		ID          int64            `json:"id"`
		ActorID     *string          `json:"actor_id"`
		Action      string           `json:"action"`
		TargetID    string           `json:"target_id"`
		TargetType  string           `json:"target_type"`
		Detail      *json.RawMessage `json:"detail,omitempty"`
		CreatedAt   string           `json:"created_at"`
	}
	result := make([]interactionResponse, len(items))
	for i, item := range items {
		result[i] = interactionResponse{
			ID:         item.ID,
			ActorID:    item.ActorID,
			Action:     item.Action,
			TargetID:   item.TargetID,
			TargetType: item.TargetType,
			CreatedAt:  item.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
		if len(item.DetailJSON) > 0 && string(item.DetailJSON) != "null" {
			raw := json.RawMessage(item.DetailJSON)
			result[i].Detail = &raw
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// logInteraction is a DRY helper for audit logging.
func (s *Server) logInteraction(workspaceID string, actorID *string, action, targetID, targetType string, detail map[string]any) {
	detailJSON, _ := json.Marshal(detail)
	s.DB.LogInteraction(&db.AgentInteraction{
		WorkspaceID: workspaceID,
		ActorID:     actorID,
		Action:      action,
		TargetID:    targetID,
		TargetType:  targetType,
		DetailJSON:  detailJSON,
	})
}
