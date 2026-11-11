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
