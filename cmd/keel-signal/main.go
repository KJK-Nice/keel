package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"keel/internal/signal"
)

func main() {
	addr := os.Getenv("KEEL_SIGNAL_ADDR")
	if addr == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "47242"
		}
		addr = ":" + port
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           signal.New().Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("Keel signal on %s — joiners open /join?room=…", addr)
	log.Fatal(srv.ListenAndServe())
}
