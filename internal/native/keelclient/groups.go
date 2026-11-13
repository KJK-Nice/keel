package keelclient

import (
	"context"
	"net/http"
	"net/url"
)

// Groups (docs/ui-redesign.md §17; internal/httpapi/agentgroups.go, groupvars.go): a folder's members with their roles,
// its roles and variables, and the Ansible export. keel serve keeps all of it; this client holds it in memory only.
// Nothing in this file writes to a session.

// GroupMember is a host in a folder or in a folder below it, with its role there.
type GroupMember struct {
	HostID string `json:"hostId"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Folder string `json:"folder"` // the host's own folder (the group itself or one below it)
}

// GroupInfo is GET /api/groups/{id}/members.
type GroupInfo struct {
	Group   Group         `json:"group"`
	Members []GroupMember `json:"members"`
}

// GroupMembers is GET /api/groups/{id}/members: the hosts in the folder and in every folder below it, by name.
func (c *Client) GroupMembers(ctx context.Context, id string) (out GroupInfo, err error) {
	err = c.do(ctx, http.MethodGet, "/api/groups/"+url.PathEscape(id)+"/members", nil, &out)
	return out, err
}

// SetGroupRole is PUT /api/groups/{id}/roles {hostId, role}: an empty role removes the host's role. keel serve checks the
// word (one lower-case word, at most 32 bytes, at most 16 roles in a group).
func (c *Client) SetGroupRole(ctx context.Context, id, hostID, role string) (out Group, err error) {
	err = c.do(ctx, http.MethodPut, "/api/groups/"+url.PathEscape(id)+"/roles", map[string]string{"hostId": hostID, "role": role}, &out)
	return out, err
}

// SetGroupVar is PUT /api/groups/{id}/vars {name, value}: an empty value removes the variable. keel serve refuses a value
// that looks like a secret or holds a vault value ("Keep secrets in the vault"). A variable is config: its value is
// shown, filled into commands by keel serve alone, and exported.
func (c *Client) SetGroupVar(ctx context.Context, id, name, value string) (out Group, err error) {
	err = c.do(ctx, http.MethodPut, "/api/groups/"+url.PathEscape(id)+"/vars", map[string]string{"name": name, "value": value}, &out)
	return out, err
}

// ExportFile is one file of an Ansible export, as keel serve built it.
type ExportFile struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"` // inventory | playbook
	Content string `json:"content"`
	Lines   int    `json:"lines"`
}

// AnsibleQuery is the export route's query string (lib/groupvars.js exportQuery).
type AnsibleQuery struct {
	Format    string // "ini" or "yaml"
	RunbookID string // a runbook for a playbook; "" for the inventory only
	Mode      string // "rolling" or "parallel", for a playbook
	User      bool   // ansible_user from each host's saved user
	Jump      bool   // the jump host as ansible_ssh_common_args
}

// Values is the query string in the order lib/groupvars.js exportQuery builds it (format, runbookId, mode, user, jump).
func (q AnsibleQuery) Values() string {
	format := "ini"
	if q.Format == "yaml" {
		format = "yaml"
	}
	out := "format=" + format
	if q.RunbookID != "" {
		mode := "rolling"
		if q.Mode == "parallel" {
			mode = "parallel"
		}
		out += "&runbookId=" + url.QueryEscape(q.RunbookID) + "&mode=" + mode
	}
	if !q.User {
		out += "&user=0"
	}
	if q.Jump {
		out += "&jump=1"
	}
	return out
}

// GroupAnsible is GET /api/groups/{id}/ansible: the inventory and, with a runbook, a playbook, built by keel serve from
// the folder's members, roles and variables. No secret is in it (a variable holding a vault value is left out with a
// comment). Keel never runs Ansible.
func (c *Client) GroupAnsible(ctx context.Context, id string, q AnsibleQuery) ([]ExportFile, error) {
	var out struct {
		Files []ExportFile `json:"files"`
	}
	err := c.do(ctx, http.MethodGet, "/api/groups/"+url.PathEscape(id)+"/ansible?"+q.Values(), nil, &out)
	return out.Files, err
}
