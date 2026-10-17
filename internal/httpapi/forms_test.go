package httpapi

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func postForm(t *testing.T, srvURL, kind string, v url.Values) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srvURL+"/ui/forms/"+kind, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Datastar-Request", "true")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b)
}

func TestUIFormSnippetSavesAndSignalsTheDialog(t *testing.T) {
	srv, _, st, _ := testServer(t)
	code, body := postForm(t, srv.URL, "snippet", url.Values{"name": {"disk"}, "command": {"df -h"}})
	if code != http.StatusOK {
		t.Fatalf("status %d: %s", code, body)
	}
	if !strings.Contains(body, `id="snippet-form-error"`) || !strings.Contains(body, "hidden") {
		t.Fatalf("success did not clear the form error: %q", body)
	}
	if !strings.Contains(body, `keel-form-saved`) || !strings.Contains(body, `form: "snippet"`) {
		t.Fatalf("success did not dispatch keel-form-saved: %q", body)
	}
	all, err := st.ListSnippets()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sn := range all {
		if sn.Name == "disk" {
			found = sn.Command == "df -h\n"
		}
	}
	if !found {
		t.Fatalf("snippet not saved with a trailing newline: %+v", all)
	}
}

func TestUIFormErrorPatchesTheFormAndSavesNothing(t *testing.T) {
	srv, _, st, _ := testServer(t)
	before, _ := st.ListSnippets()
	code, body := postForm(t, srv.URL, "snippet", url.Values{"name": {"  "}, "command": {"x"}})
	if code != http.StatusOK {
		t.Fatalf("status %d", code)
	}
	if !strings.Contains(body, `id="snippet-form-error"`) || !strings.Contains(body, "name and command are required") {
		t.Fatalf("error not patched into the form: %q", body)
	}
	if strings.Contains(body, "keel-form-saved") {
		t.Fatalf("failed save closed the dialog: %q", body)
	}
	if all, _ := st.ListSnippets(); len(all) != len(before) {
		t.Fatalf("failed save stored a snippet: %+v", all)
	}

	_, body = postForm(t, srv.URL, "tunnel", url.Values{"name": {"pg"}, "kind": {"local"}, "bindPort": {"15432"}})
	if !strings.Contains(body, "Pick an SSH host first") {
		t.Fatalf("tunnel without a host: %q", body)
	}
}

// The error text is user input echoed back; it must arrive escaped.
func TestUIFormEscapesErrorText(t *testing.T) {
	srv, _, _, _ := testServer(t)
	_, body := postForm(t, srv.URL, "identity", url.Values{"name": {"<b>x</b>"}, "kind": {"nope"}})
	if strings.Contains(body, "<b>") {
		t.Fatalf("unescaped markup in the error patch: %q", body)
	}
}

func TestUIFormIdentityKeepsSavedSecret(t *testing.T) {
	srv, _, st, _ := testServer(t)
	_, body := postForm(t, srv.URL, "identity", url.Values{
		"name": {"deploy"}, "username": {"ubuntu"}, "kind": {"password"}, "password": {"s3cret"},
	})
	if !strings.Contains(body, "keel-form-saved") {
		t.Fatalf("create failed: %q", body)
	}
	ids, _ := st.ListIdentities()
	if len(ids) != 1 {
		t.Fatalf("identities %+v", ids)
	}
	_, body = postForm(t, srv.URL, "identity", url.Values{
		"id": {ids[0].ID}, "name": {"deploy2"}, "username": {"ubuntu"}, "kind": {"password"}, "password": {"__saved__"},
	})
	if !strings.Contains(body, "keel-form-saved") {
		t.Fatalf("update failed: %q", body)
	}
	got, _ := st.GetIdentity(ids[0].ID)
	if got.Name != "deploy2" || got.Password != "s3cret" {
		t.Fatalf("update lost the saved password or name: %+v", got)
	}
}

func TestUIFormUnknownKind(t *testing.T) {
	srv, _, _, _ := testServer(t)
	if code, _ := postForm(t, srv.URL, "nope", url.Values{}); code != http.StatusNotFound {
		t.Fatalf("unknown form status %d", code)
	}
}

func TestUIFormHost(t *testing.T) {
	srv, _, st, _ := testServer(t)
	_, body := postForm(t, srv.URL, "host", url.Values{
		"name": {"web"}, "authType": {"password"}, "address": {"10.0.0.5"}, "port": {""},
		"username": {"ubuntu"}, "password": {"pw"}, "tags": {" prod, gpu ,,"},
		"keepAlive": {"on"}, "keepAliveSec": {"45"}, "agentReadable": {"false"}, "production": {"true"},
	})
	if !strings.Contains(body, "keel-form-saved") || !strings.Contains(body, `form: "host"`) {
		t.Fatalf("host create failed: %q", body)
	}
	var id string
	hosts, _ := st.ListHosts()
	for _, h := range hosts {
		if h.Name != "web" {
			continue
		}
		id = h.ID
		if h.Port != 22 || h.KeepAliveSec != 45 || !h.NoAutoReconnect || strings.Join(h.Tags, "|") != "prod|gpu" {
			t.Fatalf("host fields: %+v", h)
		}
		if h.AgentReadable == nil || *h.AgentReadable || h.Production == nil || !*h.Production {
			t.Fatalf("host switches: %+v", h)
		}
	}
	if id == "" {
		t.Fatalf("host not saved: %+v", hosts)
	}

	// Edit without touching the password: the saved one stays.
	_, body = postForm(t, srv.URL, "host", url.Values{
		"id": {id}, "name": {"web2"}, "authType": {"password"}, "address": {"10.0.0.5"}, "port": {"2222"},
		"username": {"ubuntu"}, "password": {"__saved__"}, "autoReconnect": {"on"},
	})
	if !strings.Contains(body, "keel-form-saved") {
		t.Fatalf("host update failed: %q", body)
	}
	h, err := st.GetHost(id)
	if err != nil {
		t.Fatal(err)
	}
	if h.Name != "web2" || h.Port != 2222 || h.Password != "pw" || h.KeepAliveSec != -1 || h.NoAutoReconnect {
		t.Fatalf("host update: %+v", h)
	}

	_, body = postForm(t, srv.URL, "host", url.Values{"name": {"x"}, "authType": {"password"}})
	if !strings.Contains(body, `id="host-form-error"`) || !strings.Contains(body, "address is required") {
		t.Fatalf("host error not patched: %q", body)
	}
}

func TestUIFormSnippetEditorSetsAgentFlag(t *testing.T) {
	srv, _, st, _ := testServer(t)
	all, _ := st.ListSnippets()
	if len(all) == 0 {
		t.Fatal("no seeded snippet")
	}
	sn := all[0]
	_, body := postForm(t, srv.URL, "snippet-editor", url.Values{
		"id": {sn.ID}, "name": {"renamed"}, "command": {"uptime"}, "agentUsable": {"true"},
	})
	if !strings.Contains(body, `form: "snippet-editor"`) {
		t.Fatalf("editor save failed: %q", body)
	}
	all, _ = st.ListSnippets()
	for _, x := range all {
		if x.ID == sn.ID && (x.Name != "renamed" || x.Command != "uptime\n" || !x.AgentUsable) {
			t.Fatalf("editor save: %+v", x)
		}
	}
	_, body = postForm(t, srv.URL, "snippet-editor", url.Values{"id": {sn.ID}, "name": {" "}, "command": {"x"}})
	if !strings.Contains(body, `id="snippet-editor-form-error"`) || !strings.Contains(body, "Give the snippet a name") {
		t.Fatalf("blank name: %q", body)
	}
}
