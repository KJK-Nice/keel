package keelclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// Hosts, identities, folders and the vault lock. Every secret in this file (the vault passphrase, a host's
// password, key and key passphrase) leaves in the one request that needs it. None is stored, logged or put
// in an error, and the types that carry one print without it.

// ErrPassphrase is a refused vault passphrase (403 from POST /api/vault/unlock).
var ErrPassphrase = errors.New("incorrect passphrase")

// Unlock is POST /api/vault/unlock. A wrong passphrase is ErrPassphrase; any other failure is an error that
// names the method, path and status only.
func (c *Client) Unlock(ctx context.Context, passphrase string) error {
	err := c.do(ctx, http.MethodPost, "/api/vault/unlock", map[string]string{"passphrase": passphrase}, nil)
	var se *StatusError
	if errors.As(err, &se) && se.Code == http.StatusForbidden {
		return ErrPassphrase
	}
	return err
}

// HostDetail is what the Edit host dialog sends to create, update or test a host. A blank Password, PrivateKey
// or Passphrase is left out of the body, and the server reads that as "keep the saved secret" on an update
// (store.putSecretLocked) and as "use the saved secret" for a test of an unchanged host (store.ResolveDraft).
// String and GoString leave the secrets out, so printing a HostDetail with %v, %+v or %#v never shows them.
type HostDetail struct {
	ID             string   `json:"id,omitempty"`
	Name           string   `json:"name"`
	GroupID        string   `json:"groupId"`
	Tags           []string `json:"tags"`
	Address        string   `json:"address"`
	Port           int      `json:"port"`
	Username       string   `json:"username"`
	AuthType       string   `json:"authType"`
	IdentityID     string   `json:"identityId,omitempty"`
	Password       string   `json:"password,omitempty"`
	PrivateKey     string   `json:"privateKey,omitempty"`
	Passphrase     string   `json:"passphrase,omitempty"`
	Color          string   `json:"color"`
	JumpHost       string   `json:"jumpHost,omitempty"`
	StartupCommand string   `json:"startupCommand,omitempty"`
	// KeepAliveSec is the interval, -1 for off, 0 for a local shell.
	KeepAliveSec    int   `json:"keepAliveSec"`
	NoAutoReconnect bool  `json:"noAutoReconnect"`
	AgentReadable   *bool `json:"agentReadable,omitempty"`
	Production      *bool `json:"production,omitempty"`
	AgentNotes      *bool `json:"agentNotes,omitempty"`
}

func (h HostDetail) String() string {
	return fmt.Sprintf("{%s %s %s@%s:%d %s}", h.ID, h.Name, h.Username, h.Address, h.Port, h.AuthType)
}

func (h HostDetail) GoString() string { return "keelclient.HostDetail" + h.String() }

// SaveHost creates a host (h.ID is empty: POST /api/hosts) or updates one (PUT /api/hosts/{id}). It returns the
// saved host, redacted.
func (c *Client) SaveHost(ctx context.Context, h HostDetail) (out Host, err error) {
	if h.ID == "" {
		err = c.do(ctx, http.MethodPost, "/api/hosts", h, &out)
	} else {
		err = c.do(ctx, http.MethodPut, "/api/hosts/"+url.PathEscape(h.ID), h, &out)
	}
	return out, err
}

// DeleteHost is DELETE /api/hosts/{id}.
func (c *Client) DeleteHost(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/hosts/"+url.PathEscape(id), nil, nil)
}

// DuplicateHost is POST /api/hosts/{id}/duplicate: the server copies the host, secrets included, as "name (copy)".
func (c *Client) DuplicateHost(ctx context.Context, id string) (out Host, err error) {
	err = c.do(ctx, http.MethodPost, "/api/hosts/"+url.PathEscape(id)+"/duplicate", nil, &out)
	return out, err
}

// SetAgentReadable is PUT /api/hosts/{id}/agent-readable: the host menu's "Agent can read".
func (c *Client) SetAgentReadable(ctx context.Context, id string, readable bool) error {
	return c.do(ctx, http.MethodPut, "/api/hosts/"+url.PathEscape(id)+"/agent-readable", map[string]bool{"readable": readable}, nil)
}

// TestResult is what POST /api/hosts/test answers (session.ProbeResult). Kind is ok, auth, unknown-host,
// host-key-mismatch, timeout, dns, refused or another word for any other failure. TrustToken is a one-time
// capability for the offered key: String leaves it out.
type TestResult struct {
	OK          bool   `json:"ok"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	LatencyMs   int64  `json:"latencyMs"`
	Hop         string `json:"hop"` // "jump" or "target" when a jump host was used and the connect failed
	HopName     string `json:"hopName"`
	Hostname    string `json:"hostname"`
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
	Expected    string `json:"expected"`
	TrustToken  string `json:"trustToken"`
}

func (r TestResult) String() string {
	return fmt.Sprintf("{%s ok=%v %s %s %s}", r.Kind, r.OK, r.Hostname, r.Fingerprint, r.Message)
}

func (r TestResult) GoString() string { return "keelclient.TestResult" + r.String() }

// HostKeyOffer is the key a test was refused for, as TrustHostKey sends it back: exactly what the server
// offered. ok is false when the result offers nothing to trust. Only the user's click on the trust button may
// send it.
func (r TestResult) HostKeyOffer() (k HostKey, ok bool) {
	if (r.Kind != "unknown-host" && r.Kind != "host-key-mismatch") || r.Fingerprint == "" || r.TrustToken == "" {
		return HostKey{}, false
	}
	return HostKey{Kind: r.Kind, Hostname: r.Hostname, Fingerprint: r.Fingerprint, Expected: r.Expected, KeyType: r.KeyType, Token: r.TrustToken}, true
}

// TestHost is POST /api/hosts/test: a connect and authentication with the dialog's unsaved values, then close.
// It opens no session and trusts no key. Cancel ctx to stop it; the server also stops it after 10 s.
func (c *Client) TestHost(ctx context.Context, h HostDetail) (out TestResult, err error) {
	err = c.do(ctx, http.MethodPost, "/api/hosts/test", h, &out)
	return out, err
}

// KnownKey is the saved host key of a host's address:port (GET /api/hosts/{id}/host-key).
type KnownKey struct {
	Hostname    string `json:"hostname"`
	Fingerprint string `json:"fingerprint"`
	KeyType     string `json:"keyType"`
	Known       bool   `json:"known"`
}

// HostKey is GET /api/hosts/{id}/host-key.
func (c *Client) HostKey(ctx context.Context, id string) (k KnownKey, err error) {
	err = c.do(ctx, http.MethodGet, "/api/hosts/"+url.PathEscape(id)+"/host-key", nil, &k)
	return k, err
}

// ForgetHostKey is DELETE /api/hosts/{id}/host-key. Ask the user first.
func (c *Client) ForgetHostKey(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/hosts/"+url.PathEscape(id)+"/host-key", nil, nil)
}

// GroupVar is one group variable (docs/ui-redesign.md §17 part 2): config, not a secret. keel serve refuses a value that
// looks like a secret or holds a vault value.
type GroupVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Group is a folder of the host tree. Roles (host id to role) and Vars are kept by keel serve; a folder's save never
// sends them.
type Group struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	ParentID string            `json:"parentId"`
	Roles    map[string]string `json:"roles,omitempty"`
	Vars     []GroupVar        `json:"vars,omitempty"`
}

// Groups is GET /api/groups.
func (c *Client) Groups(ctx context.Context) (out []Group, err error) {
	err = c.do(ctx, http.MethodGet, "/api/groups", nil, &out)
	return out, err
}

// SaveGroup creates a folder (g.ID is empty: POST /api/groups) or updates one (PUT /api/groups/{id}). Only the name
// and the parent are sent: roles and variables change only through SetGroupRole and SetGroupVar (store.UpsertGroup keeps
// them). It returns the saved folder.
func (c *Client) SaveGroup(ctx context.Context, g Group) (out Group, err error) {
	body := map[string]string{"name": g.Name, "parentId": g.ParentID}
	if g.ID == "" {
		err = c.do(ctx, http.MethodPost, "/api/groups", body, &out)
	} else {
		err = c.do(ctx, http.MethodPut, "/api/groups/"+url.PathEscape(g.ID), body, &out)
	}
	return out, err
}

// DeleteGroup is DELETE /api/groups/{id}: its hosts and subfolders move up one level and its notes for the agent go
// with it. Call it only after the user confirmed.
func (c *Client) DeleteGroup(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/groups/"+url.PathEscape(id), nil, nil)
}

// MoveHosts is POST /api/hosts/move: the hosts go into the folder groupID ("" takes them out of any folder).
func (c *Client) MoveHosts(ctx context.Context, ids []string, groupID string) error {
	return c.do(ctx, http.MethodPost, "/api/hosts/move", map[string]any{"ids": ids, "groupId": groupID}, nil)
}
