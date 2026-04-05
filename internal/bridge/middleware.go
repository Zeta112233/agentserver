package bridge

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

type contextKey string

const (
	ctxSessionID   contextKey = "bridge_session_id"
	ctxSandboxID   contextKey = "bridge_sandbox_id"
	ctxWorkspaceID contextKey = "bridge_workspace_id"
	ctxEpoch       contextKey = "bridge_epoch"
)

func SessionIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxSessionID).(string)
	return v
}

func SandboxIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxSandboxID).(string)
	return v
}

func WorkspaceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxWorkspaceID).(string)
	return v
}

func EpochFromContext(ctx context.Context) int {
	v, _ := ctx.Value(ctxEpoch).(int)
	return v
}

// WorkerAuthMiddleware validates worker JWT for /worker/* endpoints.
func (h *Handler) WorkerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, "missing authorization", http.StatusUnauthorized)
			return
		}

		claims, err := ValidateWorkerJWT(h.JWTSecret, token)
		if err != nil {
			status := http.StatusUnauthorized
			if err == ErrJWTExpired {
				status = http.StatusUnauthorized
			}
			http.Error(w, "invalid token: "+err.Error(), status)
			return
		}

		sessionID := chi.URLParam(r, "sessionId")
		if sessionID != "" && sessionID != claims.SessionID {
			http.Error(w, "token session mismatch", http.StatusForbidden)
			return
		}

		ctx := r.Context()
		ctx = context.WithValue(ctx, ctxSessionID, claims.SessionID)
		ctx = context.WithValue(ctx, ctxSandboxID, claims.SandboxID)
		ctx = context.WithValue(ctx, ctxWorkspaceID, claims.WorkspaceID)
		ctx = context.WithValue(ctx, ctxEpoch, claims.Epoch)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AgentOrUserAuthMiddleware authenticates via proxy_token (agent) or delegates
// to the provided user auth middleware (web UI cookie).
func (h *Handler) AgentOrUserAuthMiddleware(userAuth func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token != "" {
				// Try proxy_token auth: look up sandbox by token.
				sbx, err := h.DB.GetSandboxByAnyToken(token)
				if err == nil && sbx != nil {
					ctx := r.Context()
					ctx = context.WithValue(ctx, ctxSandboxID, sbx.ID)
					ctx = context.WithValue(ctx, ctxWorkspaceID, sbx.WorkspaceID)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
			}

			// Fall back to user auth (cookie).
			userAuth(next).ServeHTTP(w, r)
		})
	}
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return auth[7:]
	}
	return ""
}

