package agent

import (
	"path"
	"regexp"
	"strings"
)

// DirectSafe reports whether a command line may be typed without the
// approve dialog (docs/ui-redesign.md §6 rule 13). It is not Classify: it
// is a handful of exact grammars, and anything that does not match one
// exactly goes to the dialog. Classify must also say read-only.
//
// The line is printable ASCII words separated by single spaces: no
// quotes, no pipes, no other commands, no other flags.
//
//	pwd | whoami | id | date | uptime | hostname | uname -a | df -h |
//	free -h | ps aux | docker ps | docker ps -a
//	ls [-l | -la | -lah] [PATH]
//	kubectl get RESOURCE [-n NAME | -A] [-o wide]
//	systemctl status UNIT --no-pager
func DirectSafe(line string) bool {
	if line == "" || len(line) > 256 || Classify(line) != RiskReadOnly {
		return false
	}
	for i := 0; i < len(line); i++ {
		if line[i] < 0x21 && line[i] != ' ' || line[i] > 0x7e {
			return false
		}
	}
	w := strings.Split(line, " ")
	for _, x := range w {
		if x == "" {
			return false
		}
	}
	if directExact[line] {
		return true
	}
	switch w[0] {
	case "ls":
		return directLs(w[1:])
	case "kubectl":
		return directKubectl(w[1:])
	case "systemctl":
		return len(w) == 4 && w[1] == "status" && unitRe.MatchString(w[2]) && w[3] == "--no-pager"
	}
	return false
}

var directExact = set("pwd", "whoami", "id", "date", "uptime", "hostname", "uname -a", "df -h", "free -h", "ps aux", "docker ps", "docker ps -a")

var (
	unitRe = regexp.MustCompile(`^[A-Za-z0-9@._][A-Za-z0-9@._-]{0,127}$`)
	nsRe   = regexp.MustCompile(`^[a-z0-9-]{1,63}$`)
	pathRe = regexp.MustCompile(`^[A-Za-z0-9_./-]{1,128}$`)
)

var kubeResources = set("pods", "nodes", "svc", "services", "deployments", "ns", "namespaces", "ingress", "pvc", "events", "jobs", "cronjobs", "statefulsets", "daemonsets", "replicasets")

func directLs(w []string) bool {
	if len(w) > 0 && set("-l", "-la", "-lah")[w[0]] {
		w = w[1:]
	}
	switch len(w) {
	case 0:
		return true
	case 1:
		return safePath(w[0])
	}
	return false
}

// directKubectl: get RESOURCE, then at most one of -n NAME / -A and at
// most one -o wide, in any order.
func directKubectl(w []string) bool {
	if len(w) < 2 || w[0] != "get" || !kubeResources[w[1]] {
		return false
	}
	ns, wide := false, false
	for i := 2; i < len(w); i++ {
		switch {
		case (w[i] == "-n") && !ns && i+1 < len(w) && nsRe.MatchString(w[i+1]):
			ns = true
			i++
		case w[i] == "-A" && !ns:
			ns = true
		case w[i] == "-o" && !wide && i+1 < len(w) && w[i+1] == "wide":
			wide = true
			i++
		default:
			return false
		}
	}
	return true
}

// safePath is one plain path: no "." or ".." segment, no "//", not a
// flag, not a home directory or under one, and, cleaned and lower-cased
// (a case-insensitive file system folds names), naming no secret.
func safePath(p string) bool {
	if !pathRe.MatchString(p) || strings.HasPrefix(p, "-") || strings.Contains(p, "//") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	c := strings.ToLower(path.Clean(p))
	for _, h := range homeDirs {
		if c == h || strings.HasPrefix(c, h+"/") {
			return false
		}
	}
	return !secretWord(c)
}

// homeDirs hold home directories (lower-cased): a path there reaches dot
// files, so it is never typed with one click.
var homeDirs = []string{"/root", "/home", "/users", "/var/root", "/private/var/root"}

// secretPaths are the places that hold keys and tokens.
var secretPaths = []string{"/proc", "environ", "/.ssh", "/.aws", "/.kube", "/.gnupg", "/.config/gh", "/.docker", "/.netrc", "/.git-credentials", "/etc/shadow", "/etc/gshadow", "/etc/sudoers"}

// secretNames are parts of a name that suggest a secret.
var secretNames = []string{"id_", ".pem", ".key", ".p12", ".pfx", "credential", "token", "secret", "password", ".env", "kubeconfig", "shadow", "_history", ".pgpass", ".npmrc", ".pypirc", ".my.cnf", ".ssh"}

// secretWord reports whether a (cleaned, lower-cased) path names a
// secret's place or file.
func secretWord(p string) bool {
	l := "/" + p
	for _, s := range secretPaths {
		if strings.Contains(l, s) {
			return true
		}
	}
	for _, s := range secretNames {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}
