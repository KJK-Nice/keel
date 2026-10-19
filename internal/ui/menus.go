package ui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"keel/internal/store"
	"keel/internal/tunnel"
)

// sshCommand is the host's "Copy SSH command" text: ssh, the port when it
// is not 22, the saved jump host as -J, and user@address. It is built from
// the address fields only, never from a password, key, passphrase or any
// other vault value. The Local Shell has none ("").
//
// A user name or address that ssh could read as an option (a leading "-",
// as in "-oProxyCommand=…", which quoting does not stop) or that holds
// whitespace or a control character gets no command at all: hosts can come
// from an imported ~/.ssh/config, and a pasted command must never run
// anything but ssh to that host. The same holds for the jump host.
func sshCommand(h store.Host, hosts []store.Host) string {
	if h.AuthType == "local" || strings.TrimSpace(h.Address) == "" {
		return ""
	}
	if !sshSafe(h.Username) || !sshSafe(h.Address) {
		return ""
	}
	parts := []string{"ssh"}
	if h.Port != 0 && h.Port != 22 {
		parts = append(parts, "-p", strconv.Itoa(h.Port))
	}
	if h.JumpHost != "" {
		for _, j := range hosts {
			if j.ID == h.JumpHost && j.AuthType != "local" && strings.TrimSpace(j.Address) != "" {
				if !sshSafe(j.Username) || !sshSafe(j.Address) {
					return ""
				}
				jump := userAt(j.Username, j.Address)
				if j.Port != 0 && j.Port != 22 {
					jump += ":" + strconv.Itoa(j.Port)
				}
				parts = append(parts, "-J", shellWord(jump))
				break
			}
		}
	}
	parts = append(parts, shellWord(userAt(h.Username, h.Address)))
	return strings.Join(parts, " ")
}

// sshSafe reports a user name or address that ssh reads only as what it
// is: no leading "-", no whitespace, no control or invisible format
// character. An empty user name is safe (ssh then uses its default).
func sshSafe(s string) bool {
	if strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

func userAt(user, addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.Contains(addr, ":") && !strings.HasPrefix(addr, "[") {
		addr = "[" + addr + "]" // IPv6
	}
	if u := strings.TrimSpace(user); u != "" {
		return u + "@" + addr
	}
	return addr
}

// shellWord quotes s for a POSIX shell when it holds anything but the
// characters an address or a user name usually has.
func shellWord(s string) string {
	safe := s != ""
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("@%+=:,./_-[]", r)) {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// tunnelLocalAddress is the address on this machine a forward serves or
// reaches: where a local (-L) or dynamic (-D) forward listens (the bound
// address once it runs), and the destination a remote (-R) forward
// connects to. "" when there is none yet (an automatic port not bound).
func tunnelLocalAddress(t tunnel.Info) string {
	if t.Kind == "remote" {
		if t.DestHost == "" || t.DestPort == 0 {
			return ""
		}
		return fmt.Sprintf("%s:%d", t.DestHost, t.DestPort)
	}
	if t.ListenAddr != "" {
		return t.ListenAddr
	}
	if t.BindPort == 0 {
		return ""
	}
	addr := t.BindAddr
	if addr == "" {
		addr = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", addr, t.BindPort)
}

func hostAgentReadable(h store.Host) string {
	return strconv.FormatBool(h.ReadableByAgent())
}

func sideSnippetName(s store.Snippet) string {
	if s.Name == "" {
		return "Snippet"
	}
	return s.Name
}

func sideNotebookName(nb store.Notebook) string {
	if nb.Name == "" {
		return "Notebook"
	}
	return nb.Name
}

func pinLabel(nb store.Notebook) string {
	if nb.Pinned {
		return "Unpin"
	}
	return "Pin"
}

func boolAttr(b bool) string { return strconv.FormatBool(b) }
