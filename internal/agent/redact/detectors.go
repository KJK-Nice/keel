package redact

import (
	"math"
	"net/netip"
	"regexp"
	"strings"
	"unicode"
)

type detector func(s string) []span

// secretDetectors always run. Their order breaks ties between spans of the
// same length at the same place (a more specific kind first).
var secretDetectors = []detector{
	whole(KindPrivateKey, privateKeyRe),
	findOrphanKeyTail,
	whole(KindPrivateKey, ageKeyRe),
	whole(KindAnthropicKey, anthropicRe),
	whole(KindOpenAIKey, openaiRe),
	whole(KindAWSKey, awsKeyRe),
	whole(KindGoogleKey, googleRe),
	whole(KindGitHubToken, githubRe),
	whole(KindGitLabToken, gitlabRe),
	whole(KindSlackToken, slackRe),
	whole(KindStripeKey, stripeRe),
	whole(KindOtherToken, otherTokenRe),
	whole(KindJWT, jwtRe),
	group(KindAuthHeader, authHeaderRe, 1),
	group(KindAuthHeader, bearerRe, 1),
	group(KindAuthHeader, xHeaderRe, 2),
	group(KindAuthHeader, cookieRe, 1),
	group(KindURLUserinfo, urlPassRe, 1),
	group(KindURLUserinfo, urlTokenRe, 1),
	group(KindCLIFlag, cliFlagRe, 1),
	group(KindCLIFlag, mysqlPassRe, 1),
	group(KindCLIFlag, sshpassRe, 1),
	group(KindCLIFlag, curlUserRe, 1),
	findEnvLines,
	group(KindPasswordPrompt, promptRe, 1),
	findAssignments,
	findHighEntropy,
}

var (
	// A key block up to its END line, or to the end of the text when the
	// END line is not in view (scrollback cut it, or it is still printing).
	privateKeyRe = regexp.MustCompile(`(?s)-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----.*?(?:-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----|\z)`)
	ageKeyRe     = regexp.MustCompile(`AGE-SECRET-KEY-1[0-9A-Z]{20,}`)

	anthropicRe = regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}`)
	openaiRe    = regexp.MustCompile(`\bsk-(?:proj-|svcacct-|admin-|None-)?[A-Za-z0-9_-]{20,}`)
	awsKeyRe    = regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ABIA|ACCA)[0-9A-Z]{16}\b`)
	// AIza… is the Google API key format (Gemini, Maps, GCP); ya29. is an
	// OAuth access token; GOCSPX- an OAuth client secret; 1//0 a refresh token.
	googleRe     = regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}|\bya29\.[0-9A-Za-z_-]{20,}|\bGOCSPX-[0-9A-Za-z_-]{20,}|\b1//0[0-9A-Za-z_-]{30,}`)
	githubRe     = regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,255}\b|\bgithub_pat_[A-Za-z0-9_]{22,255}`)
	gitlabRe     = regexp.MustCompile(`\bglpat-[0-9A-Za-z_-]{20,}`)
	slackRe      = regexp.MustCompile(`\bxox[abposre]-[0-9A-Za-z-]{10,}|\bxapp-\d-[0-9A-Za-z-]{10,}|https://hooks\.slack\.com/services/[A-Za-z0-9/_-]+`)
	stripeRe     = regexp.MustCompile(`\b(?:sk|rk)_(?:live|test)_[0-9A-Za-z]{10,}|\bwhsec_[0-9A-Za-z]{20,}`)
	otherTokenRe = regexp.MustCompile(`\bnpm_[A-Za-z0-9]{36}\b|\bhf_[A-Za-z0-9]{30,}\b|\bSG\.[A-Za-z0-9_-]{16,}\.[A-Za-z0-9_-]{16,}|\bdo[por]_v1_[a-f0-9]{64}\b`)
	jwtRe        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)

	authHeaderRe = regexp.MustCompile(`(?i)\b(?:proxy-)?authorization[ \t]*:[ \t]*(?:(?:bearer|basic|token|digest|negotiate|ntlm|apikey)[ \t]+)?([^\s].*)`)
	bearerRe     = regexp.MustCompile(`(?i)\bbearer[ \t]+([A-Za-z0-9._~+/=-]{8,})`)
	xHeaderRe    = regexp.MustCompile(`(?i)\b(x-[a-z0-9-]*(?:key|token|secret|auth|signature)[a-z0-9-]*)[ \t]*:[ \t]*(\S+)`)
	cookieRe     = regexp.MustCompile(`(?i)\b(?:set-)?cookie[ \t]*:[ \t]*([^\s].*)`)

	urlPassRe  = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]{1,20}://[^\s/@:\[]+:([^\s/@]+)@`)
	urlTokenRe = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9+.-]{1,20}://([^\s/@:]{20,})@`)

	cliFlagRe   = regexp.MustCompile(`(?i)(?:^|\s)--?(?:password|passwd|pass|passphrase|token|secret|api-?key|access-key|secret-key|auth-token|client-secret)[ \t]+([^\s-]\S*)`)
	mysqlPassRe = regexp.MustCompile(`\b(?:mysql|mysqladmin|mysqldump|mariadb)\b[^\n]*?\s-p([^\s-]\S*)`)
	sshpassRe   = regexp.MustCompile(`\bsshpass\s+-p\s*(\S+)`)
	curlUserRe  = regexp.MustCompile(`(?:^|\s)(?:-u|--user)\s+[^\s:]+:(\S+)`)

	// "Password:", "[sudo] password for u:", "Enter passphrase for key
	// '/x':" followed by text on the same line (echoed input, or a
	// program that prints it).
	promptRe = regexp.MustCompile(`(?i)\b(?:password|passphrase|passcode)(?: for [^:\n]{1,80})?:[ \t]*([^\s].*)`)

	// .env and `env` output: an upper-case NAME=value line. The value
	// runs to the end of the line, as dotenv reads it.
	envLineRe = regexp.MustCompile(`(?m)^[ \t]*(?:export[ \t]+)?([A-Z_][A-Z0-9_]*)=(.*)$`)
	assignRe  = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.-])["']?(-{0,2}[A-Za-z_][A-Za-z0-9_.-]{0,63})["']?[ \t]*[:=][ \t]*("(?:[^"\\\n]|\\.)*"|'[^'\n]*'|[^\s"',;&)}\]]+)`)

	entropyCandRe = regexp.MustCompile(`[A-Za-z0-9+/=_-]{24,}`)
)

var keyEndRe = regexp.MustCompile(`-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`)
var keyBeginRe = regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`)

// findOrphanKeyTail covers an excerpt that starts inside a key block: the
// BEGIN line was cut off, so everything up to the first END line is key.
func findOrphanKeyTail(s string) []span {
	end := keyEndRe.FindStringIndex(s)
	if end == nil {
		return nil
	}
	if b := keyBeginRe.FindStringIndex(s); b != nil && b[0] < end[0] {
		return nil
	}
	return []span{{start: 0, end: end[1], kind: KindPrivateKey}}
}

func whole(k Kind, re *regexp.Regexp) detector {
	return func(s string) []span {
		var out []span
		for _, m := range re.FindAllStringIndex(s, -1) {
			out = append(out, span{start: m[0], end: m[1], kind: k})
		}
		return out
	}
}

func group(k Kind, re *regexp.Regexp, g int) detector {
	return func(s string) []span {
		var out []span
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			st, en := m[2*g], m[2*g+1]
			if st < 0 {
				continue
			}
			st, en = trimValue(s, st, en)
			if skipValue(s[st:en]) {
				continue
			}
			out = append(out, span{start: st, end: en, kind: k})
		}
		return out
	}
}

// trimValue drops surrounding quotes and trailing spaces, so a quoted value
// keeps its quotes and only the inside is replaced.
func trimValue(s string, st, en int) (int, int) {
	for en > st && (s[en-1] == ' ' || s[en-1] == '\t') {
		en--
	}
	if en-st >= 2 {
		q := s[st]
		if (q == '"' || q == '\'') && s[en-1] == q {
			st++
			en--
		}
	}
	return st, en
}

// skipValue keeps values that carry no secret: empty, booleans, nulls,
// masks, an existing placeholder, and short numbers (counts, ports).
func skipValue(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	switch strings.ToLower(v) {
	case "true", "false", "null", "nil", "none", "yes", "no", "on", "off", "undefined", "[redacted]", "<redacted>", "redacted":
		return true
	}
	if strings.Trim(v, "*•x") == "" {
		return true
	}
	if strings.HasPrefix(v, "[secret:") || strings.HasPrefix(v, "${") || strings.HasPrefix(v, "$(") {
		return true
	}
	if len(v) <= 4 && strings.Trim(v, "0123456789") == "" {
		return true
	}
	return false
}

// keyWords splits an identifier into lower-case words at _ . - and at
// camelCase boundaries: "dbPassword" → db, password; "API_KEY" → api, key.
func keyWords(key string) []string {
	key = strings.TrimLeft(key, "-")
	var words []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(key)
	for i, r := range rs {
		if r == '_' || r == '.' || r == '-' {
			flush()
			continue
		}
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(rs[i-1]) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

var secretWords = map[string]bool{
	"password": true, "passwd": true, "pass": true, "passphrase": true, "pwd": true,
	"secret": true, "secrets": true, "token": true, "apikey": true, "credential": true,
	"credentials": true, "privatekey": true, "accesskey": true, "secretkey": true,
	"clientsecret": true, "authtoken": true, "sessionid": true, "cookie": true,
}

// keyQualifiers make "key" a secret word when they come right before it
// (api key, access key, …). A plain "key" is a secret only in an env var.
var keyQualifiers = map[string]bool{
	"api": true, "access": true, "private": true, "secret": true, "signing": true,
	"encryption": true, "master": true, "auth": true, "client": true, "license": true,
}

// sensitiveKey reports whether a key name holds a secret. env is true for
// NAME=value lines, where any word "key" counts (STRIPE_KEY). PWD and
// OLDPWD are the working directory, not a password.
func sensitiveKey(key string, env bool) bool {
	up := strings.ToUpper(strings.TrimLeft(key, "-"))
	if up == "PWD" || up == "OLDPWD" {
		return false
	}
	words := keyWords(key)
	for i, w := range words {
		if secretWords[w] {
			return true
		}
		if w == "key" && (env || i > 0 && keyQualifiers[words[i-1]]) {
			return true
		}
		if strings.HasSuffix(w, "password") || strings.HasSuffix(w, "secret") || strings.HasSuffix(w, "token") || strings.HasSuffix(w, "apikey") {
			return true
		}
	}
	return false
}

func findEnvLines(s string) []span {
	var out []span
	for _, m := range envLineRe.FindAllStringSubmatchIndex(s, -1) {
		if !sensitiveKey(s[m[2]:m[3]], true) {
			continue
		}
		st, en := trimValue(s, m[4], m[5])
		if skipValue(s[st:en]) {
			continue
		}
		out = append(out, span{start: st, end: en, kind: KindEnvLine})
	}
	return out
}

// findAssignments walks key=value / key: value pairs. After a pair whose
// key is not sensitive it resumes at that value, not after it, so
// "mysql://x?password=y" still finds password=y inside the first value.
func findAssignments(s string) []span {
	var out []span
	pos := 0
	for pos < len(s) {
		m := assignRe.FindStringSubmatchIndex(s[pos:])
		if m == nil {
			break
		}
		for i := range m {
			if m[i] >= 0 {
				m[i] += pos
			}
		}
		if !sensitiveKey(s[m[2]:m[3]], false) {
			pos = m[4]
			continue
		}
		pos = m[5]
		st, en := trimValue(s, m[4], m[5])
		if skipValue(s[st:en]) {
			continue
		}
		out = append(out, span{start: st, end: en, kind: KindAssignment})
	}
	return out
}

// findHighEntropy is the backstop for secrets with no known shape. It is
// tuned so ordinary terminal text survives:
//   - at least 24 characters from the base64/base64url alphabet;
//   - hex-only runs (git SHAs, sha256 digests, docker ids, UUIDs) are kept:
//     they are identifiers far more often than secrets, and the model
//     needs them ("git show 3f2a…");
//   - paths (/ with short segments) are kept;
//   - upper case, lower case and digits must all appear, digits in at
//     least two separate runs (random text scatters them; identifiers like
//     sha256Checksum2 keep them together);
//   - Shannon entropy at least 3.7 bits per character under 32 characters,
//     4.0 from 32 on.
//
// Known false positives: long base64 blobs that are not secret (public
// certificates, SSH public keys, SRI hashes, go.sum h1: lines). Known false
// negatives: pure-hex secrets and secrets under 24 characters with no
// recognized key name, prefix or vault match.
func findHighEntropy(s string) []span {
	var out []span
	for _, m := range entropyCandRe.FindAllStringIndex(s, -1) {
		if looksRandom(s[m[0]:m[1]]) {
			out = append(out, span{start: m[0], end: m[1], kind: KindHighEntropy})
		}
	}
	return out
}

func looksRandom(tok string) bool {
	t := strings.TrimRight(tok, "=")
	if len(t) < 24 {
		return false
	}
	if strings.Trim(t, "0123456789abcdefABCDEF-") == "" {
		return false
	}
	if strings.Contains(t, "/") {
		longest := 0
		for _, seg := range strings.Split(t, "/") {
			if len(seg) > longest {
				longest = len(seg)
			}
		}
		if longest < 16 {
			return false
		}
	}
	var upper, lower, digitRuns int
	inDigits := false
	freq := map[rune]int{}
	for _, r := range t {
		freq[r]++
		isDigit := r >= '0' && r <= '9'
		switch {
		case r >= 'A' && r <= 'Z':
			upper++
		case r >= 'a' && r <= 'z':
			lower++
		}
		if isDigit && !inDigits {
			digitRuns++
		}
		inDigits = isDigit
	}
	if upper == 0 || lower == 0 || digitRuns < 2 {
		return false
	}
	h := 0.0
	n := float64(len(t))
	for _, c := range freq {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	min := 4.0
	if len(t) < 32 {
		min = 3.7
	}
	return h >= min
}

var (
	ipv4Re     = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\.){3}(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)\b`)
	ipv6CandRe = regexp.MustCompile(`[0-9A-Fa-f]{0,4}(?::[0-9A-Fa-f]{0,4}){2,7}(?:(?::|\.)(?:\d{1,3}\.){0,3}\d{1,3})?`)
)

// findIPs finds IPv4 and IPv6 addresses. Loopback and unspecified
// addresses (127.0.0.1, ::1, 0.0.0.0, ::) and netmasks starting 255. are
// kept: they identify nothing. An IPv4 inside a longer dotted number
// (1.2.3.4.5, a version) is kept.
func findIPs(s string) []span {
	var out []span
	for _, m := range ipv4Re.FindAllStringIndex(s, -1) {
		if m[0] > 0 && s[m[0]-1] == '.' || m[1] < len(s) && s[m[1]] == '.' && m[1]+1 < len(s) && s[m[1]+1] >= '0' && s[m[1]+1] <= '9' {
			continue
		}
		v := s[m[0]:m[1]]
		if isLoopbackOrUnspecified(v) || strings.HasPrefix(v, "255.") {
			continue
		}
		out = append(out, span{start: m[0], end: m[1], kind: KindIP})
	}
	for _, m := range ipv6CandRe.FindAllStringIndex(s, -1) {
		v := s[m[0]:m[1]]
		if strings.Count(v, ":") < 2 || len(v) < 6 {
			continue
		}
		if m[0] > 0 && isWordByte(s, m[0]-1) || isWordByte(s, m[1]) {
			continue
		}
		ip, err := netip.ParseAddr(v)
		if err != nil || !ip.Is6() || ip.IsLoopback() || ip.IsUnspecified() {
			continue
		}
		out = append(out, span{start: m[0], end: m[1], kind: KindIP})
	}
	return out
}

func isIP(s string) bool {
	_, err := netip.ParseAddr(s)
	return err == nil
}

func isLoopbackOrUnspecified(s string) bool {
	ip, err := netip.ParseAddr(s)
	return err == nil && (ip.Unmap().IsLoopback() || ip.Unmap().IsUnspecified())
}

var (
	// Domain names whose last label is a common TLD. File names whose
	// extension is also a TLD (.sh .py .md .rs .pl .so) are kept out on
	// purpose: install.sh is not a host.
	fqdnRe = regexp.MustCompile(`(?i)\b(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:com|net|org|io|dev|app|ai|co|cloud|internal|local|lan|corp|home|arpa|edu|gov|mil|biz|info|xyz|tech|site|online|us|uk|de|fr|jp|cn|nl|eu|ca|au|in|br|ch|se|no|es|it|be|at|dk|fi|ie|nz|sg|kr|tw|hk|za|mx)\b`)
	// user@host in prompts, scp targets and e-mail addresses.
	userHostRe = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_.-]*@([A-Za-z0-9][A-Za-z0-9-]*(?:\.[A-Za-z0-9-]+)*)`)
)

func findHostnames(s string) []span {
	var out []span
	for _, m := range fqdnRe.FindAllStringIndex(s, -1) {
		out = append(out, span{start: m[0], end: m[1], kind: KindHostname})
	}
	for _, m := range userHostRe.FindAllStringSubmatchIndex(s, -1) {
		v := s[m[2]:m[3]]
		// react@18.3.1 is a version, not a host.
		if strings.EqualFold(v, "localhost") || isIP(v) || strings.Trim(v, "0123456789.") == "" {
			continue
		}
		out = append(out, span{start: m[2], end: m[3], kind: KindHostname})
	}
	return out
}
