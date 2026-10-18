// Package redact replaces secrets (always) and, when asked, IP addresses
// and hostnames in terminal text before any of it can leave the machine.
//
// It is pure: no I/O, no globals that change, no clock. One Redactor serves
// one agent request, so a value seen twice in that request gets the same
// placeholder ([secret:1], [ip:2], [host:1]) and the model can still tell
// that two lines mention the same thing. Report counts what was replaced,
// per kind, for keel.log; it never holds the values.
//
// The only way to get a Safe value (what a provider request accepts) is
// through a Redactor or a compile-time constant (Static), so text that
// skipped redaction cannot be put in a provider request by mistake.
package redact

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Kind is one rule that matched. Kinds are stable: they are written to
// keel.log and tested.
type Kind string

const (
	KindPrivateKey     Kind = "private_key"     // PEM / OpenSSH / PGP private key blocks, age secret keys
	KindPasswordPrompt Kind = "password_prompt" // text after "Password:" / "passphrase for …:" on the same line
	KindAssignment     Kind = "assignment"      // password=… token: … "api_key": "…" --secret=…
	KindCLIFlag        Kind = "cli_flag"        // --password x, mysql -pX, sshpass -p x, curl -u user:pass
	KindAuthHeader     Kind = "auth_header"     // Authorization:, Bearer …, Cookie:, X-Api-Key:
	KindAWSKey         Kind = "aws_key"
	KindGoogleKey      Kind = "google_key" // AIza… (Gemini, GCP), ya29. OAuth, GOCSPX- client secrets
	KindGitHubToken    Kind = "github_token"
	KindGitLabToken    Kind = "gitlab_token"
	KindSlackToken     Kind = "slack_token"
	KindStripeKey      Kind = "stripe_key"
	KindOpenAIKey      Kind = "openai_key"
	KindAnthropicKey   Kind = "anthropic_key"
	KindOtherToken     Kind = "other_token" // npm, Hugging Face, SendGrid, DigitalOcean
	KindJWT            Kind = "jwt"
	KindURLUserinfo    Kind = "url_userinfo"
	KindEnvLine        Kind = "env_line"
	KindHighEntropy    Kind = "high_entropy"
	KindKnownSecret    Kind = "known_secret" // a value stored in the vault
	KindIP             Kind = "ip"
	KindHostname       Kind = "hostname"
)

// category is the placeholder family a kind belongs to.
type category string

const (
	catSecret category = "secret"
	catIP     category = "ip"
	catHost   category = "host"
)

func (k Kind) category() category {
	switch k {
	case KindIP:
		return catIP
	case KindHostname:
		return catHost
	default:
		return catSecret
	}
}

// Options choose the optional rules and carry the exact-match lists.
// Secrets are always redacted; there is no option to turn that off.
type Options struct {
	// IPs redacts IPv4 and IPv6 addresses (loopback and unspecified kept).
	IPs bool
	// Hostnames redacts domain names, the host part of user@host, and
	// the saved host addresses in Hosts.
	Hostnames bool
	// Known are secret values from the vault, replaced wherever they
	// appear (multi-line values also line by line).
	Known []string
	// Hosts are the user's saved host addresses. A name is replaced when
	// Hostnames is on, an IP address when IPs is on.
	Hosts []string
}

// Report counts replacements per kind. It never holds a matched value.
type Report struct {
	Counts map[Kind]int `json:"counts"`
}

// Total is the number of replacements.
func (r Report) Total() int {
	n := 0
	for _, c := range r.Counts {
		n += c
	}
	return n
}

// Strings returns the counts with string keys, for keel.log.
func (r Report) Strings() map[string]int {
	if len(r.Counts) == 0 {
		return nil
	}
	out := make(map[string]int, len(r.Counts))
	for k, v := range r.Counts {
		out[string(k)] = v
	}
	return out
}

// Kinds lists the kinds that matched, sorted.
func (r Report) Kinds() []Kind {
	out := make([]Kind, 0, len(r.Counts))
	for k := range r.Counts {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Safe is text that went through a Redactor (or is a constant written in
// Keel's source). Provider requests take Safe values only.
type Safe struct {
	text string
}

// String returns the redacted text.
func (s Safe) String() string { return s.text }

// Tail keeps at most max bytes from the end of s, starting at a line
// boundary when one is in reach. Cutting already-redacted text cannot
// expose a secret.
func (s Safe) Tail(max int) Safe {
	if max <= 0 || len(s.text) <= max {
		return s
	}
	t := s.text[len(s.text)-max:]
	if i := strings.IndexByte(t, '\n'); i >= 0 && i < len(t)-1 {
		t = t[i+1:]
	}
	return Safe{text: t}
}

// staticText is unexported so Static accepts only untyped string
// constants: a string variable cannot be converted to it outside this
// package.
type staticText string

// Static makes a Safe value from a string constant in Keel's own source,
// such as a system prompt. It cannot take a variable.
func Static(s staticText) Safe { return Safe{text: string(s)} }

// Redactor redacts the texts of one request with shared placeholders.
type Redactor struct {
	opts   Options
	known  []knownValue
	hosts  []knownValue
	seen   map[string]string
	next   map[category]int
	report Report
}

type knownValue struct {
	v        string
	bounded  bool // match only between non-word characters
	lower    bool // compare without case
	kind     Kind
	priority int
}

// New returns a Redactor for one request.
func New(opts Options) *Redactor {
	r := &Redactor{
		opts:   opts,
		seen:   map[string]string{},
		next:   map[category]int{},
		report: Report{Counts: map[Kind]int{}},
	}
	r.known = knownList(opts.Known)
	r.hosts = hostList(opts.Hosts, opts.IPs, opts.Hostnames)
	return r
}

// Text redacts s and returns it as Safe. Placeholders are shared with
// every other Text call on this Redactor.
func (r *Redactor) Text(s string) Safe {
	return Safe{text: r.redact(s)}
}

// Report returns the counts so far.
func (r *Redactor) Report() Report {
	out := Report{Counts: make(map[Kind]int, len(r.report.Counts))}
	for k, v := range r.report.Counts {
		out.Counts[k] = v
	}
	return out
}

// String redacts s with a one-off Redactor. Tests and callers that need
// no shared placeholders use it.
func String(s string, opts Options) (string, Report) {
	r := New(opts)
	out := r.Text(s)
	return out.String(), r.Report()
}

func (r *Redactor) placeholder(k Kind, value string) string {
	cat := k.category()
	key := string(cat) + "\x00" + value
	if cat == catHost {
		key = strings.ToLower(key)
	}
	if p, ok := r.seen[key]; ok {
		return p
	}
	r.next[cat]++
	p := "[" + string(cat) + ":" + strconv.Itoa(r.next[cat]) + "]"
	r.seen[key] = p
	return p
}

func (r *Redactor) redact(s string) string {
	s = Normalize(s)
	spans := r.find(s)
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	last := 0
	for _, sp := range spans {
		b.WriteString(s[last:sp.start])
		b.WriteString(r.placeholder(sp.kind, s[sp.start:sp.end]))
		r.report.Counts[sp.kind]++
		last = sp.end
	}
	b.WriteString(s[last:])
	return b.String()
}

type span struct {
	start, end int
	kind       Kind
	priority   int // lower wins: secrets, then ips, then hostnames
	order      int // detector order, breaks ties
}

const (
	prioSecret = 0
	prioIP     = 1
	prioHost   = 2
)

// find runs every detector and keeps a set of non-overlapping spans:
// secrets before IPs before hostnames, then the longest, then the first.
func (r *Redactor) find(s string) []span {
	var all []span
	for i, d := range secretDetectors {
		for _, sp := range d(s) {
			sp.priority = prioSecret
			sp.order = i
			all = append(all, sp)
		}
	}
	all = append(all, findKnown(s, r.known, len(secretDetectors))...)
	if r.opts.IPs {
		for _, sp := range findIPs(s) {
			sp.priority = prioIP
			all = append(all, sp)
		}
	}
	if r.opts.Hostnames {
		for _, sp := range findHostnames(s) {
			sp.priority = prioHost
			all = append(all, sp)
		}
	}
	all = append(all, findKnown(s, r.hosts, len(secretDetectors)+1)...)
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		if la, lb := a.end-a.start, b.end-b.start; la != lb {
			return la > lb
		}
		if a.start != b.start {
			return a.start < b.start
		}
		return a.order < b.order
	})
	var kept []span
	for _, sp := range all {
		if sp.end <= sp.start {
			continue
		}
		overlap := false
		for _, k := range kept {
			if sp.start < k.end && k.start < sp.end {
				overlap = true
				break
			}
		}
		if !overlap {
			kept = append(kept, sp)
		}
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].start < kept[j].start })
	return kept
}

// knownList prepares the vault's values for exact matching. A multi-line
// value (a private key) is also matched line by line, so a partial print
// of it is caught. Values shorter than 3 characters are skipped: they would
// replace ordinary words and letters everywhere (a documented gap); 3–5
// character values match only as whole words.
func knownList(values []string) []knownValue {
	var out []knownValue
	add := func(v string) {
		v = strings.TrimSpace(strings.ReplaceAll(v, "\r\n", "\n"))
		n := len(v)
		switch {
		case n < 3:
			return
		case n < 6:
			out = append(out, knownValue{v: v, bounded: true, kind: KindKnownSecret, priority: prioSecret})
		default:
			out = append(out, knownValue{v: v, kind: KindKnownSecret, priority: prioSecret})
		}
	}
	for _, v := range values {
		add(v)
		// The same value as text that went through whitespace or
		// invisible-character cleaning (a note is one line with single
		// spaces, store.CleanAgentNote): a vault value with a tab, two
		// spaces, a zero-width or a bidi character must still match.
		for _, w := range normalizedForms(v) {
			if w != v {
				add(w)
			}
		}
		if strings.Contains(v, "\n") {
			for _, line := range strings.Split(v, "\n") {
				if len(strings.TrimSpace(line)) >= 16 {
					add(line)
				}
			}
		}
	}
	// Longest first so a whole key wins over one of its lines.
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].v) > len(out[j].v) })
	return out
}

// normalizedForms is v with control, separator and format characters
// turned into spaces and whitespace runs collapsed (as notes are cleaned),
// and v with format characters removed and whitespace collapsed.
func normalizedForms(v string) []string {
	spaced := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, v)
	stripped := strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Cf, r) {
			return -1
		}
		return r
	}, v)
	return []string{strings.Join(strings.Fields(spaced), " "), strings.Join(strings.Fields(stripped), " ")}
}

func hostList(addrs []string, ips, names bool) []knownValue {
	var out []knownValue
	seen := map[string]bool{}
	for _, a := range addrs {
		a = strings.TrimSpace(strings.ToLower(a))
		a = strings.TrimSuffix(strings.TrimPrefix(a, "["), "]")
		if len(a) < 3 || seen[a] || a == "localhost" || isLoopbackOrUnspecified(a) {
			continue
		}
		seen[a] = true
		if isIP(a) {
			if ips {
				out = append(out, knownValue{v: a, bounded: true, lower: true, kind: KindIP, priority: prioIP})
			}
			continue
		}
		if names {
			out = append(out, knownValue{v: a, bounded: true, lower: true, kind: KindHostname, priority: prioHost})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].v) > len(out[j].v) })
	return out
}

func findKnown(s string, list []knownValue, order int) []span {
	if len(list) == 0 {
		return nil
	}
	var out []span
	hay := s
	lowered := ""
	for _, k := range list {
		h := hay
		if k.lower {
			if lowered == "" {
				lowered = strings.ToLower(s)
			}
			h = lowered
		}
		from := 0
		for {
			i := strings.Index(h[from:], k.v)
			if i < 0 {
				break
			}
			start := from + i
			end := start + len(k.v)
			from = start + 1
			if k.bounded && (isWordByte(h, start-1) || isWordByte(h, end)) {
				continue
			}
			out = append(out, span{start: start, end: end, kind: k.kind, priority: k.priority, order: order})
		}
	}
	return out
}

func isWordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
