package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestRefreshAccessToken_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
		}
		if r.PostForm.Get("refresh_token") != "old-refresh" {
			t.Errorf("refresh_token = %q", r.PostForm.Get("refresh_token"))
		}
		json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresIn:    3600,
		})
	}))
	defer srv.Close()

	resp, err := refreshAccessToken(srv.URL, "old-refresh")
	if err != nil {
		t.Fatalf("refreshAccessToken: %v", err)
	}
	if resp.AccessToken != "new-access" {
		t.Errorf("AccessToken = %q", resp.AccessToken)
	}
	if resp.RefreshToken != "new-refresh" {
		t.Errorf("RefreshToken = %q", resp.RefreshToken)
	}
}

func TestRefreshAccessToken_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
	}))
	defer srv.Close()

	_, err := refreshAccessToken(srv.URL, "bad-refresh")
	if err == nil {
		t.Fatal("expected error for invalid refresh token")
	}
}

func TestEnsureValidToken_ValidToken(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")

	SaveCredentials(credPath, &Credentials{
		AccessToken:  "valid-token",
		RefreshToken: "ref",
		ExpiresAt:    time.Now().Add(time.Hour),
	})

	token, err := ensureValidToken("https://server.example.com", credPath)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if token != "valid-token" {
		t.Errorf("token = %q, want %q", token, "valid-token")
	}
}

func TestEnsureValidToken_NoCredentials(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")

	_, err := ensureValidToken("https://server.example.com", credPath)
	if err != ErrNeedReLogin {
		t.Fatalf("expected ErrNeedReLogin, got: %v", err)
	}
}

func TestEnsureValidToken_ExpiredNoRefresh(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, ".credentials.json")

	SaveCredentials(credPath, &Credentials{
		AccessToken: "expired",
		ExpiresAt:   time.Now().Add(-time.Hour),
	})

	_, err := ensureValidToken("https://server.example.com", credPath)
	if err != ErrNeedReLogin {
		t.Fatalf("expected ErrNeedReLogin, got: %v", err)
	}
}
