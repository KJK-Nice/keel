// Package sshconfig reads an OpenSSH client config (~/.ssh/config) for the
// first-run host import. It only reads: it never writes the config back,
// never runs a command (ProxyCommand, Match exec, LocalCommand) and never
// expands environment variables. The files are treated as untrusted input:
// every file, line and result is size-limited, only regular files are
// opened, and Include is bounded in depth, count and cycles.
//
// Supported: Host, HostName, User, Port, IdentityFile, Include, plus
// ProxyCommand, which is only reported, and ProxyJump, which the import
// maps to a Keel jump host when it is one hop to a host that is imported
// too or already saved (ParseJump; internal/httpapi/sshimport.go). Match
// blocks are skipped with a note. Everything else is
// ignored, as ssh would apply it and Keel has no use for it.
package sshconfig

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Limits. A real config is a few KB; these only stop a hostile or broken
// file from eating memory or time.
const (
	DefaultMaxFileBytes = 256 << 10
	DefaultMaxFiles     = 64
	DefaultMaxDepth     = 8
	DefaultMaxHosts     = 1000
	MaxLineBytes        = 4096
	maxNotes            = 200
	maxPatterns         = 500
)

type Options struct {
	// SSHDir is ~/.ssh (KEEL_SSH_DIR overrides it in keel serve). The config
	// is SSHDir/config and a relative Include is relative to it.
	SSHDir string
	// Home is what ~ and %d stand for. Empty means the parent of SSHDir, so
	// a test SSHDir never lets ~ reach the real home directory.
	Home string
	// LocalUser is the user for hosts without User, as ssh would use.
	LocalUser string

	MaxFileBytes int64
	MaxFiles     int
	MaxDepth     int
	MaxHosts     int
}

// Host is one concrete name from a Host line, with every option that
// applies to it resolved the way ssh does (first value wins, IdentityFile
// adds up), including from wildcard blocks such as "Host *".
type Host struct {
	Alias    string `json:"alias"`
	HostName string `json:"hostName"`
	User     string `json:"user"`
	Port     int    `json:"port"`
	// IdentityFiles are absolute paths, in config order.
	IdentityFiles []string `json:"identityFiles,omitempty"`
	// Proxy is "ProxyJump" or "ProxyCommand" when the host needs one. A
	// ProxyCommand is never used and its command is never returned.
	// ProxyJump keeps its target; the import uses it when it can.
	Proxy     string   `json:"proxy,omitempty"`
	ProxyJump string   `json:"proxyJump,omitempty"`
	Source    string   `json:"source"`
	Notes     []string `json:"notes,omitempty"`
}

// Pattern is a Host name with a wildcard (*, ?) or a negation (!). It
// matches other names; it is not a host Keel can connect to.
type Pattern struct {
	Pattern string `json:"pattern"`
	Source  string `json:"source"`
}

type Note struct {
	Source string `json:"source"`
	Text   string `json:"text"`
}

type Result struct {
	// Path is the config path for display (~ for Home).
	Path     string    `json:"path"`
	Found    bool      `json:"found"`
	Hosts    []Host    `json:"hosts"`
	Patterns []Pattern `json:"patterns"`
	Notes    []Note    `json:"notes"`
}

type option struct {
	key    string // lower case
	args   []string
	source string
}

type block struct {
	patterns []string // lower case; "!x" negates
	match    bool
	opts     []option
}

type parser struct {
	opts    Options
	res     Result
	blocks  []*block
	aliases []string
	seen    map[string]string // alias (lower) -> source
	files   int
	stack   []string
	limited bool
}

// Parse reads SSHDir/config and every file it includes. A missing config
// is not an error: Found is false.
func Parse(opts Options) (Result, error) {
	if opts.SSHDir == "" {
		return Result{}, fmt.Errorf("sshconfig: SSHDir is required")
	}
	sshDir, err := filepath.Abs(opts.SSHDir)
	if err != nil {
		return Result{}, err
	}
	opts.SSHDir = sshDir
	if opts.Home == "" {
		opts.Home = filepath.Dir(sshDir)
	}
	if opts.MaxFileBytes <= 0 {
		opts.MaxFileBytes = DefaultMaxFileBytes
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = DefaultMaxFiles
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = DefaultMaxDepth
	}
	if opts.MaxHosts <= 0 {
		opts.MaxHosts = DefaultMaxHosts
	}
	p := &parser{opts: opts, seen: map[string]string{}}
	path := filepath.Join(sshDir, "config")
	p.res.Path = p.display(path)
	p.res.Hosts = []Host{}
	p.res.Patterns = []Pattern{}
	p.res.Notes = []Note{}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return p.res, nil
		}
		return p.res, err
	}
	p.res.Found = true
	// Options before the first Host line apply to every host.
	p.blocks = append(p.blocks, &block{patterns: []string{"*"}})
	p.readFile(path, 0, "")
	p.resolve()
	return p.res, nil
}

// display shortens a path under Home to ~/...
func (p *parser) display(path string) string {
	home := filepath.Clean(p.opts.Home)
	if path == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

func (p *parser) note(source, format string, args ...any) {
	if len(p.res.Notes) >= maxNotes {
		return
	}
	p.res.Notes = append(p.res.Notes, Note{Source: source, Text: fmt.Sprintf(format, args...)})
}

// readFile parses one file. from is the Include line that named it.
func (p *parser) readFile(path string, depth int, from string) {
	where := from
	if where == "" {
		where = p.display(path)
	}
	if depth > p.opts.MaxDepth {
		p.note(where, "Include nested deeper than %d levels; %s skipped", p.opts.MaxDepth, p.display(path))
		return
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		p.note(where, "cannot read %s", p.display(path))
		return
	}
	for _, f := range p.stack {
		if f == real {
			p.note(where, "Include loop: %s includes itself; skipped", p.display(path))
			return
		}
	}
	if p.files >= p.opts.MaxFiles {
		p.note(where, "more than %d config files; %s skipped", p.opts.MaxFiles, p.display(path))
		return
	}
	data, ok := p.readLimited(real, where, path)
	if !ok {
		return
	}
	p.files++
	p.stack = append(p.stack, real)
	defer func() { p.stack = p.stack[:len(p.stack)-1] }()

	name := p.display(path)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), int(p.opts.MaxFileBytes)+1)
	n := 0
	inMatch := false
	for sc.Scan() {
		n++
		src := fmt.Sprintf("%s:%d", name, n)
		line := strings.TrimSuffix(sc.Text(), "\r")
		if len(line) > MaxLineBytes {
			p.note(src, "line longer than %d bytes; skipped", MaxLineBytes)
			continue
		}
		key, args, err := splitLine(line)
		if err != nil {
			p.note(src, "%v; line skipped", err)
			continue
		}
		if key == "" {
			continue
		}
		switch key {
		case "host":
			inMatch = false
			if len(args) == 0 {
				p.note(src, "Host without a name; skipped")
				p.blocks = append(p.blocks, &block{}) // matches nothing
				continue
			}
			p.hostLine(args, src)
			continue
		case "match":
			inMatch = true
			p.note(src, "Match block skipped: Keel imports Host blocks only")
			p.blocks = append(p.blocks, &block{match: true})
			continue
		case "include":
			if inMatch {
				continue
			}
			if len(args) == 0 {
				p.note(src, "Include without a file; skipped")
				continue
			}
			for _, a := range args {
				p.include(a, depth, src)
			}
			continue
		}
		if inMatch {
			continue
		}
		switch key {
		case "hostname", "user", "port", "identityfile", "proxycommand", "proxyjump":
			if len(args) == 0 {
				p.note(src, "%s without a value; skipped", canonical(key))
				continue
			}
			cur := p.blocks[len(p.blocks)-1]
			cur.opts = append(cur.opts, option{key: key, args: args, source: src})
		}
	}
	if err := sc.Err(); err != nil {
		p.note(name, "cannot read the rest of the file: %v", err)
	}
}

// readLimited reads a regular file of at most MaxFileBytes.
func (p *parser) readLimited(real, where, path string) ([]byte, bool) {
	fi, err := os.Stat(real)
	if err != nil {
		p.note(where, "cannot read %s", p.display(path))
		return nil, false
	}
	if !fi.Mode().IsRegular() {
		p.note(where, "%s is not a regular file; skipped", p.display(path))
		return nil, false
	}
	if fi.Size() > p.opts.MaxFileBytes {
		p.note(where, "%s is larger than %d KiB; skipped", p.display(path), p.opts.MaxFileBytes>>10)
		return nil, false
	}
	data, err := readFileLimited(real, p.opts.MaxFileBytes)
	if err != nil {
		p.note(where, "cannot read %s: %v", p.display(path), err)
		return nil, false
	}
	return data, true
}

func (p *parser) include(arg string, depth int, src string) {
	pat, ok := p.expandPath(arg, "", src, false)
	if !ok {
		p.note(src, "Include %s uses tokens Keel does not expand; skipped", arg)
		return
	}
	if !filepath.IsAbs(pat) {
		pat = filepath.Join(p.opts.SSHDir, pat)
	}
	matches, err := filepath.Glob(pat)
	if err != nil {
		p.note(src, "Include %s is not a valid pattern; skipped", arg)
		return
	}
	if len(matches) == 0 {
		p.note(src, "Include %s matched no file", arg)
		return
	}
	sort.Strings(matches)
	for _, m := range matches {
		p.readFile(m, depth+1, src)
	}
}

func (p *parser) hostLine(names []string, src string) {
	b := &block{}
	for _, raw := range names {
		name := strings.ToLower(raw)
		b.patterns = append(b.patterns, name)
		if isPattern(raw) {
			if len(p.res.Patterns) < maxPatterns {
				p.res.Patterns = append(p.res.Patterns, Pattern{Pattern: raw, Source: src})
			}
			continue
		}
		if _, dup := p.seen[name]; dup {
			continue
		}
		if len(p.aliases) >= p.opts.MaxHosts {
			if !p.limited {
				p.limited = true
				p.note(src, "more than %d hosts; the rest are skipped", p.opts.MaxHosts)
			}
			continue
		}
		p.seen[name] = src
		p.aliases = append(p.aliases, raw)
	}
	p.blocks = append(p.blocks, b)
}

func isPattern(name string) bool {
	return strings.ContainsAny(name, "*?") || strings.HasPrefix(name, "!")
}

// resolve applies the blocks to every concrete alias, as ssh does.
func (p *parser) resolve() {
	for _, alias := range p.aliases {
		h := Host{Alias: alias, Source: p.seen[strings.ToLower(alias)]}
		var hostName, user, port, proxy string
		var gotProxy bool
		seenID := map[string]bool{}
		for _, b := range p.blocks {
			if b.match || !blockMatches(b.patterns, strings.ToLower(alias)) {
				continue
			}
			for _, o := range b.opts {
				val := o.args[0]
				switch o.key {
				case "hostname":
					if hostName == "" {
						hostName = p.expandHostName(val, alias, &h)
					}
				case "user":
					if user == "" {
						user = val
						if strings.ContainsAny(val, "%$") {
							h.Notes = append(h.Notes, fmt.Sprintf("User %s kept as written (tokens are not expanded)", val))
						}
					}
				case "port":
					if port == "" {
						port = val
						if n, err := strconv.Atoi(val); err == nil && n >= 1 && n <= 65535 {
							h.Port = n
						} else {
							h.Notes = append(h.Notes, fmt.Sprintf("Port %q is not a port number; 22 used", val))
						}
					}
				case "identityfile":
					if strings.EqualFold(val, "none") {
						continue
					}
					if path, ok := p.expandPath(val, alias, o.source, true); ok {
						if !filepath.IsAbs(path) {
							path = filepath.Join(p.opts.Home, path)
						}
						path = filepath.Clean(path)
						if !seenID[path] {
							seenID[path] = true
							h.IdentityFiles = append(h.IdentityFiles, path)
						}
					} else {
						h.Notes = append(h.Notes, fmt.Sprintf("IdentityFile %s uses tokens Keel does not expand; not offered", val))
					}
				case "proxycommand", "proxyjump":
					if gotProxy {
						continue
					}
					gotProxy = true
					if strings.EqualFold(val, "none") {
						continue
					}
					proxy = canonical(o.key)
					if o.key == "proxyjump" {
						h.ProxyJump = strings.Join(o.args, " ")
					}
				}
			}
		}
		if hostName == "" {
			hostName = alias
		}
		if h.Port == 0 {
			h.Port = 22
		}
		if user == "" {
			user = p.opts.LocalUser
		}
		h.HostName = hostName
		h.User = user
		h.Proxy = proxy
		switch proxy {
		case "ProxyCommand":
			h.Notes = append(h.Notes, "ProxyCommand is not supported; the host is added without it")
		case "ProxyJump":
			h.Notes = append(h.Notes, "ProxyJump "+h.ProxyJump+" is kept as the jump host when that host is added too or already saved (one hop only); otherwise the host is added without it")
		}
		p.res.Hosts = append(p.res.Hosts, h)
	}
}

var tokenRE = regexp.MustCompile(`%.`)

// expandHostName replaces %h (the alias) and %%; any other token is kept
// as written, with a note.
func (p *parser) expandHostName(val, alias string, h *Host) string {
	kept := strings.Contains(val, "${")
	out := tokenRE.ReplaceAllStringFunc(val, func(t string) string {
		switch t {
		case "%h":
			return alias
		case "%%":
			return "%"
		}
		kept = true
		return t
	})
	if kept {
		h.Notes = append(h.Notes, fmt.Sprintf("HostName %s kept as written (only %%h is expanded)", val))
	}
	return out
}

var safeAlias = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// expandPath expands a leading ~ and the tokens %d (Home), %h (the alias,
// when it is a plain name) and %%. Any other token, or ${ENV}, makes the
// path unusable (ok false): Keel leaves it alone rather than guess.
func (p *parser) expandPath(val, alias, src string, allowHost bool) (string, bool) {
	if strings.Contains(val, "${") {
		p.note(src, "%s uses an environment variable; Keel does not expand those", val)
		return "", false
	}
	if val == "~" {
		val = p.opts.Home
	} else if strings.HasPrefix(val, "~/") {
		val = filepath.Join(p.opts.Home, val[2:])
	} else if strings.HasPrefix(val, "~") {
		p.note(src, "%s names another user's home; not followed", val)
		return "", false
	}
	ok := true
	out := tokenRE.ReplaceAllStringFunc(val, func(t string) string {
		switch t {
		case "%%":
			return "%"
		case "%d":
			return p.opts.Home
		case "%h":
			if allowHost && safeAlias.MatchString(alias) && !strings.Contains(alias, "..") {
				return alias
			}
		}
		ok = false
		return t
	})
	if !ok {
		return "", false
	}
	if strings.HasSuffix(out, "%") {
		return "", false
	}
	return out, true
}

// blockMatches is ssh's Host matching: a negated pattern that matches
// rules the block out; otherwise any positive match applies it.
func blockMatches(patterns []string, alias string) bool {
	hit := false
	for _, pat := range patterns {
		if strings.HasPrefix(pat, "!") {
			if globMatch(pat[1:], alias) {
				return false
			}
			continue
		}
		if globMatch(pat, alias) {
			hit = true
		}
	}
	return hit
}

// globMatch matches * (any run) and ? (one byte), nothing else.
func globMatch(pat, s string) bool {
	px, sx := 0, 0
	star, mark := -1, 0
	for sx < len(s) {
		switch {
		case px < len(pat) && (pat[px] == '?' || pat[px] == s[sx]):
			px++
			sx++
		case px < len(pat) && pat[px] == '*':
			star = px
			mark = sx
			px++
		case star >= 0:
			px = star + 1
			mark++
			sx = mark
		default:
			return false
		}
	}
	for px < len(pat) && pat[px] == '*' {
		px++
	}
	return px == len(pat)
}

// splitLine returns the lower-case keyword and its arguments. Blank lines
// and comments return an empty keyword. "Key value", "Key=value" and
// "Key = value" are all accepted; arguments may be double-quoted.
func splitLine(line string) (string, []string, error) {
	s := strings.TrimLeft(line, " \t")
	if s == "" || strings.HasPrefix(s, "#") {
		return "", nil, nil
	}
	i := strings.IndexAny(s, " \t=")
	if i < 0 {
		return strings.ToLower(s), nil, nil
	}
	key := strings.ToLower(s[:i])
	rest := strings.TrimLeft(s[i:], " \t")
	if strings.HasPrefix(rest, "=") {
		rest = strings.TrimLeft(rest[1:], " \t")
	}
	args, err := splitArgs(rest)
	if err != nil {
		return "", nil, err
	}
	if key == "" {
		return "", nil, fmt.Errorf("line has no keyword")
	}
	return key, args, nil
}

func splitArgs(s string) ([]string, error) {
	var out []string
	for {
		s = strings.TrimLeft(s, " \t")
		if s == "" || strings.HasPrefix(s, "#") {
			return out, nil
		}
		if s[0] == '"' {
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				return nil, fmt.Errorf("unterminated quote")
			}
			out = append(out, s[1:1+end])
			s = s[2+end:]
			continue
		}
		end := strings.IndexAny(s, " \t")
		if end < 0 {
			out = append(out, s)
			return out, nil
		}
		out = append(out, s[:end])
		s = s[end:]
	}
}

func canonical(key string) string {
	switch key {
	case "hostname":
		return "HostName"
	case "user":
		return "User"
	case "port":
		return "Port"
	case "identityfile":
		return "IdentityFile"
	case "proxycommand":
		return "ProxyCommand"
	case "proxyjump":
		return "ProxyJump"
	}
	return key
}

// Jump is one ProxyJump hop: [user@]host[:port]. Port 0 and User "" mean
// not given.
type Jump struct {
	User string
	Host string
	Port int
}

// ParseJump reads a ProxyJump value. It fails for more than one hop
// ("a,b"), an ssh:// URI and anything it cannot read.
func ParseJump(spec string) (Jump, bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.ContainsAny(spec, ", \t") || strings.Contains(spec, "://") {
		return Jump{}, false
	}
	var j Jump
	if i := strings.LastIndex(spec, "@"); i >= 0 {
		j.User = spec[:i]
		spec = spec[i+1:]
		if j.User == "" {
			return Jump{}, false
		}
	}
	host := spec
	if strings.HasPrefix(spec, "[") {
		end := strings.Index(spec, "]")
		if end < 0 {
			return Jump{}, false
		}
		host = spec[1:end]
		rest := spec[end+1:]
		if rest != "" {
			port, ok := jumpPort(strings.TrimPrefix(rest, ":"), strings.HasPrefix(rest, ":"))
			if !ok {
				return Jump{}, false
			}
			j.Port = port
		}
	} else if strings.Count(spec, ":") == 1 {
		i := strings.Index(spec, ":")
		host = spec[:i]
		port, ok := jumpPort(spec[i+1:], true)
		if !ok {
			return Jump{}, false
		}
		j.Port = port
	}
	if host == "" {
		return Jump{}, false
	}
	j.Host = host
	return j, true
}

func jumpPort(s string, ok bool) (int, bool) {
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > 65535 {
		return 0, false
	}
	return n, true
}
