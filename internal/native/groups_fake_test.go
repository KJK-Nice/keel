package native

import (
	"context"
	"fmt"
	"strings"

	"keel/internal/native/keelclient"
)

// The groups half of fakeBackend: roles, variables, the export and the group run, recorded, and answered as the test says.
// Nothing here reads a pane or types. Every value in these tests is made up.

// fakeVarSecret is what the fake refuses as a secret-looking variable value, as keel serve's Redactor would.
const fakeVarSecret = "fake-secret-looking-value"

type fakeGroups struct {
	roleReqs []string // "group host role"
	roleErr  error
	varReqs  []string // "group name" (the value is never recorded)
	varErr   error

	files     []keelclient.ExportFile
	ansibleQs []keelclient.AnsibleQuery
	ansibleEr error

	// The run keel serve holds, and everything asked of it.
	run      *keelclient.GroupRun
	runReqs  []string // "start rb group mode", "get", "stop", "mode m", "propose a,b", "fill p1,p2", "mark step host action"
	runErr   error
	proposed [][]string // the session ids of each propose
	fills    map[string]keelclient.GroupFill
	fillErr  error
	// onPropose lets a test make the proposals keel serve would make for the panes named.
	onPropose func(run *keelclient.GroupRun, panes []string)
	startRun  keelclient.GroupRun // what StartGroupRun answers
	started   int
}

func (f *fakeBackend) SetGroupRole(_ context.Context, id, hostID, role string) (keelclient.Group, error) {
	f.grp.roleReqs = append(f.grp.roleReqs, id+" "+hostID+" "+role)
	if f.grp.roleErr != nil {
		return keelclient.Group{}, f.grp.roleErr
	}
	if role != "" && !validRole(role) {
		return keelclient.Group{}, &keelclient.StatusError{Code: 400, Msg: "a role is one word"}
	}
	for i := range f.groups {
		if f.groups[i].ID == id {
			if f.groups[i].Roles == nil {
				f.groups[i].Roles = map[string]string{}
			}
			if role == "" {
				delete(f.groups[i].Roles, hostID)
			} else {
				f.groups[i].Roles[hostID] = role
			}
			return f.groups[i], nil
		}
	}
	return keelclient.Group{}, &keelclient.StatusError{Code: 404, Msg: "not found"}
}

func (f *fakeBackend) SetGroupVar(_ context.Context, id, name, value string) (keelclient.Group, error) {
	f.grp.varReqs = append(f.grp.varReqs, id+" "+name)
	if f.grp.varErr != nil {
		return keelclient.Group{}, f.grp.varErr
	}
	if strings.Contains(value, fakeVarSecret) {
		return keelclient.Group{}, &keelclient.StatusError{Code: 400, Msg: "Keep secrets in the vault"}
	}
	for i := range f.groups {
		if f.groups[i].ID != id {
			continue
		}
		var vars []keelclient.GroupVar
		found := false
		for _, v := range f.groups[i].Vars {
			if v.Name == name {
				found = true
				if value != "" {
					vars = append(vars, keelclient.GroupVar{Name: name, Value: value})
				}
				continue
			}
			vars = append(vars, v)
		}
		if !found && value != "" {
			vars = append(vars, keelclient.GroupVar{Name: name, Value: value})
		}
		f.groups[i].Vars = vars
		return f.groups[i], nil
	}
	return keelclient.Group{}, &keelclient.StatusError{Code: 404, Msg: "not found"}
}

func (f *fakeBackend) GroupAnsible(_ context.Context, id string, q keelclient.AnsibleQuery) ([]keelclient.ExportFile, error) {
	f.grp.ansibleQs = append(f.grp.ansibleQs, q)
	if f.grp.ansibleEr != nil {
		return nil, f.grp.ansibleEr
	}
	return f.grp.files, nil
}

func (f *fakeBackend) runAnswer() (keelclient.GroupRun, error) {
	if f.grp.runErr != nil {
		return keelclient.GroupRun{}, f.grp.runErr
	}
	if f.grp.run == nil {
		return keelclient.GroupRun{}, &keelclient.StatusError{Code: 404, Msg: "no such run"}
	}
	return *f.grp.run, nil
}

func (f *fakeBackend) StartGroupRun(_ context.Context, runbookID, groupID, mode string) (keelclient.GroupRun, error) {
	f.grp.runReqs = append(f.grp.runReqs, fmt.Sprintf("start %s %s %s", runbookID, groupID, mode))
	if f.grp.runErr != nil {
		return keelclient.GroupRun{}, f.grp.runErr
	}
	f.grp.started++
	run := f.grp.startRun
	f.grp.run = &run
	return run, nil
}

func (f *fakeBackend) GroupRun(context.Context, string) (keelclient.GroupRun, error) {
	f.grp.runReqs = append(f.grp.runReqs, "get")
	return f.runAnswer()
}

func (f *fakeBackend) StopGroupRun(context.Context, string) (keelclient.GroupRun, error) {
	f.grp.runReqs = append(f.grp.runReqs, "stop")
	if f.grp.run != nil {
		f.grp.run.Status = "stopped"
	}
	return f.runAnswer()
}

func (f *fakeBackend) SetGroupRunMode(_ context.Context, _, mode string) (keelclient.GroupRun, error) {
	f.grp.runReqs = append(f.grp.runReqs, "mode "+mode)
	if f.grp.run != nil && f.grp.runErr == nil {
		f.grp.run.Mode = mode
	}
	return f.runAnswer()
}

func (f *fakeBackend) ProposeGroupStep(_ context.Context, _ string, panes []string) (keelclient.GroupRun, error) {
	f.grp.runReqs = append(f.grp.runReqs, "propose "+strings.Join(panes, ","))
	f.grp.proposed = append(f.grp.proposed, panes)
	if f.grp.run != nil && f.grp.onPropose != nil {
		f.grp.onPropose(f.grp.run, panes)
	}
	return f.runAnswer()
}

// FillGroupStep answers with the fills the test set, one per proposal asked for.
func (f *fakeBackend) FillGroupStep(_ context.Context, _ string, ids []string) ([]keelclient.GroupFill, error) {
	f.grp.runReqs = append(f.grp.runReqs, "fill "+strings.Join(ids, ","))
	if f.grp.fillErr != nil {
		return nil, f.grp.fillErr
	}
	var out []keelclient.GroupFill
	for _, id := range ids {
		if fl, ok := f.grp.fills[id]; ok {
			fl.ProposalRef = id
			out = append(out, fl)
		}
	}
	return out, nil
}

func (f *fakeBackend) MarkGroupStep(_ context.Context, _, stepID, hostID, action string) (keelclient.GroupRun, error) {
	f.grp.runReqs = append(f.grp.runReqs, "mark "+stepID+" "+hostID+" "+action)
	return f.runAnswer()
}
