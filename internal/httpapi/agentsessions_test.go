package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keel/internal/store"
)

func (f *askFixture) call(t *testing.T, method, path string, body any) (int, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, r)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

// The session routes: an answered conversation is listed, opens as a
// transcript, renames, proposes again and deletes. keel.chats on disk is
// encrypted, and no secret from the pane or the vault is in it even after
// decrypting.
func TestAgentSessionRoutes(t *testing.T) {
	f := newAskFixture(t, true)
	web := f.open(t, f.webID, "login: "+askHostPass+"\n"+askPaneToken+"\n$ uptime\n")
	if code, resp := f.ask(t, map[string]any{"conversationId": askConv, "question": "is " + askHostPass + " ok?", "panes": []string{web.ID}}); code != http.StatusOK {
		t.Fatalf("ask → %d %v", code, resp)
	}

	code, b := f.call(t, "GET", "/api/agent/conversations", nil)
	var list struct {
		HistoryDays int                      `json:"historyDays"`
		Sessions    []store.AgentChatSummary `json:"sessions"`
	}
	if code != 200 || json.Unmarshal(b, &list) != nil || len(list.Sessions) != 1 || list.HistoryDays != 30 {
		t.Fatalf("list → %d %s", code, b)
	}
	if s := list.Sessions[0]; s.ID != askConv || s.Proposals != 1 || len(s.Hosts) != 1 || s.Hosts[0] != "web-1" {
		t.Fatalf("summary %+v", s)
	}

	code, b = f.call(t, "GET", "/api/agent/conversations/"+askConv, nil)
	if code != 200 || bytes.Contains(b, []byte(askHostPass)) {
		t.Fatalf("transcript → %d %s", code, b)
	}
	var tr struct {
		Turns []struct {
			Proposals []struct {
				ID      string `json:"id"`
				Expired bool   `json:"expired"`
			} `json:"proposals"`
		} `json:"turns"`
	}
	_ = json.Unmarshal(b, &tr)
	if len(tr.Turns) != 1 || len(tr.Turns[0].Proposals) != 1 || tr.Turns[0].Proposals[0].Expired {
		t.Fatalf("transcript in the same run keeps the live proposal: %s", b)
	}
	pid := tr.Turns[0].Proposals[0].ID

	if code, b := f.call(t, "PATCH", "/api/agent/conversations/"+askConv, map[string]string{"title": "Uptime on web"}); code != 200 || !bytes.Contains(b, []byte(`"Uptime on web"`)) {
		t.Fatalf("rename → %d %s", code, b)
	}
	if code, _ := f.call(t, "PATCH", "/api/agent/conversations/"+askConv, map[string]string{"title": " "}); code != 400 {
		t.Fatalf("empty rename → %d", code)
	}
	code, b = f.call(t, "POST", "/api/agent/conversations/"+askConv+"/proposals/"+pid+"/again", nil)
	if code != 200 || !bytes.Contains(b, []byte(`"sessionId":"`+web.ID+`"`)) || bytes.Contains(b, []byte(pid)) {
		t.Fatalf("propose again → %d %s", code, b)
	}
	if code, _ := f.call(t, "GET", "/api/agent/conversations/conv-missing-01", nil); code != 404 {
		t.Fatalf("missing transcript → %d", code)
	}

	chats, err := os.ReadFile(filepath.Join(filepath.Dir(f.logPath), "keel.chats"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(chats, []byte("uptime")) || bytes.Contains(chats, []byte("web-1")) {
		t.Fatal("keel.chats is not encrypted")
	}
	saved, err := f.st.GetAgentChat(askConv)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(saved)
	for _, leak := range []string{askHostPass, askPaneToken, "Zx81Kq0LmN4pR7tV2wY5bC9dF3gH6jK", askKey} {
		if bytes.Contains(plain, []byte(leak)) {
			t.Fatalf("saved conversation holds a secret (%.8s…)", leak)
		}
	}
	if saved.Title != "Uptime on web" || len(saved.Turns) != 2 || !saved.Turns[1].Again {
		t.Fatalf("saved %+v", saved)
	}

	if code, _ := f.call(t, "DELETE", "/api/agent/conversations/"+askConv, nil); code != 204 {
		t.Fatalf("delete → %d", code)
	}
	code, b = f.call(t, "GET", "/api/agent/conversations", nil)
	if code != 200 || !strings.Contains(string(b), `"sessions":[]`) {
		t.Fatalf("list after delete → %d %s", code, b)
	}
}
