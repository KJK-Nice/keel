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
		addr = ":47242"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           signal.New().Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("Keel signal on %s — joiners open /join?room=…", addr)
	log.Fatal(srv.ListenAndServe())
}
