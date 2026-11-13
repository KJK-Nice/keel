package httpapi

import (
	"encoding/json"
	"net/http"
)

// The largest request body each kind of route reads. A longer body fails
// to decode and the route answers 400.
const (
	// bodySwitch: a flag, an id, a role, a mode.
	bodySwitch = 1 << 10
	// bodyLine: a name, one variable, a host key to trust.
	bodyLine = 4 << 10
	// bodyNote: a note for the agent, the signal server's URL.
	bodyNote = 8 << 10
	// bodyAgent: agent settings, a provider key, keel mcp's requests.
	bodyAgent = 16 << 10
	// bodyAsk: a question for the agent and what goes with it.
	bodyAsk = 32 << 10
	// bodyForm: a group, a snippet, a forward, a move, a passphrase, the
	// values a group run is filled with.
	bodyForm = 64 << 10
	// bodyHost: a host or an identity with its private key, a runbook.
	bodyHost = 256 << 10
	// bodyBulk: a notebook, an ssh config import.
	bodyBulk = 4 << 20
)

// decode reads the request's JSON body into v: at most limit bytes, the
// first JSON value, unknown fields ignored. Every route reads its body
// here.
func decode(w http.ResponseWriter, r *http.Request, limit int64, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, limit)).Decode(v)
}

// savedStatus is a save's status: 201 for a create (POST, no id in the
// path), 200 for an update (PUT with the id).
func savedStatus(pathID string) int {
	if pathID == "" {
		return http.StatusCreated
	}
	return http.StatusOK
}
