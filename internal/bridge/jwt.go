package bridge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// WorkerJWTClaims are the claims embedded in a worker JWT.
type WorkerJWTClaims struct {
	SessionID   string `json:"sid"`
	SandboxID   string `json:"sbx"`
	WorkspaceID string `json:"wid"`
	Epoch       int    `json:"epoch"`
	Exp         int64  `json:"exp"`
}

var (
	ErrJWTExpired   = errors.New("jwt expired")
	ErrJWTMalformed = errors.New("jwt malformed")
	ErrJWTSignature = errors.New("jwt signature invalid")
)

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// IssueWorkerJWT creates an HMAC-SHA256 JWT with the given claims.
func IssueWorkerJWT(secret []byte, sessionID, sandboxID, workspaceID string, epoch int, ttl time.Duration) (string, error) {
	claims := WorkerJWTClaims{
		SessionID:   sessionID,
		SandboxID:   sandboxID,
		WorkspaceID: workspaceID,
		Epoch:       epoch,
		Exp:         time.Now().Add(ttl).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := jwtHeader + "." + payloadB64

	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return signingInput + "." + sig, nil
}

// ValidateWorkerJWT validates an HMAC-SHA256 JWT and returns the claims.
func ValidateWorkerJWT(secret []byte, token string) (*WorkerJWTClaims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, ErrJWTMalformed
	}

	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signingInput))
	expectedSig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if !hmac.Equal([]byte(parts[2]), []byte(expectedSig)) {
		return nil, ErrJWTSignature
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrJWTMalformed
	}

	var claims WorkerJWTClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, ErrJWTMalformed
	}

	if time.Now().Unix() > claims.Exp {
		return nil, ErrJWTExpired
	}

	return &claims, nil
}
