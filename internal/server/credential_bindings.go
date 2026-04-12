package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
	"github.com/agentserver/agentserver/internal/crypto"
	"github.com/agentserver/agentserver/internal/db"
)

func (s *Server) handleListCredentialBindings(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")

	bindings, err := s.DB.ListCredentialBindingsMeta(wsID, kind)
	if err != nil {
		log.Printf("list credential bindings: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type bindingResp struct {
		ID          string         `json:"id"`
		DisplayName string         `json:"display_name"`
		ServerURL   string         `json:"server_url"`
		AuthType    string         `json:"auth_type"`
		PublicMeta  json.RawMessage `json:"public_meta"`
		IsDefault   bool           `json:"is_default"`
		CreatedAt   string         `json:"created_at"`
	}

	result := make([]bindingResp, 0, len(bindings))
	for _, b := range bindings {
		result = append(result, bindingResp{
			ID:          b.ID,
			DisplayName: b.DisplayName,
			ServerURL:   b.ServerURL,
			AuthType:    b.AuthType,
			PublicMeta:  b.PublicMeta,
			IsDefault:   b.IsDefault,
			CreatedAt:   b.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleCreateCredentialBinding(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")

	if len(s.EncryptionKey) == 0 {
		http.Error(w, "credential proxy not configured", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
		Config      string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.DisplayName == "" || req.Config == "" {
		http.Error(w, "display_name and config are required", http.StatusBadRequest)
		return
	}

	// Look up the provider and validate the upload.
	prov, err := provider.Lookup(kind)
	if err != nil {
		http.Error(w, fmt.Sprintf("unknown credential kind %q", kind), http.StatusBadRequest)
		return
	}

	result, err := prov.ParseUpload(r.Header.Get("Content-Type"), []byte(req.Config))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid config: %v", err), http.StatusBadRequest)
		return
	}

	// Use the user-provided display name if given, otherwise fall back to what the parser returned.
	displayName := req.DisplayName
	if displayName == "" {
		displayName = result.DisplayName
	}

	// Generate binding ID.
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		log.Printf("generate binding id: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	bindingID := base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(idBytes)

	// Encrypt auth secret.
	authBlob, err := crypto.Encrypt(s.EncryptionKey, result.AuthSecret)
	if err != nil {
		log.Printf("encrypt credential: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Check if this is the first binding for the (workspace, kind) pair.
	count, err := s.DB.CountCredentialBindings(wsID, kind)
	if err != nil {
		log.Printf("count credential bindings: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	isDefault := count == 0

	publicMetaJSON, _ := json.Marshal(result.PublicMeta)

	binding := &db.CredentialBinding{
		ID:          bindingID,
		WorkspaceID: wsID,
		Kind:        kind,
		DisplayName: displayName,
		ServerURL:   result.ServerURL,
		PublicMeta:  publicMetaJSON,
		AuthType:    result.AuthType,
		AuthBlob:    authBlob,
		IsDefault:   isDefault,
	}

	if err := s.DB.CreateCredentialBinding(binding); err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			http.Error(w, "a binding with this display name already exists", http.StatusConflict)
			return
		}
		log.Printf("create credential binding: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":           bindingID,
		"display_name": displayName,
		"server_url":   result.ServerURL,
		"auth_type":    result.AuthType,
		"public_meta":  result.PublicMeta,
		"is_default":   isDefault,
	})
}

func (s *Server) handleDeleteCredentialBinding(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	bindingID := chi.URLParam(r, "bindingId")

	binding, err := s.DB.GetCredentialBinding(bindingID)
	if err != nil {
		log.Printf("get credential binding: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if binding == nil || binding.WorkspaceID != wsID || binding.Kind != kind {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// If this is the default and not the last one, reject.
	if binding.IsDefault {
		count, err := s.DB.CountCredentialBindings(wsID, kind)
		if err != nil {
			log.Printf("count credential bindings: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if count > 1 {
			http.Error(w, "cannot delete the default binding while others exist; use set-default first", http.StatusConflict)
			return
		}
	}

	if err := s.DB.DeleteCredentialBinding(bindingID); err != nil {
		log.Printf("delete credential binding: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetDefaultCredentialBinding(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	bindingID := chi.URLParam(r, "bindingId")

	if err := s.DB.SetCredentialBindingDefault(wsID, kind, bindingID); err != nil {
		log.Printf("set default credential binding: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePatchCredentialBinding(w http.ResponseWriter, r *http.Request) {
	wsID := chi.URLParam(r, "id")
	kind := chi.URLParam(r, "kind")
	bindingID := chi.URLParam(r, "bindingId")

	binding, err := s.DB.GetCredentialBinding(bindingID)
	if err != nil {
		log.Printf("get credential binding: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if binding == nil || binding.WorkspaceID != wsID || binding.Kind != kind {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	var req struct {
		DisplayName *string `json:"display_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.DisplayName != nil && *req.DisplayName != "" {
		_, err := s.DB.Exec(
			`UPDATE credential_bindings SET display_name = $1, updated_at = NOW() WHERE id = $2`,
			*req.DisplayName, bindingID,
		)
		if err != nil {
			log.Printf("update credential binding: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
