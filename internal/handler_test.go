package form_mailer

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jordan-wright/email"
)

func setupTestConfig(t *testing.T) {
	t.Helper()

	prevConfig := conf
	prevSend := sendEmailFunc

	conf = &Config{
		RateBurst:         1,
		RateRefillMinutes: 10,
		AllowJSON:         true,
		AllowForm:         true,
		MaxBodyKB:         1024,
		ListenAddr:        ":0",
		Sites: map[string]*SiteCfg{
			"acme": {
				Key:           "acme",
				To:            "ops@example.com",
				SubjectPrefix: "[Contact]",
				FromAddr:      "noreply@example.com",
				SMTP: &SmtpCfg{
					Host: "smtp.example.com",
					Port: 587,
					User: "user",
					Pass: "pass",
					SSL:  false,
				},
			},
		},
	}

	bucketsMu.Lock()
	buckets = map[string]*Bucket{}
	bucketsMu.Unlock()

	t.Cleanup(func() {
		conf = prevConfig
		sendEmailFunc = prevSend
		bucketsMu.Lock()
		buckets = map[string]*Bucket{}
		bucketsMu.Unlock()
	})
}

func TestHandleContactJSONSuccess(t *testing.T) {
	setupTestConfig(t)

	var (
		capturedEmail *email.Email
		mu            sync.Mutex
	)

	sendEmailFunc = func(site *SiteCfg, e *email.Email) error {
		mu.Lock()
		defer mu.Unlock()
		capturedEmail = e
		if site.Key != "acme" {
			t.Fatalf("unexpected site key: %s", site.Key)
		}
		return nil
	}

	body := `{"name":"Alice","email":"alice@example.com","message":"Hello there"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/contact/acme", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	HandleContact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	if capturedEmail == nil {
		t.Fatal("expected email to be captured")
	}

	if got, want := capturedEmail.Subject, "[Contact] New contact"; got != want {
		t.Fatalf("unexpected subject: got %q want %q", got, want)
	}
	if got, want := capturedEmail.To[0], "ops@example.com"; got != want {
		t.Fatalf("unexpected recipient: got %q want %q", got, want)
	}
	if got := string(capturedEmail.Text); !strings.Contains(got, "Hello there") {
		t.Fatalf("email body missing message: %q", got)
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("expected ok=true in response, got %v", resp)
	}
}

func TestHandleContactRateLimited(t *testing.T) {
	setupTestConfig(t)

	var calls int
	sendEmailFunc = func(site *SiteCfg, e *email.Email) error {
		calls++
		return nil
	}

	body := []byte(`{"name":"Bob","email":"bob@example.com","message":"Hello"}`)

	makeRequest := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/contact/acme", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		HandleContact(rec, req)
		return rec
	}

	if rec := makeRequest(); rec.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", rec.Code)
	}
	if rec := makeRequest(); rec.Code != http.StatusOK {
		t.Fatalf("second request expected 200, got %d", rec.Code)
	}
	if rec := makeRequest(); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request expected 429, got %d", rec.Code)
	}
	if calls != 2 {
		t.Fatalf("expected sendEmailFunc to be called twice, got %d", calls)
	}
}

func TestHandleContactCORSAllowed(t *testing.T) {
	setupTestConfig(t)
	conf.Sites["acme"].AllowedOrigins = []string{"https://example.com"}

	sendEmailFunc = func(site *SiteCfg, e *email.Email) error {
		return nil
	}

	body := `{"name":"Alice","email":"alice@example.com","message":"Hello there"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/contact/acme", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	HandleContact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Fatalf("missing Access-Control-Allow-Origin, got %q", got)
	}
}

func TestHandleContactCORSForbidden(t *testing.T) {
	setupTestConfig(t)
	conf.Sites["acme"].AllowedOrigins = []string{"https://allowed.example.com"}

	var calls int
	sendEmailFunc = func(site *SiteCfg, e *email.Email) error {
		calls++
		return nil
	}

	body := `{"name":"Alice","email":"alice@example.com","message":"Hello there"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/contact/acme", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://blocked.example.com")
	rec := httptest.NewRecorder()

	HandleContact(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", rec.Code)
	}
	if calls != 0 {
		t.Fatalf("expected sendEmailFunc not to be called, got %d", calls)
	}
}

func TestHandleContactCORSWildcard(t *testing.T) {
	setupTestConfig(t)
	conf.Sites["acme"].AllowedOrigins = []string{"*"}

	sendEmailFunc = func(site *SiteCfg, e *email.Email) error {
		return nil
	}

	body := `{"name":"Alice","email":"alice@example.com","message":"Hello there"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/contact/acme", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://any.origin.com")
	rec := httptest.NewRecorder()

	HandleContact(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard Access-Control-Allow-Origin, got %q", got)
	}
}

func TestHandleHealth(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)

	HandleHealth(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); !strings.Contains(got, "ok") {
		t.Fatalf("unexpected body: %q", got)
	}
}
