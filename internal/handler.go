package form_mailer

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"

	"github.com/jordan-wright/email"
)

var (
	config = GetConfig()
)

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
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Signature")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	siteKey := strings.TrimPrefix(r.URL.Path, "/v1/contact/")
	if siteKey == "" || strings.ContainsRune(siteKey, '/') {
		http.Error(w, "bad site key", http.StatusBadRequest)
		return
	}

	cs, ok := config.Sites[siteKey]
	if !ok {
		http.Error(w, "unknown site", http.StatusNotFound)
		return
	}

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
	if !Allow(siteKey, ip, config.RateBurst, config.RateRefillMinutes) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
		return
	}

	// Read body once for HMAC (and to enforce max size), then re-wrap for decode
	maxBytes := config.MaxBodyKB * 1024
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(maxBytes)+1))
	r.Body.Close()
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if len(body) > maxBytes {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Optional shared-secret HMAC
	if cs.Secret != "" {
		sig := r.Header.Get("X-Signature") // hex(HMAC-SHA256(body, secret))
		if !verifyHMAC(body, cs.Secret, sig) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Recreate Body for decoding
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	ct := r.Header.Get("Content-Type")
	var p = ContactRequest{}

	switch {
	case strings.HasPrefix(ct, "application/json") && config.AllowJSON:
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
	case config.AllowForm:
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		p.Name = r.Form.Get("name")
		p.Email = r.Form.Get("email")
		p.Message = r.Form.Get("message")
		p.Website = r.Form.Get("website")
	default:
		http.Error(w, "unsupported content type", http.StatusUnsupportedMediaType)
		return
	}

	// Honeypot & validation
	if p.Website != "" || p.Name == "" || !emailRegex.MatchString(p.Email) || strings.TrimSpace(p.Message) == "" {
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

	addr := net.JoinHostPort(cs.SMTP.Host, strconv.Itoa(cs.SMTP.Port))
	auth := smtp.PlainAuth("", cs.SMTP.User, cs.SMTP.Pass, cs.SMTP.Host)

	if cs.SMTP.SSL {
		err = e.SendWithTLS(addr, auth, nil)
	} else {
		err = e.Send(addr, auth)
	}
	if err != nil {
		log.Println("send error:", err)
		http.Error(w, "failed to send", http.StatusInternalServerError)
		return
	}

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
