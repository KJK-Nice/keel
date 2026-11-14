package keelclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Keychain identities. GET /api/identities redacts every secret to "__saved__" (httpapi redactIdentity), and
// Identity keeps only whether one is stored: it has no field that could hold a private key, a password or a
// passphrase, whatever the server sent. A secret leaves in the one request that saves it (IdentityInput) and is
// not kept, logged or put in an error.

// Identity is a Keychain identity as the picker and the Keychain page show it.
type Identity struct {
	ID        string
	Name      string
	Kind      string // "key" or "password"
	Username  string
	CreatedAt time.Time

	// What the vault holds, never the value.
	HasPassword, HasPrivateKey, HasPassphrase bool
}

// UnmarshalJSON reads the API's identity and drops the redacted secrets down to a stored-or-not flag.
func (i *Identity) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID         string    `json:"id"`
		Name       string    `json:"name"`
		Kind       string    `json:"kind"`
		Username   string    `json:"username"`
		CreatedAt  time.Time `json:"createdAt"`
		Password   string    `json:"password"`
		PrivateKey string    `json:"privateKey"`
		Passphrase string    `json:"passphrase"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*i = Identity{ID: raw.ID, Name: raw.Name, Kind: raw.Kind, Username: raw.Username, CreatedAt: raw.CreatedAt,
		HasPassword: raw.Password != "", HasPrivateKey: raw.PrivateKey != "", HasPassphrase: raw.Passphrase != ""}
	return nil
}

// Identities is GET /api/identities.
func (c *Client) Identities(ctx context.Context) (out []Identity, err error) {
	err = c.do(ctx, http.MethodGet, "/api/identities", nil, &out)
	return out, err
}

// IdentityInput is what the identity dialog sends. A blank secret is left out and the server keeps the saved one.
// String and GoString leave the secrets out, so printing one with %v, %+v or %#v never shows them.
type IdentityInput struct {
	ID       string `json:"-"` // "" creates, else updates
	Name     string `json:"name"`
	Username string `json:"username"`
	Kind     string `json:"kind"`

	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"privateKey,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

func (in IdentityInput) String() string {
	return fmt.Sprintf("{%s %s %s %s}", in.ID, in.Name, in.Username, in.Kind)
}

func (in IdentityInput) GoString() string { return "keelclient.IdentityInput" + in.String() }

// SaveIdentity creates an identity (in.ID is empty: POST /api/identities) or updates one (PUT /api/identities/{id})
// and returns it, redacted.
func (c *Client) SaveIdentity(ctx context.Context, in IdentityInput) (out Identity, err error) {
	if in.ID == "" {
		err = c.do(ctx, http.MethodPost, "/api/identities", in, &out)
	} else {
		err = c.do(ctx, http.MethodPut, "/api/identities/"+url.PathEscape(in.ID), in, &out)
	}
	return out, err
}

// DeleteIdentity is DELETE /api/identities/{id}. The server clears the identity from the hosts that use it, so the
// Keychain page refuses to ask while a host uses it.
func (c *Client) DeleteIdentity(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/api/identities/"+url.PathEscape(id), nil, nil)
}
