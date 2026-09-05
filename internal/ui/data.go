package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/a-h/templ"

	"keel/components/badge"
	"keel/internal/applog"
	"keel/internal/store"
	"keel/internal/tunnel"
)

var hostColors = []string{"#5ee0b5", "#6ea8ff", "#c084fc", "#f5c15d", "#ff6b7a", "#67e8f9"}

func mergeAttrs(parts ...templ.Attributes) templ.Attributes {
	out := templ.Attributes{}
	for _, p := range parts {
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

func tunnelBadgeVariant(status tunnel.Status) badge.Variant {
	switch status {
	case tunnel.StatusReady:
		return badge.VariantDefault
	case tunnel.StatusError:
		return badge.VariantDestructive
	case tunnel.StatusConnecting:
		return badge.VariantSecondary
	default:
		return badge.VariantOutline
	}
}

func logBadgeVariant(kind string) badge.Variant {
	switch strings.ToLower(kind) {
	case "error", "fail", "denied":
		return badge.VariantDestructive
	default:
		return badge.VariantSecondary
	}
}

type PageProps struct {
	Locked     bool
	NeedsSetup bool
	Titlebar   bool
	Native     bool
	Name       string
	SignalURL  string
	Query      string
	LogFilter  string
	Hosts      []store.Host
	Identities []store.Identity
	Tunnels    []tunnel.Info
	Snippets   []store.Snippet
	Logs       []applog.Entry
}

func (p PageProps) SignalsJSON() string {
	locked, setup := "false", "false"
	if p.Locked {
		locked = "true"
	}
	if p.NeedsSetup {
		setup = "true"
	}
	q, _ := json.Marshal(p.Query)
	lf, _ := json.Marshal(p.LogFilter)
	return fmt.Sprintf(
		`{"vault":"hosts","query":%s,"logFilter":%s,"locked":%s,"needsSetup":%s,"authType":"password","identKind":"password","tunnelKind":"local"}`,
		q, lf, locked, setup,
	)
}

func hostSubtitle(h store.Host) string {
	if h.AuthType == "local" {
		return "this machine"
	}
	user := h.Username
	if user == "" {
		user = "user"
	}
	addr := h.Address
	if h.Port > 0 && h.Port != 22 {
		addr = fmt.Sprintf("%s:%d", addr, h.Port)
	}
	return user + "@" + addr
}

func hostProtocol(h store.Host) string {
	if h.AuthType == "local" {
		return "local"
	}
	return "ssh"
}

func hostCountLabel(n int) string {
	if n == 1 {
		return "1 Host"
	}
	return fmt.Sprintf("%d Hosts", n)
}

func folderGroups(groups []hostGroup) []hostGroup {
	out := make([]hostGroup, 0)
	for _, g := range groups {
		if g.Name != "" && g.Name != "Hosts" {
			out = append(out, g)
		}
	}
	return out
}

func flatHosts(groups []hostGroup) []store.Host {
	n := 0
	for _, g := range groups {
		n += len(g.Hosts)
	}
	out := make([]store.Host, 0, n)
	for _, g := range groups {
		out = append(out, g.Hosts...)
	}
	return out
}

func groupHosts(hosts []store.Host, query string) []hostGroup {
	q := strings.ToLower(strings.TrimSpace(query))
	order := make([]string, 0)
	by := map[string][]store.Host{}
	for _, h := range hosts {
		if q != "" {
			blob := strings.ToLower(h.Name + " " + h.Address + " " + h.Username + " " + h.Group + " " + strings.Join(h.Tags, " "))
			if !strings.Contains(blob, q) {
				continue
			}
		}
		g := h.Group
		if g == "" {
			g = "Hosts"
		}
		if _, ok := by[g]; !ok {
			order = append(order, g)
		}
		by[g] = append(by[g], h)
	}
	out := make([]hostGroup, 0, len(order))
	for _, g := range order {
		out = append(out, hostGroup{Name: g, Hosts: by[g]})
	}
	return out
}

type hostGroup struct {
	Name  string
	Hosts []store.Host
}

func filterLogs(logs []applog.Entry, query string) []applog.Entry {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return logs
	}
	out := make([]applog.Entry, 0, len(logs))
	for _, e := range logs {
		if strings.Contains(strings.ToLower(e.Kind+" "+e.Message), q) {
			out = append(out, e)
		}
	}
	return out
}

func tunnelDest(t tunnel.Info) string {
	if t.Kind == "dynamic" {
		return "SOCKS5"
	}
	return fmt.Sprintf("%s:%d", t.DestHost, t.DestPort)
}

func tunnelListen(t tunnel.Info) string {
	if t.ListenAddr != "" {
		return t.ListenAddr
	}
	if t.BindPort == 0 {
		return t.BindAddr + ":auto"
	}
	return fmt.Sprintf("%s:%d", t.BindAddr, t.BindPort)
}

func hostNameByID(hosts []store.Host, id string) string {
	for _, h := range hosts {
		if h.ID == id {
			return h.Name
		}
	}
	return id
}

func identityColor(kind string) string {
	if kind == "key" {
		return "#c084fc"
	}
	return "#6ea8ff"
}

func identityKindLabel(kind string) string {
	if kind == "key" {
		return "Private key"
	}
	return "Auth password"
}

func tunnelColor(status tunnel.Status) string {
	switch status {
	case tunnel.StatusReady:
		return "#5ee0b5"
	case tunnel.StatusConnecting:
		return "#f5c15d"
	case tunnel.StatusError:
		return "#ff6b7a"
	default:
		return "#8a8a8a"
	}
}

func tunnelKindLetter(kind string) string {
	switch kind {
	case "remote":
		return "R"
	case "dynamic":
		return "D"
	default:
		return "L"
	}
}

func tunnelKindLabel(kind string) string {
	switch kind {
	case "remote":
		return "Remote"
	case "dynamic":
		return "Dynamic"
	default:
		return "Local"
	}
}

func snippetCommand(s store.Snippet) string {
	return strings.TrimSpace(s.Command)
}

func logColor(kind string) string {
	switch strings.ToLower(kind) {
	case "error", "fail", "denied":
		return "#ff6b7a"
	case "session":
		return "#5ee0b5"
	case "tunnel":
		return "#6ea8ff"
	default:
		return "#8a8a8a"
	}
}

func logDateLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("Jan 2, 2006")
}

func logTimeLabel(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("15:04:05")
}
