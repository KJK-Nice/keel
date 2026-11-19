package httpapi

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"slime/internal/store"
)

// A host saved or deleted under the id of one of the vault's own secrets
// is refused, and the secret is still there afterwards.
func TestHostIDsCannotNameInternalSecrets(t *testing.T) {
	f := newAgentFixture(t, testToken)
	at := time.Now().UTC()
	if err := f.st.PutAgentChat(store.AgentChat{ID: "conv-1", Title: "t", CreatedAt: at, UpdatedAt: at,
		Turns: []store.AgentChatTurn{{At: at, Question: "q", Answer: "a"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.AddAgentNote(store.AgentNote{Text: "a note"}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetAgentKey("gemini", "gemini-key"); err != nil {
		t.Fatal(err)
	}
	ident, err := f.st.UpsertIdentity(store.Identity{Name: "ops", Kind: "password", Username: "u", Password: "ident-secret"})
	if err != nil {
		t.Fatal(err)
	}
	host := map[string]any{"name": "x", "address": "10.0.0.1", "port": 22, "username": "u", "authType": "password"}
	for _, id := range []string{"agentchats:key", "agentmemory:key", "agentkey:gemini", "identity:" + ident.ID} {
		if code, body := f.do(t, http.MethodPut, "/api/hosts/"+id, host); code != http.StatusBadRequest {
			t.Fatalf("PUT %s = %d %s, want 400", id, code, body)
		}
		if code, body := f.do(t, http.MethodDelete, "/api/hosts/"+id, nil); code == http.StatusNoContent {
			t.Fatalf("DELETE %s = %d %s", id, code, body)
		}
	}
	reopened, err := store.New(filepath.Join(f.dir, "keel.json"), store.TestVault())
	if err != nil {
		t.Fatal(err)
	}
	if c, err := reopened.GetAgentChat("conv-1"); err != nil || c.Title != "t" {
		t.Fatalf("chat after the refusals: %+v %v", c, err)
	}
	if notes, err := reopened.ListAgentNotes(); err != nil || len(notes) != 1 {
		t.Fatalf("notes after the refusals: %+v %v", notes, err)
	}
	if k, err := reopened.AgentKey("gemini"); err != nil || k != "gemini-key" {
		t.Fatalf("agent key after the refusals: %v", err)
	}
	if in, err := reopened.GetIdentity(ident.ID); err != nil || in.Password != "ident-secret" {
		t.Fatalf("identity after the refusals: %v", err)
	}
}
