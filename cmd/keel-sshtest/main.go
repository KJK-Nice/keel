// Command keel-sshtest runs two SSH servers on 127.0.0.1 for e2e tests (a
// jump host and a target), using internal/session/sshtest. It writes their
// addresses, users, passwords and key fingerprints as JSON to the file
// named by -out, then serves until it is stopped. Test use only: it has
// throwaway passwords and an echo shell, and listens on loopback only.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"keel/internal/session/sshtest"
)

type server struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	Password    string `json:"password"`
	Fingerprint string `json:"fingerprint"`
}

func describe(s *sshtest.Server) server {
	return server{Host: s.Host, Port: s.Port, User: s.User, Password: s.Password, Fingerprint: s.Fingerprint()}
}

func main() {
	out := flag.String("out", "", "file to write the servers' JSON to")
	flag.Parse()
	if *out == "" {
		log.Fatal("keel-sshtest: -out is required")
	}
	jump, err := sshtest.Start(sshtest.Options{User: "jumper", Password: "e2e-jump-pass"})
	if err != nil {
		log.Fatal(err)
	}
	defer jump.Close()
	target, err := sshtest.Start(sshtest.Options{User: "deploy", Password: "e2e-target-pass", NoForward: true})
	if err != nil {
		log.Fatal(err)
	}
	defer target.Close()
	b, err := json.MarshalIndent(map[string]server{"jump": describe(jump), "target": describe(target)}, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, b, 0o600); err != nil {
		log.Fatal(err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
