package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"keel/internal/agent"
	"keel/internal/agent/service"
	"keel/internal/ansible"
	"keel/internal/store"
)

// Groups, part 2 (docs/ui-redesign.md §17, boards CaptureVars,
// AnsibleExport and the GroupRoles drawer): group variables, filling a
// group run's step for the approve dialog, and the Ansible export. Nothing
// here writes to a session, reads a pane or sends anything off the
// machine (agentask_test.go checks this file).

// agentFillGroupRun is POST /api/agent/groupruns/{id}/fill {proposalIds}:
// for the approve dialog, each live proposal of the run with its variables
// filled in for its host (service.FillGroupProposals). It is the user's
// own page asking what Type would send; nothing is typed, sent off the
// machine or logged with a value. The session socket's Type fills again
// and compares (AuthorizeType), so the dialog shows the exact bytes.
func (s *Server) agentFillGroupRun(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		ProposalIDs []string `json:"proposalIds"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64*1024)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	if len(req.ProposalIDs) > 256 {
		writeError(w, http.StatusBadRequest, "too many proposals")
		return
	}
	fills, err := s.agent.FillGroupProposals(r.PathValue("id"), req.ProposalIDs)
	if err != nil {
		writeGroupRunErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fills": fills})
}

// setGroupVar is PUT /api/groups/{id}/vars {name, value}: an empty value
// removes the variable. The agent service refuses a value that looks like
// a secret or holds a vault value: group variables are config, shown,
// filled into commands and exported. Logged with the group id only.
func (s *Server) setGroupVar(w http.ResponseWriter, r *http.Request) {
	if s.agent == nil {
		writeError(w, http.StatusServiceUnavailable, "agent unavailable")
		return
	}
	var req struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid variable payload")
		return
	}
	g, err := s.agent.SetGroupVar(r.PathValue("id"), req.Name, req.Value)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrGroupVarSecret):
			writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), "agent: "))
		case errors.Is(err, store.ErrLocked):
			writeStoreErr(w, err)
		default:
			writeRunbookErr(w, err)
		}
		return
	}
	writeJSON(w, http.StatusOK, g)
	what := "set"
	if strings.TrimSpace(req.Value) == "" {
		what = "removed"
	}
	s.note("group", "group variable "+what+" in group "+g.ID, "", "", "")
}

// ExportFile is one file of an Ansible export.
type ExportFile struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // inventory | playbook
	Content string `json:"content"`
	Lines   int    `json:"lines"`
}

// groupAnsible is GET /api/groups/{id}/ansible?format=ini|yaml
// [&runbookId=…&mode=rolling|parallel][&user=0][&jump=1]: the group's
// inventory (its members, roles as Ansible groups, its variables) and, with
// a runbook, a playbook of its steps. Keel never runs Ansible: the page
// offers the text to copy or download. The export is built from fields
// with no secret in them (ansible.Host has no field for a password, key
// or passphrase); a host whose key or password is in the vault gets a
// comment instead. Logged with ids only.
func (s *Server) groupAnsible(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	g, members, err := s.store.GroupMembers(r.PathValue("id"))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	hosts, err := s.store.ListHosts()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	byID := map[string]store.Host{}
	for _, h := range hosts {
		byID[h.ID] = h
	}
	inv := ansible.Inventory{
		Group:   g.Name,
		UseUser: q.Get("user") != "0",
		UseJump: q.Get("jump") == "1",
		Stamp:   time.Now().Format("2006-01-02"),
	}
	for _, m := range members {
		h, ok := byID[m.HostID]
		if !ok {
			continue
		}
		inv.Hosts = append(inv.Hosts, exportHost(h, m.Role, byID))
	}
	for _, v := range g.Vars {
		inv.Vars = append(inv.Vars, ansible.Var{Name: v.Name, Value: v.Value})
	}
	format := q.Get("format")
	if format != "yaml" {
		format = "ini"
	}
	var files []ExportFile
	if format == "yaml" {
		files = append(files, exportFile("inventory.yml", "inventory", ansible.YAML(inv)))
	} else {
		files = append(files, exportFile("inventory.ini", "inventory", ansible.INI(inv)))
	}
	runbookID := q.Get("runbookId")
	if runbookID != "" {
		b, err := s.store.GetRunbook(runbookID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
		mode := q.Get("mode")
		if mode != "parallel" {
			mode = "rolling"
		}
		pb := ansible.Playbook{Name: b.Name, Mode: mode}
		for _, st := range b.Steps {
			step := ansible.Step{Title: st.Title, Description: st.Description, Command: st.Command, Role: st.Role, OneHost: st.OneHost}
			if st.Command != "" {
				step.ReadOnly = agent.Classify(st.Command) == agent.RiskReadOnly
			}
			for _, c := range st.Captures {
				step.Captures = append(step.Captures, ansible.Capture{Name: c.Name, Pattern: c.Pattern})
			}
			pb.Steps = append(pb.Steps, step)
		}
		files = append(files, exportFile("playbooks/"+ansible.Slug(b.Name)+".yml", "playbook", ansible.PlaybookYAML(pb, inv)))
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
	msg := fmt.Sprintf("ansible export of group %s (%d hosts, %s inventory)", g.ID, len(inv.Hosts), format)
	if runbookID != "" {
		msg += " with a playbook of runbook " + runbookID
	}
	s.note("group", msg, "", "", "")
}

// exportHost takes only what an inventory may hold from a saved host:
// never its password, private key or passphrase.
func exportHost(h store.Host, role string, byID map[string]store.Host) ansible.Host {
	out := ansible.Host{Name: h.Name, Address: h.Address, Port: h.Port, User: h.Username, Auth: h.AuthType, Role: role, Production: h.IsProduction()}
	if h.JumpHost != "" {
		if j, ok := byID[h.JumpHost]; ok {
			out.Jump = &ansible.Jump{Name: j.Name, Address: j.Address, Port: j.Port, User: j.Username}
		}
	}
	return out
}

func exportFile(name, kind, content string) ExportFile {
	return ExportFile{Name: name, Kind: kind, Content: content, Lines: strings.Count(content, "\n")}
}
