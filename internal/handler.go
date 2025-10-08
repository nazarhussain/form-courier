package form_mailer

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/jordan-wright/email"
)

var sendEmailFunc = sendEmailSMTP

// Parse payload (JSON or form)
type ContactRequest struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Message string `json:"message"`
	Website string `json:"website,omitempty"` // honeypot
}

func HandleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func HandleContact(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context())
	cfg := GetConfig()

	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Signature")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		logger.Warn("method not allowed")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	siteKey := strings.TrimPrefix(r.URL.Path, "/v1/contact/")
	if siteKey == "" || strings.ContainsRune(siteKey, '/') {
		logger.Warn("bad site key")
		http.Error(w, "bad site key", http.StatusBadRequest)
		return
	}

	cs, ok := cfg.Sites[siteKey]
	if !ok {
		logger.Warn("unknown site", "site", siteKey)
		http.Error(w, "unknown site", http.StatusNotFound)
		return
	}
	logger = logger.With("site", cs.Key)

	// CORS for that site (exact match)
	if len(cs.AllowedOrigins) > 0 {
		origin := r.Header.Get("Origin")
		for _, ao := range cs.AllowedOrigins {
			if origin == ao {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				break
			}
		}
	}

	ip := clientIP(r)
	logger = logger.With("ip", ip)
	if !Allow(siteKey, ip, cfg.RateBurst, cfg.RateRefillMinutes) {
		logger.Warn("rate limited")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	// Read body once for HMAC (and to enforce max size), then re-wrap for decode
	maxBytes := cfg.MaxBodyKB * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxBytes)+1))
	r.Body.Close()
	if err != nil {
		logger.Warn("body read error", "err", err)
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > maxBytes {
		logger.Warn("payload too large", "size_bytes", len(body))
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Optional shared-secret HMAC
	if cs.Secret != "" {
		sig := r.Header.Get("X-Signature") // hex(HMAC-SHA256(body, secret))
		if !verifyHMAC(body, cs.Secret, sig) {
			logger.Warn("invalid signature")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Recreate Body for decoding
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	ct := r.Header.Get("Content-Type")
	var p = ContactRequest{}

	switch {
	case strings.HasPrefix(ct, "application/json") && cfg.AllowJSON:
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			logger.Warn("bad json payload", "err", err)
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	case cfg.AllowForm:
		if err := r.ParseForm(); err != nil {
			logger.Warn("bad form payload", "err", err)
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		p.Name = r.Form.Get("name")
		p.Email = r.Form.Get("email")
		p.Message = r.Form.Get("message")
		p.Website = r.Form.Get("website")
	default:
		logger.Warn("unsupported content type", "content_type", ct)
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	// Honeypot & validation
	if p.Website != "" || p.Name == "" || !emailRegex.MatchString(p.Email) || strings.TrimSpace(p.Message) == "" {
		logger.Warn("invalid submission", "from", p.Email)
		http.Error(w, "invalid submission", http.StatusBadRequest)
		return
	}

	// Compose email
	subject := strings.TrimSpace(cs.SubjectPrefix + " New contact")
	msg := fmt.Sprintf(
		"Site: %s\nFrom: %s <%s>\nIP: %s\n\n%s\n",
		cs.Key, p.Name, p.Email, ip, p.Message,
	)

	e := email.NewEmail()
	e.From = cs.FromAddr
	e.To = []string{cs.To}
	e.ReplyTo = []string{fmt.Sprintf("%s <%s>", p.Name, p.Email)}
	e.Subject = subject
	e.Text = []byte(msg)

	if err := sendEmailFunc(cs, e); err != nil {
		logger.Error("smtp send failed", "err", err)
		http.Error(w, "failed to send", http.StatusInternalServerError)
		return
	}

	logger.Info("contact email sent", "from", p.Email)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		parts := strings.Split(xf, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func verifyHMAC(body []byte, secret, hexSig string) bool {
	if secret == "" || hexSig == "" {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	sum := m.Sum(nil)
	want := strings.ToLower(hex.EncodeToString(sum))
	have := strings.ToLower(hexSig)
	// constant-time compare
	return hmac.Equal([]byte(want), []byte(have))
}

func sendEmailSMTP(cs *SiteCfg, e *email.Email) error {
	if cs.SMTP == nil {
		return fmt.Errorf("smtp config missing for site %s", cs.Key)
	}

	addr := net.JoinHostPort(cs.SMTP.Host, strconv.Itoa(cs.SMTP.Port))
	auth := smtp.PlainAuth("", cs.SMTP.User, cs.SMTP.Pass, cs.SMTP.Host)

	if cs.SMTP.SSL {
		tlsCfg := &tls.Config{ServerName: cs.SMTP.Host}
		return e.SendWithTLS(addr, auth, tlsCfg)
	}
	return e.Send(addr, auth)
}
