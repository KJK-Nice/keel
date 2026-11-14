package keelclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// First run: the vault, the ssh config import and the agent step's text. The server reads the ssh config
// (KEEL_SSH_DIR in keel serve); this client never reads ~/.ssh. Passphrases leave in the one request that needs
// them, are not kept, and never appear in an error or in %v, %+v or %#v.

// ErrVaultExists is a vault that already exists (409 from POST /api/vault/setup).
var ErrVaultExists = errors.New("the vault already exists")

// SetupVault is POST /api/vault/setup: it creates the vault with a passphrase. A keychain vault is not
// a request: keel serve makes it itself when it starts. A vault that exists is ErrVaultExists.
func (c *Client) SetupVault(ctx context.Context, passphrase string) error {
	err := c.do(ctx, http.MethodPost, "/api/vault/setup", map[string]string{"passphrase": passphrase}, nil)
	var se *StatusError
	if errors.As(err, &se) && se.Code == http.StatusConflict {
		return ErrVaultExists
	}
	return err
}

// FirstRunDone is POST /api/first-run/done.
func (c *Client) FirstRunDone(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/first-run/done", nil, nil)
}

// ConfigHost is one Host block of the ssh config, as GET /api/ssh-config/preview lists it.
type ConfigHost struct {
	Alias         string   `json:"alias"`
	HostName      string   `json:"hostName"`
	User          string   `json:"user"`
	Port          int      `json:"port"`
	IdentityFiles []string `json:"identityFiles"`
	Proxy         string   `json:"proxy"` // "ProxyJump", "ProxyCommand" or ""
	ProxyJump     string   `json:"proxyJump"`
	Source        string   `json:"source"`
	Notes         []string `json:"notes"`
	Saved         bool     `json:"saved"` // a host with the same address, user and port is in the vault
	JumpAlias     string   `json:"jumpAlias"`
	JumpSaved     string   `json:"jumpSaved"`
	JumpVia       string   `json:"jumpVia"`
	JumpProblem   string   `json:"jumpProblem"`
}

// ConfigKey is a private key the config names: its path and what the server found out, never its content.
type ConfigKey struct {
	Path      string   `json:"path"`
	Display   string   `json:"display"`
	Name      string   `json:"name"`
	Exists    bool     `json:"exists"`
	Encrypted bool     `json:"encrypted"`
	Problem   string   `json:"problem"`
	UsedBy    []string `json:"usedBy"`
}

// ConfigPattern is a Host pattern (a wildcard or a negation): not a host to add.
type ConfigPattern struct {
	Pattern string `json:"pattern"`
	Source  string `json:"source"`
}

// ConfigNote is something the parser skipped or changed.
type ConfigNote struct {
	Source string `json:"source"`
	Text   string `json:"text"`
}

// SSHPreview is GET /api/ssh-config/preview.
type SSHPreview struct {
	Path     string          `json:"path"`
	Found    bool            `json:"found"`
	Hosts    []ConfigHost    `json:"hosts"`
	Patterns []ConfigPattern `json:"patterns"`
	Keys     []ConfigKey     `json:"keys"`
	Notes    []ConfigNote    `json:"notes"`
}

// SSHConfigPreview is GET /api/ssh-config/preview.
func (c *Client) SSHConfigPreview(ctx context.Context) (out SSHPreview, err error) {
	err = c.do(ctx, http.MethodGet, "/api/ssh-config/preview", nil, &out)
	return out, err
}

// ImportKey turns one key on, with its passphrase when it has one. String leaves the passphrase out.
type ImportKey struct {
	Path       string `json:"path"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (k ImportKey) String() string { return "{" + k.Path + "}" }

func (k ImportKey) GoString() string { return "keelclient.ImportKey" + k.String() }

// ImportRequest is the body of POST /api/ssh-config/import: the aliases the user ticked, the keys whose copy
// switch the user turned on, and the ProxyJump hosts to add without their jump. Nothing else is sent: no address
// or path of its own.
type ImportRequest struct {
	Hosts       []string    `json:"hosts"`
	Keys        []ImportKey `json:"keys"`
	WithoutJump []string    `json:"withoutJump,omitempty"`
}

func (r ImportRequest) String() string {
	return fmt.Sprintf("{%d hosts %d keys %d without jump}", len(r.Hosts), len(r.Keys), len(r.WithoutJump))
}

func (r ImportRequest) GoString() string { return "keelclient.ImportRequest" + r.String() }

// ImportFailure is a host that was ticked but not added.
type ImportFailure struct {
	Alias string `json:"alias"`
	Error string `json:"error"`
}

// ImportResult is what the import added (secrets redacted by the server).
type ImportResult struct {
	Hosts      []Host          `json:"hosts"`
	Identities []Identity      `json:"identities"`
	Failed     []ImportFailure `json:"failed"`
}

// SSHConfigImport is POST /api/ssh-config/import. A refusal is a *StatusError whose Msg is the server's text, and
// whose ErrorKind and Key say which key it was about.
func (c *Client) SSHConfigImport(ctx context.Context, req ImportRequest) (out ImportResult, err error) {
	if req.Hosts == nil {
		req.Hosts = []string{}
	}
	if req.Keys == nil {
		req.Keys = []ImportKey{}
	}
	err = c.do(ctx, http.MethodPost, "/api/ssh-config/import", req, &out)
	return out, err
}

// LogEntry is one row of GET /api/logs. Agent entries carry counts, never content.
type LogEntry struct {
	Time       time.Time      `json:"time"`
	Kind       string         `json:"kind"`
	Message    string         `json:"message"`
	HostID     string         `json:"hostId"`
	SessionID  string         `json:"sessionId"`
	TunnelID   string         `json:"tunnelId"`
	Lines      int            `json:"lines"`
	Bytes      int            `json:"bytes"`
	Redactions map[string]int `json:"redactions"`
	Source     string         `json:"source"`
	Count      int            `json:"count"`
	ModelMs    int64          `json:"modelMs"`
	JevMs      int64          `json:"jevMs"`
}

// Logs is GET /api/logs?limit=n: the newest entries first. A limit of 0 is the server's default (200).
func (c *Client) Logs(ctx context.Context, limit int) (out []LogEntry, err error) {
	path := "/api/logs"
	if limit > 0 {
		path += "?" + url.Values{"limit": {strconv.Itoa(limit)}}.Encode()
	}
	err = c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}
