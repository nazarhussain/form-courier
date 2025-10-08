package main

import (
	"log"
	"net/http"
	"time"

	form_courier "github.com/nazarhussain/form-courier/internal"
)

func main() {
	config := form_courier.GetConfig()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", form_courier.HandleHealth)

	// POST /v1/contact/{siteKey}
	mux.HandleFunc("/v1/contact/", form_courier.HandleContact)

	s := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           secHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("form-mailer listening on %s with %d site(s)\n", config.ListenAddr, len(config.Sites))
	log.Fatal(s.ListenAndServe())
}

func secHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer-when-downgrade")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "0")
		next.ServeHTTP(w, r)
	})
}
