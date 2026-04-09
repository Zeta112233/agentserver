package server

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/agentserver/agentserver/internal/db"
	"github.com/google/uuid"
)

// handleSendMessage sends a message to another agent's mailbox.
// POST /api/agent/mailbox/send
// Auth: proxy_token (Bearer)
func (s *Server) handleSendMessage(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}
	sbx, err := s.DB.GetSandboxByAnyToken(token)
	if err != nil || sbx == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req struct {
		To      string `json:"to"`
		Text    string `json:"text"`
		MsgType string `json:"msg_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.To == "" || req.Text == "" {
		http.Error(w, "to and text required", http.StatusBadRequest)
		return
	}
	if req.MsgType == "" {
		req.MsgType = "text"
	}

	// Validate target exists and is in the same workspace.
	targetSbx, err := s.DB.GetSandbox(req.To)
	if err != nil || targetSbx == nil {
		http.Error(w, "target agent not found", http.StatusNotFound)
		return
	}
	if targetSbx.WorkspaceID != sbx.WorkspaceID {
		http.Error(w, "target not in same workspace", http.StatusForbidden)
		return
	}

	msgID := "msg_" + uuid.New().String()
	msg := &db.AgentMessage{
		ID:          msgID,
		WorkspaceID: sbx.WorkspaceID,
		FromID:      sbx.ID,
		ToID:        req.To,
		Text:        req.Text,
		MsgType:     req.MsgType,
	}

	if err := s.DB.SendMessage(msg); err != nil {
		log.Printf("send message: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.logInteraction(sbx.WorkspaceID, &sbx.ID, "message_sent", msgID, "message", map[string]any{
		"to": req.To, "msg_type": req.MsgType,
	})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"message_id": msgID,
		"status":     "sent",
	})
}

// handleReadInbox reads unread messages for the calling agent.
// GET /api/agent/mailbox/inbox
// Auth: proxy_token (Bearer)
func (s *Server) handleReadInbox(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("Authorization")
	if len(token) > 7 && token[:7] == "Bearer " {
		token = token[7:]
	} else {
		http.Error(w, "missing authorization", http.StatusUnauthorized)
		return
	}
	sbx, err := s.DB.GetSandboxByAnyToken(token)
	if err != nil || sbx == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	limit := 10
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	msgs, err := s.DB.ReadInbox(sbx.ID, limit)
	if err != nil {
		log.Printf("read inbox: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []db.AgentMessage{}
	}

	type msgResponse struct {
		ID        string `json:"id"`
		From      string `json:"from"`
		Text      string `json:"text"`
		MsgType   string `json:"msg_type"`
		CreatedAt string `json:"created_at"`
	}
	result := make([]msgResponse, len(msgs))
	for i, m := range msgs {
		result[i] = msgResponse{
			ID:        m.ID,
			From:      m.FromID,
			Text:      m.Text,
			MsgType:   m.MsgType,
			CreatedAt: m.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}
