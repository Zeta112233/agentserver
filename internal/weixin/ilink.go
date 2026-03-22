package weixin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	DefaultAPIBaseURL = "https://ilinkai.weixin.qq.com"
	defaultBotType    = "3"
	startTimeout      = 10 * time.Second
	pollTimeout       = 40 * time.Second // slightly longer than ilink's 35s long-poll
	sessionTTL        = 10 * time.Minute
)

// Session holds the state of an in-progress QR login for a single sandbox.
type Session struct {
	QRCode    string    // opaque qrcode string for status polling
	QRCodeURL string    // image URL for frontend rendering
	StartedAt time.Time
}

// StatusResult is the parsed response from get_qrcode_status.
type StatusResult struct {
	Status  string `json:"status"` // "wait", "scaned", "confirmed", "expired"
	Token   string `json:"bot_token,omitempty"`
	BotID   string `json:"ilink_bot_id,omitempty"`
	BaseURL string `json:"baseurl,omitempty"`
	UserID  string `json:"ilink_user_id,omitempty"`
}

var (
	mu       sync.Mutex
	sessions = map[string]*Session{}
)

// purgeExpired removes sessions older than sessionTTL. Must be called with mu held.
func purgeExpired() {
	now := time.Now()
	for id, s := range sessions {
		if now.Sub(s.StartedAt) > sessionTTL {
			delete(sessions, id)
		}
	}
}

func GetSession(sandboxID string) *Session {
	mu.Lock()
	defer mu.Unlock()
	purgeExpired()
	s := sessions[sandboxID]
	if s != nil && time.Since(s.StartedAt) > sessionTTL {
		delete(sessions, sandboxID)
		return nil
	}
	return s
}

func SetSession(sandboxID string, s *Session) {
	mu.Lock()
	defer mu.Unlock()
	purgeExpired()
	sessions[sandboxID] = s
}

// TakeSession atomically returns and removes the session (used on confirmed).
func TakeSession(sandboxID string) *Session {
	mu.Lock()
	defer mu.Unlock()
	s := sessions[sandboxID]
	delete(sessions, sandboxID)
	return s
}

func ClearSession(sandboxID string) {
	mu.Lock()
	defer mu.Unlock()
	delete(sessions, sandboxID)
}

type qrCodeResponse struct {
	QRCode           string `json:"qrcode"`
	QRCodeImgContent string `json:"qrcode_img_content"`
}

// StartLogin calls the ilink API to generate a new QR code for WeChat login.
func StartLogin(ctx context.Context, apiBaseURL string) (*Session, error) {
	if apiBaseURL == "" {
		apiBaseURL = DefaultAPIBaseURL
	}
	u, err := url.Parse(apiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid apiBaseURL: %w", err)
	}
	u.Path = "/ilink/bot/get_bot_qrcode"
	q := u.Query()
	q.Set("bot_type", defaultBotType)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ilink get_bot_qrcode: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ilink get_bot_qrcode: status %d", resp.StatusCode)
	}

	var qr qrCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&qr); err != nil {
		return nil, fmt.Errorf("ilink get_bot_qrcode: decode: %w", err)
	}
	if qr.QRCode == "" || qr.QRCodeImgContent == "" {
		return nil, fmt.Errorf("ilink get_bot_qrcode: empty response")
	}

	return &Session{
		QRCode:    qr.QRCode,
		QRCodeURL: qr.QRCodeImgContent,
		StartedAt: time.Now(),
	}, nil
}

// PollLoginStatus long-polls the ilink API for QR code scan status.
// Blocks for up to ~35 seconds (server-side long-poll).
func PollLoginStatus(ctx context.Context, apiBaseURL, qrcode string) (*StatusResult, error) {
	if apiBaseURL == "" {
		apiBaseURL = DefaultAPIBaseURL
	}
	u, err := url.Parse(apiBaseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid apiBaseURL: %w", err)
	}
	u.Path = "/ilink/bot/get_qrcode_status"
	q := u.Query()
	q.Set("qrcode", qrcode)
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("iLink-App-ClientVersion", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ilink get_qrcode_status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ilink get_qrcode_status: status %d", resp.StatusCode)
	}

	var result StatusResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ilink get_qrcode_status: decode: %w", err)
	}
	return &result, nil
}
