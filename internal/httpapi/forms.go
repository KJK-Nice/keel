package httpapi

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	"keel/internal/store"
)

// Vault dialogs post with Datastar (docs/ui-redesign.md §12, D3):
//
//	<form data-on:submit__prevent="@post('/ui/forms/group', {contentType: 'form'})">
//
// The handler saves through the same functions as the JSON API below and
// answers over SSE: on an error it patches the form's #<kind>-form-error;
// on success it clears that and dispatches keel-form-saved, which term.js
// uses to close the dialog. The lists themselves change through the
// stream (uistream.go), like any other save.

// inputError is a problem with what the user typed: the API answers 400.
type inputError struct{ msg string }

func (e inputError) Error() string { return e.msg }

func badInput(msg string) error { return inputError{msg} }

func isInputError(err error) bool {
	var ie inputError
	return errors.As(err, &ie)
}

// writeSaveErr answers a JSON API save that failed: 400 for input errors,
// the store's mapping otherwise.
func writeSaveErr(w http.ResponseWriter, err error) {
	if isInputError(err) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeStoreErr(w, err)
}

// saveHost creates (h.ID "") or updates a host. Blank secrets keep the
// saved ones (store.UpsertHost).
func (s *Server) saveHost(h store.Host) (store.Host, error) {
	if err := validateHost(h); err != nil {
		return store.Host{}, badInput(err.Error())
	}
	isNew := h.ID == ""
	saved, err := s.store.UpsertHost(h)
	if err != nil {
		return store.Host{}, err
	}
	if isNew {
		s.note("host", "host created: "+saved.Name, saved.ID, "", "")
	} else {
		s.note("host", "host updated: "+saved.Name, saved.ID, "", "")
	}
	return saved, nil
}

// saveGroup creates (id "") or updates a group.
func (s *Server) saveGroup(id, name, parentID string) (store.Group, error) {
	if strings.TrimSpace(name) == "" {
		return store.Group{}, badInput("name is required")
	}
	g := store.Group{Name: name, ParentID: parentID}
	if id != "" {
		for _, x := range s.mustGroups() {
			if x.ID == id {
				g = x
				g.Name, g.ParentID = name, parentID
			}
		}
	}
	g.ID = id
	saved, err := s.store.UpsertGroup(g)
	if err != nil {
		return store.Group{}, err
	}
	if id == "" {
		s.note("group", "group created: "+saved.Name, "", "", "")
	} else {
		s.note("group", "group updated: "+saved.Name, "", "", "")
	}
	return saved, nil
}

func (s *Server) mustGroups() []store.Group {
	gs, _ := s.store.ListGroups()
	return gs
}

// saveIdentity creates (in.ID "") or updates an identity. Blank secrets
// keep the saved ones (store.UpsertIdentity).
func (s *Server) saveIdentity(in store.Identity) (store.Identity, error) {
	if err := validateIdentity(in); err != nil {
		return store.Identity{}, badInput(err.Error())
	}
	isNew := in.ID == ""
	saved, err := s.store.UpsertIdentity(in)
	if err != nil {
		return store.Identity{}, err
	}
	if isNew {
		s.note("identity", "identity created: "+saved.Name, "", "", "")
	} else {
		s.note("identity", "identity updated: "+saved.Name, "", "", "")
	}
	return saved, nil
}

// saveTunnel creates (tn.ID "") or updates a forward. Store validation
// errors (ports, bind address, SOCKS exposure) are the user's to fix.
func (s *Server) saveTunnel(tn store.Tunnel) (store.Tunnel, error) {
	isNew := tn.ID == ""
	saved, err := s.store.UpsertTunnel(tn)
	if err != nil {
		if errors.Is(err, store.ErrLocked) || errors.Is(err, store.ErrNotFound) {
			return store.Tunnel{}, err
		}
		return store.Tunnel{}, badInput(err.Error())
	}
	if isNew {
		s.note("tunnel", "tunnel created: "+saved.Name, saved.HostID, "", saved.ID)
	} else {
		s.note("tunnel", "tunnel updated: "+saved.Name, saved.HostID, "", saved.ID)
	}
	return saved, nil
}

// saveSnippet creates (id "") or updates a snippet. A nil agentUsable keeps
// the saved flag: the snippet dialog has no agent switch.
func (s *Server) saveSnippet(id, name, command string, agentUsable *bool) (store.Snippet, error) {
	var old store.Snippet
	if id != "" {
		all, err := s.store.ListSnippets()
		if err != nil {
			return store.Snippet{}, err
		}
		for _, x := range all {
			if x.ID == id {
				old = x
			}
		}
	} else if strings.TrimSpace(command) == "" {
		return store.Snippet{}, badInput("name and command are required")
	}
	if strings.TrimSpace(name) == "" {
		if id != "" {
			return store.Snippet{}, badInput("Give the snippet a name")
		}
		return store.Snippet{}, badInput("name and command are required")
	}
	sn := store.Snippet{ID: id, Name: name, Command: command, AgentUsable: old.AgentUsable}
	if agentUsable != nil {
		sn.AgentUsable = *agentUsable
	}
	saved, err := s.store.UpsertSnippet(sn)
	if err != nil {
		return store.Snippet{}, err
	}
	if id == "" {
		s.note("snippet", "snippet created: "+saved.Name, "", "", "")
	} else {
		s.note("snippet", "snippet updated: "+saved.Name, "", "", "")
	}
	s.noteSnippetFlag(saved.Name, old.AgentUsable, saved.AgentUsable)
	return saved, nil
}

// uiForm handles POST /ui/forms/{kind} from the vault dialogs.
func (s *Server) uiForm(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	if err := r.ParseMultipartForm(1 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		writeError(w, http.StatusBadRequest, "invalid form")
		return
	}
	f := func(name string) string { return strings.TrimSpace(r.FormValue(name)) }
	var (
		msg string
		err error
	)
	switch kind {
	case "host":
		_, err = s.saveHost(hostFromForm(f, r.FormValue))
		msg = "Host saved"
	case "group":
		_, err = s.saveGroup(f("id"), f("name"), f("parentId"))
		msg = "Group saved"
	case "identity":
		_, err = s.saveIdentity(store.Identity{
			ID:         f("id"),
			Name:       f("name"),
			Username:   f("username"),
			Kind:       f("kind"),
			Password:   formSecret(r.FormValue("password")),
			PrivateKey: formSecret(r.FormValue("privateKey")),
			Passphrase: formSecret(r.FormValue("passphrase")),
		})
		msg = "Identity saved"
	case "tunnel":
		if f("hostId") == "" {
			err = badInput("Pick an SSH host first")
			break
		}
		bindAddr := f("bindAddr")
		if bindAddr == "" {
			bindAddr = "127.0.0.1"
		}
		_, err = s.saveTunnel(store.Tunnel{
			ID:       f("id"),
			Name:     f("name"),
			HostID:   f("hostId"),
			Kind:     f("kind"),
			BindAddr: bindAddr,
			BindPort: formInt(f("bindPort")),
			DestHost: f("destHost"),
			DestPort: formInt(f("destPort")),
		})
		msg = "Forward saved"
	case "snippet":
		command := r.FormValue("command")
		if command != "" && !strings.HasSuffix(command, "\n") {
			command += "\n"
		}
		_, err = s.saveSnippet(f("id"), f("name"), command, nil)
		msg = "Snippet saved"
	case "snippet-editor":
		// The Snippets page editor: an existing snippet, with the agent switch.
		if f("id") == "" {
			err = badInput("Pick a snippet first")
			break
		}
		command := r.FormValue("command")
		if !strings.HasSuffix(command, "\n") {
			command += "\n"
		}
		agent := f("agentUsable") == "true"
		_, err = s.saveSnippet(f("id"), f("name"), command, &agent)
		msg = "Snippet saved"
	default:
		writeError(w, http.StatusNotFound, "unknown form")
		return
	}

	sse := datastar.NewSSE(w, r)
	errID := kind + "-form-error"
	if err != nil {
		text := err.Error()
		if errors.Is(err, store.ErrLocked) {
			text = "The vault is locked."
		} else if errors.Is(err, store.ErrNotFound) {
			text = "It was deleted elsewhere; close this dialog."
		}
		_ = sse.PatchElements(fmt.Sprintf(`<p id="%s" class="k-error" role="alert">%s</p>`, errID, html.EscapeString(text)))
		return
	}
	_ = sse.PatchElements(fmt.Sprintf(`<p id="%s" class="k-error" role="alert" hidden></p>`, errID))
	_ = sse.ExecuteScript(fmt.Sprintf(
		`window.dispatchEvent(new CustomEvent("keel-form-saved", {detail: {form: %q, message: %q}}))`,
		kind, msg))
}

// hostFromForm reads the host dialog. Switches post as hidden "true" /
// "false" inputs; checkboxes are present only when checked. A local shell
// has no keepalive, reconnect, jump host or startup command.
func hostFromForm(f, raw func(string) string) store.Host {
	auth := f("authType")
	port := formInt(f("port"))
	if port == 0 {
		port = 22
	}
	h := store.Host{
		ID:             f("id"),
		Name:           f("name"),
		GroupID:        f("groupId"),
		Tags:           formTags(f("tags")),
		Address:        f("address"),
		Port:           port,
		Username:       f("username"),
		AuthType:       auth,
		Password:       formSecret(raw("password")),
		PrivateKey:     formSecret(raw("privateKey")),
		Passphrase:     formSecret(raw("passphrase")),
		Color:          f("color"),
		JumpHost:       f("jumpHost"),
		StartupCommand: f("startupCommand"),
		AgentReadable:  formBool(f("agentReadable"), true),
		Production:     formBool(f("production"), false),
		AgentNotes:     formBool(f("agentNotes"), true),
	}
	if auth == "identity" {
		h.IdentityID = f("identityId")
	}
	switch {
	case auth == "local":
		h.KeepAliveSec, h.JumpHost, h.StartupCommand = 0, "", ""
	case f("keepAlive") != "":
		h.KeepAliveSec = formInt(f("keepAliveSec"))
		if h.KeepAliveSec <= 0 {
			h.KeepAliveSec = 30
		}
	default:
		h.KeepAliveSec = -1
	}
	h.NoAutoReconnect = auth != "local" && f("autoReconnect") == ""
	return h
}

func formTags(v string) []string {
	var out []string
	for _, t := range strings.Split(v, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func formBool(v string, def bool) *bool {
	b := def
	switch v {
	case "true":
		b = true
	case "false":
		b = false
	}
	return &b
}

// formSecret maps the dialogs' "keep the saved secret" placeholder value to
// blank, which the store reads as "unchanged".
func formSecret(v string) string {
	if v == "__saved__" {
		return ""
	}
	return v
}

func formInt(v string) int {
	n, _ := strconv.Atoi(v)
	return n
}
