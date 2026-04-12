package credentialproxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/agentserver/agentserver/internal/credentialproxy/provider"
	"github.com/agentserver/agentserver/internal/crypto"
	"github.com/go-chi/chi/v5"
)

var bindingIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,64}$`)

// Server is the credential proxy HTTP server.
type Server struct {
	config     Config
	store      *Store
	logger     *slog.Logger
	httpClient *http.Client
}

// NewServer creates a new credential proxy server.
func NewServer(cfg Config, store *Store, logger *slog.Logger) *Server {
	return &Server{
		config: cfg,
		store:  store,
		logger: logger,
	}
}

// Routes returns the HTTP handler for the credential proxy.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if err := s.store.Ping(); err != nil {
			http.Error(w, "database unreachable", http.StatusServiceUnavailable)
			return
		}
		resp := map[string]any{
			"status":                 "ok",
			"allow_private_upstreams": s.config.AllowPrivateUpstreams,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// /{kind}/{bid}/* -> auth middleware -> provider dispatch
	r.HandleFunc("/{kind}/{bid}/*", s.handleProxy)
	r.HandleFunc("/{kind}/{bid}", s.handleProxy)

	return r
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request) {
	kind := chi.URLParam(r, "kind")
	bid := chi.URLParam(r, "bid")

	// Validate binding ID format.
	if !bindingIDPattern.MatchString(bid) {
		http.Error(w, "invalid binding id", http.StatusBadRequest)
		return
	}

	// Extract proxy token from Authorization header.
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "missing or invalid authorization", http.StatusUnauthorized)
		return
	}
	proxyToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Validate proxy token.
	start := time.Now()
	info, err := s.ValidateProxyToken(r.Context(), proxyToken)
	if err != nil {
		s.logger.Error("proxy token validation failed", "error", err)
		http.Error(w, "authentication error", http.StatusInternalServerError)
		return
	}
	if info == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Fetch binding, scoped to workspace.
	binding, err := s.store.GetBinding(info.WorkspaceID, kind, bid)
	if err != nil {
		s.logger.Error("get binding failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if binding == nil {
		http.Error(w, "credential binding not found", http.StatusNotFound)
		return
	}

	// Decrypt auth blob.
	plaintext, err := crypto.Decrypt(s.config.EncryptionKey, binding.AuthBlob)
	if err != nil {
		s.logger.Error("credential decryption failed", "error", err, "binding_id", bid)
		http.Error(w, "credential decryption failed", http.StatusInternalServerError)
		return
	}

	// Look up provider.
	prov, err := provider.Lookup(kind)
	if err != nil {
		http.Error(w, "unknown credential kind", http.StatusNotFound)
		return
	}

	// Build decrypted binding for the provider.
	var publicMeta map[string]any
	if len(binding.PublicMeta) > 0 {
		json.Unmarshal(binding.PublicMeta, &publicMeta)
	}

	decrypted := &provider.DecryptedBinding{
		BindingMeta: provider.BindingMeta{
			ID:          binding.ID,
			WorkspaceID: binding.WorkspaceID,
			Kind:        binding.Kind,
			DisplayName: binding.DisplayName,
			ServerURL:   binding.ServerURL,
			PublicMeta:  publicMeta,
			AuthType:    binding.AuthType,
			IsDefault:   binding.IsDefault,
		},
		AuthSecret: plaintext,
	}

	// Check for upgrade header to log appropriately.
	isUpgrade := r.Header.Get("Upgrade") != ""
	if isUpgrade {
		LogUpgradeOpen(s.logger, info.WorkspaceID, info.SandboxID, kind, bid, r.URL.Path)
	}

	// Wrap ResponseWriter to capture status code for audit logging.
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

	// Dispatch to provider.
	prov.ServeHTTP(rec, r, decrypted)

	latencyMs := time.Since(start).Milliseconds()
	if !isUpgrade {
		LogProxied(s.logger, info.WorkspaceID, info.SandboxID, kind, bid, r.Method, r.URL.Path, rec.status, latencyMs)
	}
}

// statusRecorder wraps http.ResponseWriter to capture the status code.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap returns the underlying ResponseWriter for http.Flusher/http.Hijacker detection.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}
