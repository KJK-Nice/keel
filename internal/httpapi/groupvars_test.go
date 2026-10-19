package httpapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"keel/internal/agent/service"
	"keel/internal/store"
)

// Groups, part 2 end to end (docs/ui-redesign.md §17): a group variable
// (a secret-looking value refused), a step that captures a value from its
// output between the OSC 133 marks, the next step's proposal keeping
// {{name}}, the approve dialog's fill with the values, the session socket
// typing only the filled bytes, and keel.log with no value; then the
// Ansible export with no password in it.
func TestGroupVarsCaptureFillTypeAndExport(t *testing.T) {
	f := newAskFixture(t, true)
	g, err := f.st.UpsertGroup(store.Group{Name: "k8s-prod"})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.st.AssignHostsGroup([]string{f.webID, f.prodID}, g.ID); err != nil {
		t.Fatal(err)
	}
	if code, b := f.call(t, "PUT", "/api/groups/"+g.ID+"/roles", map[string]string{"hostId": f.webID, "role": "control-plane"}); code != 200 {
		t.Fatalf("role → %d %s", code, b)
	}
	if code, b := f.call(t, "PUT", "/api/groups/"+g.ID+"/vars", map[string]string{"name": "k8s_version", "value": "1.31.2"}); code != 200 || !strings.Contains(string(b), `"k8s_version"`) {
		t.Fatalf("var → %d %s", code, b)
	}
	if code, _ := f.call(t, "PUT", "/api/groups/"+g.ID+"/vars", map[string]string{"name": "pw", "value": askHostPass}); code != 400 {
		t.Fatalf("vault value as a variable → %d", code)
	}
	if code, _ := f.call(t, "PUT", "/api/groups/"+g.ID+"/vars", map[string]string{"name": "9x", "value": "a"}); code != 400 {
		t.Fatalf("bad name → %d", code)
	}

	code, b := f.call(t, "POST", "/api/agent/runbooks", service.RunbookInput{Name: "bootstrap", Steps: []store.RunbookStep{
		{Title: "token", Command: "kubeadm token create --print-join-command", Role: "control-plane", Captures: []store.StepCapture{{Name: "join_token", Pattern: `--token\s+(\S+)`}}},
		{Title: "join", Command: "echo join --token {{join_token}} --v {{k8s_version}}", Role: "control-plane"},
	}})
	var saved service.SavedRunbook
	if code != 200 || json.Unmarshal(b, &saved) != nil {
		t.Fatalf("runbook → %d %s", code, b)
	}
	code, b = f.call(t, "POST", "/api/agent/groupruns", map[string]string{"runbookId": saved.Runbook.ID, "groupId": g.ID, "mode": "rolling"})
	var run service.GroupRun
	if code != 200 || json.Unmarshal(b, &run) != nil {
		t.Fatalf("start → %d %s", code, b)
	}
	web := f.open(t, f.webID, "$ ")
	propose := func() *service.Proposal {
		t.Helper()
		_, b := f.call(t, "POST", "/api/agent/groupruns/"+run.ID+"/propose", map[string]any{"panes": []string{web.ID}})
		run = service.GroupRun{}
		if err := json.Unmarshal(b, &run); err != nil {
			t.Fatal(err)
		}
		return run.Steps[run.Current].Cells[0].Proposal
	}
	driver := dialSession(t, f.srv.URL, web.ID, "me", "web")
	defer driver.Close()
	waitJSON(t, driver, "ready")
	bridge := f.bridge(t, f.webID)

	p1 := propose()
	got := readPTY(bridge)
	if r := typeGroupOverSocket(t, driver, p1.Bytes, run.ID, p1.ID, false); r["ok"] != true {
		t.Fatalf("type step 1 → %v", r)
	}
	<-got
	const token = "abcdef.0123456789abcdef"
	go func() {
		_, _ = bridge.outW.Write([]byte("kubeadm token create\r\n\x1b]133;C\x07kubeadm join 10.20.0.10:6443 --token \x1b[1m" + token + "\x1b[0m --discovery-token-ca-cert-hash sha256:4f\r\n\x1b]133;D;0\x07$ "))
	}()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && run.Current < 1 {
		_, b := f.call(t, "GET", "/api/agent/groupruns/"+run.ID, nil)
		run = service.GroupRun{}
		_ = json.Unmarshal(b, &run)
		time.Sleep(20 * time.Millisecond)
	}
	if run.Current != 1 {
		t.Fatalf("step 1 not done: %+v", run.Steps[0].Cells)
	}
	if c := run.Steps[0].Cells[0]; len(c.Captures) != 1 || c.Captures[0].State != "captured" || c.Captures[0].Bytes != len(token) {
		t.Fatalf("captures %+v", c.Captures)
	}
	p2 := propose()
	if !strings.Contains(p2.Bytes, "{{join_token}}") || strings.Contains(p2.Bytes, token) {
		t.Fatalf("stored proposal %q", p2.Bytes)
	}
	// The template is not what Type may send.
	if r := typeGroupOverSocket(t, driver, p2.Bytes, run.ID, p2.ID, false); r["ok"] != false {
		t.Fatalf("template typed → %v", r)
	}
	code, b = f.call(t, "POST", "/api/agent/groupruns/"+run.ID+"/fill", map[string]any{"proposalIds": []string{p2.ID}})
	var fills struct {
		Fills []service.GroupFill `json:"fills"`
	}
	if code != 200 || json.Unmarshal(b, &fills) != nil || len(fills.Fills) != 1 {
		t.Fatalf("fill → %d %s", code, b)
	}
	want := "\x15echo join --token " + token + " --v 1.31.2\n"
	if fills.Fills[0].Bytes != want {
		t.Fatalf("filled %+v", fills.Fills[0])
	}
	got = readPTY(bridge)
	if r := typeGroupOverSocket(t, driver, want, run.ID, p2.ID, false); r["ok"] != true {
		t.Fatalf("type filled → %v", r)
	}
	select {
	case hit := <-got:
		if hit != want {
			t.Fatalf("PTY got %q", hit)
		}
	case <-time.After(time.Second):
		t.Fatal("the filled step never reached the PTY")
	}
	for _, e := range f.logs.Recent(1000) {
		j, _ := json.Marshal(e)
		if strings.Contains(string(j), token) || strings.Contains(string(j), "1.31.2") {
			t.Fatalf("a value reached keel.log: %s", j)
		}
	}

	// Ansible: the inventory and the playbook, without any password.
	code, b = f.call(t, "GET", "/api/groups/"+g.ID+"/ansible?format=ini&runbookId="+saved.Runbook.ID, nil)
	var exp struct {
		Files []ExportFile `json:"files"`
	}
	if code != 200 || json.Unmarshal(b, &exp) != nil || len(exp.Files) != 2 {
		t.Fatalf("export → %d %s", code, b)
	}
	inv, pb := exp.Files[0], exp.Files[1]
	if inv.Name != "inventory.ini" || !strings.Contains(inv.Content, "[control_plane]") || !strings.Contains(inv.Content, "k8s_version=1.31.2") || !strings.Contains(inv.Content, "--ask-pass") {
		t.Fatalf("inventory:\n%s", inv.Content)
	}
	if pb.Name != "playbooks/bootstrap.yml" || !strings.Contains(pb.Content, "{{ join_token | quote }}") || !strings.Contains(pb.Content, "TODO: Keel captures join_token") {
		t.Fatalf("playbook:\n%s", pb.Content)
	}
	for _, secret := range []string{askHostPass, "other-pass-1234", token} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("the export holds %q", secret)
		}
	}
	// A variable kept before its value went into the vault (straight in the
	// store, past the save check) is left out of the export.
	if _, err := f.st.SetGroupVar(g.ID, "old_pw", askHostPass); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"ini", "yaml"} {
		code, b = f.call(t, "GET", "/api/groups/"+g.ID+"/ansible?format="+format, nil)
		if code != 200 || strings.Contains(string(b), askHostPass) || !strings.Contains(string(b), "old_pw: left out: its value is also in Keel's vault") {
			t.Fatalf("%s export with a vault value → %d %s", format, code, b)
		}
	}
	code, b = f.call(t, "GET", "/api/groups/"+g.ID+"/ansible?format=yaml", nil)
	if code != 200 || !strings.Contains(string(b), "inventory.yml") || strings.Contains(string(b), askHostPass) {
		t.Fatalf("yaml export → %d %s", code, b)
	}
}
