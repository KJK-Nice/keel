package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"keel/internal/env"
	"keel/internal/signal"
)

func main() {
	addr := env.Get("SIGNAL_ADDR")
	if addr == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "47242"
		}
		addr = ":" + port
	}
	sig := signal.New()
	clientIP := env.Get("SIGNAL_CLIENT_IP")
	if err := sig.SetClientIP(clientIP); err != nil {
		log.Fatal(err)
	}
	if clientIP == "" {
		log.Printf("per-address room rate limit off; set SLIME_SIGNAL_CLIENT_IP (remote or xff:N) to enable it")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           sig.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("Slime signal on %s — joiners open /join?room=…", addr)
	log.Fatal(srv.ListenAndServe())
}
