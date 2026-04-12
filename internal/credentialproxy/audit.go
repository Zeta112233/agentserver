package credentialproxy

import (
	"log/slog"
	"os"
)

// NewLogger creates a structured logger for the credential proxy.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
	})).With("component", "credentialproxy")
}

// LogProxied logs a completed proxy request.
func LogProxied(logger *slog.Logger, workspaceID, sandboxID, kind, bindingID, method, path string, status int, latencyMs int64) {
	logger.Info("proxied",
		"workspace_id", workspaceID,
		"sandbox_id", sandboxID,
		"kind", kind,
		"binding_id", bindingID,
		"method", method,
		"path", path,
		"status", status,
		"latency_ms", latencyMs,
	)
}

// LogUpgradeOpen logs the start of an upgrade (SPDY/WebSocket) connection.
func LogUpgradeOpen(logger *slog.Logger, workspaceID, sandboxID, kind, bindingID, path string) {
	logger.Info("upgrade_open",
		"workspace_id", workspaceID,
		"sandbox_id", sandboxID,
		"kind", kind,
		"binding_id", bindingID,
		"path", path,
	)
}

// LogUpgradeClosed logs the end of an upgrade connection.
func LogUpgradeClosed(logger *slog.Logger, workspaceID, sandboxID, kind, bindingID, path string, durationMs, bytesIn, bytesOut int64) {
	logger.Info("upgrade_closed",
		"workspace_id", workspaceID,
		"sandbox_id", sandboxID,
		"kind", kind,
		"binding_id", bindingID,
		"path", path,
		"duration_ms", durationMs,
		"bytes_in", bytesIn,
		"bytes_out", bytesOut,
	)
}
