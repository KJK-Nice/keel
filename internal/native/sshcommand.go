package native

import (
	"strconv"
	"strings"
	"unicode"

	"keel/internal/native/keelclient"
)

// sshCommand is the host menu's "Copy SSH command" text (internal/ui/menus.go sshCommand, with its cases): ssh, the port when
// it is not 22, the saved jump host as -J, and user@address. It is built from the address fields only, never from a password,
// key, passphrase or any other vault value (a keelclient.Host has none). The Local Shell has none (""). A user name or address
// that ssh could read as an option (a leading "-", which quoting does not stop) or that holds whitespace or a control or
// invisible character gets no command at all, and neither does a jump host like that: hosts can come from an imported
// ~/.ssh/config, and a pasted command must never run anything but ssh to that host. Copying it writes nothing to a session.
func sshCommand(h keelclient.Host, hosts []keelclient.Host) string {
	if h.AuthType == "local" || strings.TrimSpace(h.Address) == "" || !sshSafe(h.Username) || !sshSafe(h.Address) {
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
	return strings.Join(append(parts, shellWord(userAt(h.Username, h.Address))), " ")
}

// sshSafe reports a user name or address that ssh reads only as what it is. An empty user name is safe (ssh then uses its
// default).
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

// shellWord quotes s for a POSIX shell when it holds anything but the characters an address or a user name usually has.
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

// copyToClipboard has the next frame write text to the clipboard and say "Copied <what>" (term.js data-copy).
func (w *workspace) copyToClipboard(text, what string) { w.clip, w.clipWhat = text, what }
