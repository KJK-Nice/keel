package redact

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// Token-shaped test values are assembled at run time (j("gh", "p_", …)) so
// this file holds no literal that a secret scanner would flag.
func j(parts ...string) string { return strings.Join(parts, "") }

func rnd(seed uint64, n int, alphabet string) string {
	r := rand.New(rand.NewPCG(seed, seed*7+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = alphabet[r.IntN(len(alphabet))]
	}
	return string(b)
}

const (
	b62    = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b64url = b62 + "-_"
	hexLow = "0123456789abcdef"
)

var (
	ghToken    = j("gh", "p_", rnd(1, 36, b62))
	ghPAT      = j("github", "_pat_", rnd(2, 82, b62+"_"))
	awsID      = j("AK", "IA", rnd(3, 16, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567"))
	googleKey  = j("AI", "za", rnd(4, 35, b64url))
	slackTok   = j("xo", "xb-", rnd(5, 12, "0123456789"), "-", rnd(6, 24, b62))
	stripeKey  = j("sk", "_live_", rnd(7, 24, b62))
	openaiKey  = j("sk", "-proj-", rnd(8, 48, b64url))
	anthKey    = j("sk", "-ant-api03-", rnd(9, 80, b64url))
	gitlabTok  = j("gl", "pat-", rnd(10, 20, b64url))
	npmTok     = j("np", "m_", rnd(11, 36, b62))
	jwtTok     = j("ey", "JhbGciOiJIUzI1NiJ9.", "ey", "JzdWIiOiIxMjM0NTY3ODkwIn0.", rnd(12, 43, b64url))
	random40   = rnd(13, 40, b62)
	random28   = rnd(14, 28, b62+"+/")
	gitSHA     = rnd(15, 40, hexLow)
	sha256     = rnd(16, 64, hexLow)
	uuid       = rnd(17, 8, hexLow) + "-" + rnd(18, 4, hexLow) + "-4" + rnd(19, 3, hexLow) + "-a" + rnd(20, 3, hexLow) + "-" + rnd(21, 12, hexLow)
	pemBody    = rnd(22, 64, b62+"+/") + "\n" + rnd(23, 64, b62+"+/") + "\n" + rnd(24, 20, b62) + "=="
	pemBlock   = j("-----BEGIN ", "OPENSSH PRIVATE KEY-----\n", pemBody, "\n-----END ", "OPENSSH PRIVATE KEY-----")
	rsaPartial = j("-----BEGIN ", "RSA PRIVATE KEY-----\n", rnd(25, 64, b62), "\n", rnd(26, 64, b62))
)

type tcase struct {
	name  string
	in    string
	opts  Options
	want  string // exact output when set
	gone  []string
	kept  []string
	kinds []Kind
}

func cases() []tcase {
	return []tcase{
		// Private keys.
		{name: "pem block", in: "$ cat id\n" + pemBlock + "\n$ ", want: "$ cat id\n[secret:1]\n$ ", kinds: []Kind{KindPrivateKey}},
		{name: "pem block cut off by scrollback", in: rsaPartial, want: "[secret:1]", kinds: []Kind{KindPrivateKey}},
		{name: "pem inside json with literal \\n", in: `{"private_key": "` + j("-----BEGIN ", "PRIVATE KEY-----\\nMIIE", rnd(27, 40, b62), "\\n-----END ", "PRIVATE KEY-----\\n") + `"}`,
			gone: []string{"MIIE"}, kept: []string{`{"private_key": "`}},
		{name: "age secret key", in: "key: " + j("AGE-SECRET", "-KEY-1", rnd(28, 58, "ABCDEFGHJKLMNPQRSTUVWXYZ0123456789")), want: "key: [secret:1]"},
		{name: "excerpt starting inside a key", in: pemBody + "\n-----END OPENSSH PRIVATE KEY-----\n$ ls", want: "[secret:1]\n$ ls", kinds: []Kind{KindPrivateKey}},
		{name: "public key header is kept", in: "-----BEGIN PUBLIC KEY-----", want: "-----BEGIN PUBLIC KEY-----"},

		// Password prompts and assignments.
		{name: "echoed password after prompt", in: "Password: hunter2-echoed\n", want: "Password: [secret:1]\n", kinds: []Kind{KindPasswordPrompt}},
		{name: "sudo prompt without input", in: "[sudo] password for kit: \nok", want: "[sudo] password for kit: \nok"},
		{name: "passphrase prompt with typed text", in: "Enter passphrase for key '/home/u/.ssh/id_ed25519': opensesame", want: "Enter passphrase for key '/home/u/.ssh/id_ed25519': [secret:1]"},
		{name: "password=", in: "mysql://x?password=s3cr3t&user=a", want: "mysql://x?password=[secret:1]&user=a"},
		{name: "token:", in: "token: abcdef123456", want: "token: [secret:1]"},
		{name: "json api_key keeps quotes", in: `{"api_key": "k-123456", "name": "x"}`, want: `{"api_key": "[secret:1]", "name": "x"}`},
		{name: "yaml client_secret", in: "  client_secret: 'zz-top-9'\n", want: "  client_secret: '[secret:1]'\n"},
		{name: "camelCase dbPassword", in: "dbPassword=letmein99", want: "dbPassword=[secret:1]"},
		{name: "--secret=", in: "deploy --secret=abc123xyz", want: "deploy --secret=[secret:1]"},
		{name: "booleans and masks kept", in: "password: ****\nsecret=true\ntoken: null", want: "password: ****\nsecret=true\ntoken: null"},
		{name: "PWD is a directory", in: "PWD=/home/kit/src\nOLDPWD=/tmp", want: "PWD=/home/kit/src\nOLDPWD=/tmp"},
		{name: "plain key= is not a secret", in: `{"key": "name", "value": 3}`, want: `{"key": "name", "value": 3}`},
		{name: "short counts kept", in: "pass: 10 fail: 0", want: "pass: 10 fail: 0"},

		// CLI flags.
		{name: "--password value", in: "app --password hunter22 --verbose", want: "app --password [secret:1] --verbose", kinds: []Kind{KindCLIFlag}},
		{name: "mysql -p", in: "mysql -u root -pS3cretPw db", want: "mysql -u root -p[secret:1] db"},
		{name: "sshpass -p", in: "sshpass -p 'pw-123' ssh host", want: "sshpass -p '[secret:1]' ssh host"},
		{name: "curl -u", in: "curl -u admin:adm1nPw https://x", want: "curl -u admin:[secret:1] https://x"},

		// Headers.
		{name: "authorization bearer", in: "Authorization: Bearer abc.def.ghi-123\n", want: "Authorization: Bearer [secret:1]\n", kinds: []Kind{KindAuthHeader}},
		{name: "authorization basic", in: "> authorization: Basic dXNlcjpwYXNz", want: "> authorization: Basic [secret:1]"},
		{name: "curl -H bearer", in: `curl -H "Authorization: Bearer tok_1234567890" x`, gone: []string{"tok_1234567890"}},
		{name: "x-api-key header", in: "X-Api-Key: 0a1b2c3d4e", want: "X-Api-Key: [secret:1]"},
		{name: "cookie header", in: "Cookie: sid=abc; theme=dark", want: "Cookie: [secret:1]"},

		// Provider key formats.
		{name: "github token", in: "export X=1; echo " + ghToken, want: "export X=1; echo [secret:1]", kinds: []Kind{KindGitHubToken}},
		{name: "github fine-grained pat", in: ghPAT, want: "[secret:1]", kinds: []Kind{KindGitHubToken}},
		{name: "aws access key id", in: "aws_access_key_id " + awsID, want: "aws_access_key_id [secret:1]", kinds: []Kind{KindAWSKey}},
		{name: "google / gemini key", in: "key " + googleKey + " ok", want: "key [secret:1] ok", kinds: []Kind{KindGoogleKey}},
		{name: "slack token", in: slackTok, want: "[secret:1]", kinds: []Kind{KindSlackToken}},
		{name: "stripe secret key", in: "STRIPE " + stripeKey, want: "STRIPE [secret:1]", kinds: []Kind{KindStripeKey}},
		{name: "openai key", in: openaiKey, want: "[secret:1]", kinds: []Kind{KindOpenAIKey}},
		{name: "anthropic key wins over openai shape", in: anthKey, want: "[secret:1]", kinds: []Kind{KindAnthropicKey}},
		{name: "gitlab token", in: gitlabTok, want: "[secret:1]", kinds: []Kind{KindGitLabToken}},
		{name: "npm token", in: npmTok, want: "[secret:1]", kinds: []Kind{KindOtherToken}},
		{name: "jwt", in: "id_token " + jwtTok, want: "id_token [secret:1]", kinds: []Kind{KindJWT}},

		// URLs.
		{name: "url userinfo password", in: "postgres://app:pg-pass-1@db:5432/x", want: "postgres://app:[secret:1]@db:5432/x", kinds: []Kind{KindURLUserinfo}},
		{name: "token as url user", in: "https://" + rnd(29, 30, b62) + "@github.com/o/r", want: "https://[secret:1]@github.com/o/r"},
		{name: "ssh url user kept", in: "ssh://deploy@host:22", want: "ssh://deploy@host:22"},

		// .env lines.
		{name: "env secret lines", in: "DATABASE_PASSWORD=p a s s\nexport STRIPE_KEY=sk\nPATH=/usr/bin\nHOME=/home/kit\n",
			want: "DATABASE_PASSWORD=[secret:1]\nexport STRIPE_KEY=[secret:2]\nPATH=/usr/bin\nHOME=/home/kit\n", kinds: []Kind{KindEnvLine}},
		{name: "env quoted value", in: `API_TOKEN="abc def"`, want: `API_TOKEN="[secret:1]"`},

		// High entropy.
		{name: "random base62 40", in: "value " + random40 + " end", want: "value [secret:1] end", kinds: []Kind{KindHighEntropy}},
		{name: "random base64 28", in: random28, want: "[secret:1]", kinds: []Kind{KindHighEntropy}},
		{name: "git sha kept", in: "commit " + gitSHA + "\nAuthor: k", want: "commit " + gitSHA + "\nAuthor: k"},
		{name: "sha256 kept", in: "sha256:" + sha256, want: "sha256:" + sha256},
		{name: "uuid kept", in: "request-id: " + uuid, want: "request-id: " + uuid},
		{name: "long path kept", in: "/usr/local/lib/python3.11/site-packages/urllib3/connectionpool.py", want: "/usr/local/lib/python3.11/site-packages/urllib3/connectionpool.py"},
		{name: "camel identifier kept", in: "TestRedactHighEntropyFalsePositive2024", want: "TestRedactHighEntropyFalsePositive2024"},
		{name: "snake identifier kept", in: "kube_pod_container_status_restarts_total", want: "kube_pod_container_status_restarts_total"},
		{name: "k8s pod name kept", in: "api-7d9f8b6c5-x2x4z-and-more-lowercase", want: "api-7d9f8b6c5-x2x4z-and-more-lowercase"},
		{name: "base64 blob redacted (documented)", in: "data: " + rnd(30, 120, b62+"+/") + "==", gone: []string{"data: A", "=="}, kinds: []Kind{KindHighEntropy}},

		// ANSI and wrapping.
		{name: "ansi colors around a token", in: "\x1b[32mtoken\x1b[0m=\x1b[1mabc123secret\x1b[0m", want: "token=[secret:1]"},
		{name: "ansi split inside a token", in: "echo " + ghToken[:10] + "\x1b[0m" + ghToken[10:], want: "echo [secret:1]"},
		{name: "osc title and crlf", in: "\x1b]0;kit@host\x07password=abcdef\r\nok\r\n", want: "password=[secret:1]\nok\n"},
		{name: "backspace edits", in: "password=abX\bc123", want: "password=[secret:1]"},
		{name: "pem wrapped with crlf", in: strings.ReplaceAll(pemBlock, "\n", "\r\n"), want: "[secret:1]"},
		{name: "soft-wrapped long line stays one line", in: strings.Repeat("x ", 120) + ghToken, want: strings.Repeat("x ", 120) + "[secret:1]"},
		{name: "base64 hard-wrapped at 76", in: rnd(31, 76, b62+"+/") + "\n" + rnd(32, 76, b62+"+/") + "\n" + rnd(33, 30, b62) + "==",
			want: "[secret:1]\n[secret:2]\n[secret:3]"},
		// Known gap: a token hard-wrapped by a program into halves shorter
		// than any rule's minimum is not caught. Kept here so a change in
		// behavior is deliberate.
		{name: "documented gap: token split by newline", in: ghToken[:20] + "\n" + ghToken[20:], want: ghToken[:20] + "\n" + ghToken[20:]},

		// Stable placeholders.
		{name: "same value same placeholder", in: "token=aaa111bbb\nagain token=aaa111bbb\nsecret=zzz999yyy",
			want: "token=[secret:1]\nagain token=[secret:1]\nsecret=[secret:2]"},

		// Known vault values.
		{name: "known secret anywhere", in: "echo myVaultPass!", opts: Options{Known: []string{"myVaultPass!"}}, want: "echo [secret:1]", kinds: []Kind{KindKnownSecret}},
		{name: "short known secret only as a word", in: "root rooted root", opts: Options{Known: []string{"root"}}, want: "[secret:1] rooted [secret:1]"},
		{name: "two-char known secret skipped (documented)", in: "ab cd", opts: Options{Known: []string{"ab"}}, want: "ab cd"},
		{name: "known private key line", in: "line: " + strings.Split(pemBody, "\n")[0], opts: Options{Known: []string{pemBlock}}, want: "line: [secret:1]"},

		// Optional IPs.
		{name: "ips off by default", in: "from 10.0.0.5", want: "from 10.0.0.5"},
		{name: "ipv4", in: "from 10.0.0.5 to 10.0.0.6, again 10.0.0.5", opts: Options{IPs: true}, want: "from [ip:1] to [ip:2], again [ip:1]", kinds: []Kind{KindIP}},
		{name: "loopback and versions kept", in: "127.0.0.1 0.0.0.0 v1.2.3.4.5 255.255.255.0", opts: Options{IPs: true}, want: "127.0.0.1 0.0.0.0 v1.2.3.4.5 255.255.255.0"},
		{name: "ipv6", in: "inet6 fe80::1c2d:3eff:fe4f:5a6b/64 and ::1", opts: Options{IPs: true}, want: "inet6 [ip:1]/64 and ::1"},
		{name: "times and macs kept", in: "12:34:56 aa:bb:cc:dd:ee:ff std::vector", opts: Options{IPs: true}, want: "12:34:56 aa:bb:cc:dd:ee:ff std::vector"},
		{name: "saved host ip", in: "ssh 203.0.113.9", opts: Options{IPs: true, Hosts: []string{"203.0.113.9"}}, want: "ssh [ip:1]"},

		// Optional hostnames.
		{name: "hostnames off by default", in: "db.prod.example.com", want: "db.prod.example.com"},
		{name: "fqdn and prompt", in: "kit@ip-10-0-1-5:~$ ping db.prod.example.com", opts: Options{Hostnames: true}, want: "kit@[host:1]:~$ ping [host:2]", kinds: []Kind{KindHostname}},
		{name: "file names kept", in: "install.sh main.go README.md setup.py", opts: Options{Hostnames: true}, want: "install.sh main.go README.md setup.py"},
		{name: "saved host names", in: "connected to bastion-7 (Bastion-7)", opts: Options{Hostnames: true, Hosts: []string{"bastion-7", "localhost"}}, want: "connected to [host:1] ([host:1])"},
		{name: "npm version not a host", in: "react@18.3.1", opts: Options{Hostnames: true}, want: "react@18.3.1"},

		// Secrets win over ip and host.
		{name: "secret beats ip", in: "password=10.1.2.3", opts: Options{IPs: true}, want: "password=[secret:1]"},
		{name: "url with password and host", in: "redis://u:pw-9876@cache.example.com:6379", opts: Options{Hostnames: true}, want: "redis://u:[secret:1]@[host:1]:6379"},
	}
}

func TestRedactTable(t *testing.T) {
	for _, tc := range cases() {
		t.Run(tc.name, func(t *testing.T) {
			got, rep := String(tc.in, tc.opts)
			if tc.want != "" || tc.in == "" {
				if got != tc.want {
					t.Fatalf("\n in: %q\ngot: %q\nwant: %q", tc.in, got, tc.want)
				}
			}
			for _, g := range tc.gone {
				if strings.Contains(got, g) {
					t.Fatalf("%q still in %q", g, got)
				}
			}
			for _, k := range tc.kept {
				if !strings.Contains(got, k) {
					t.Fatalf("%q lost from %q", k, got)
				}
			}
			for _, k := range tc.kinds {
				if rep.Counts[k] == 0 {
					t.Fatalf("kind %s not reported: %v (out %q)", k, rep.Counts, got)
				}
			}
			if tc.want == tc.in && rep.Total() != 0 {
				t.Fatalf("nothing should match, report %v", rep.Counts)
			}
		})
	}
}

// Every case's output must be stable: redacting it again changes nothing
// (placeholders are not themselves secret-shaped).
func TestRedactIdempotent(t *testing.T) {
	for _, tc := range cases() {
		once, _ := String(tc.in, tc.opts)
		twice, rep := String(once, tc.opts)
		if once != twice {
			t.Fatalf("%s: second pass changed %q → %q (%v)", tc.name, once, twice, rep.Counts)
		}
	}
}

func TestRandomTokensCaught(t *testing.T) {
	// The backstop must catch nearly all random base62 tokens of 32+
	// characters and most of 24+.
	for _, c := range []struct {
		n    int
		min  float64
		alph string
	}{{32, 0.95, b62}, {40, 0.98, b62}, {24, 0.80, b62}, {44, 0.99, b62 + "+/"}} {
		hit := 0
		const total = 400
		for i := 0; i < total; i++ {
			tok := rnd(uint64(1000+i*c.n), c.n, c.alph)
			if looksRandom(tok) {
				hit++
			}
		}
		if rate := float64(hit) / total; rate < c.min {
			t.Fatalf("len %d: caught %.2f, want ≥ %.2f", c.n, rate, c.min)
		}
	}
}

func TestHexNeverHighEntropy(t *testing.T) {
	for i := 0; i < 200; i++ {
		for _, n := range []int{24, 32, 40, 64} {
			if looksRandom(rnd(uint64(i*n+5), n, hexLow)) {
				t.Fatal("hex string flagged as high entropy")
			}
		}
	}
}

func TestPlaceholdersSharedAcrossTexts(t *testing.T) {
	r := New(Options{IPs: true})
	a := r.Text("token=abc123def from 10.9.8.7")
	b := r.Text("again 10.9.8.7 token=abc123def, new 10.9.8.6")
	if a.String() != "token=[secret:1] from [ip:1]" || b.String() != "again [ip:1] token=[secret:1], new [ip:2]" {
		t.Fatalf("got %q / %q", a, b)
	}
	rep := r.Report()
	if rep.Counts[KindAssignment] != 2 || rep.Counts[KindIP] != 3 || rep.Total() != 5 {
		t.Fatalf("report %v", rep.Counts)
	}
	if m := rep.Strings(); m["ip"] != 3 {
		t.Fatalf("Strings() = %v", m)
	}
}

func TestReportNeverHoldsValues(t *testing.T) {
	_, rep := String("password=supersecretvalue", Options{})
	for k := range rep.Counts {
		if strings.Contains(string(k), "supersecret") {
			t.Fatal("report leaked a value")
		}
	}
}

func TestStaticIsConstantOnly(t *testing.T) {
	s := Static("You are Keel's terminal assistant.")
	if s.String() != "You are Keel's terminal assistant." {
		t.Fatal(s)
	}
	// var v = "x"; Static(v) does not compile: staticText is unexported.
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{
		"\x1b[1;31mred\x1b[0m":       "red",
		"a\r\nb\rc":                  "a\nb\nc",
		"\x1b]8;;http://x\x1b\\link": "link",
		"ab\b\bcd":                   "cd",
		"x\x00y\x07z":                "xyz",
		"tab\there":                  "tab\there",
		"\x1b(Bplain\x1b=":           "plain",
		"é\bè":                       "è",
	} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSensitiveKey(t *testing.T) {
	yes := []string{"password", "DB_PASSWORD", "dbPassword", "api_key", "apiKey", "API-KEY", "client_secret", "--token", "aws_secret_access_key", "x.auth.token", "SECRET", "accessKey"}
	no := []string{"key", "name", "PWD", "OLDPWD", "tokens_used", "passenger", "monkey", "keyboard", "author"}
	for _, k := range yes {
		if !sensitiveKey(k, false) {
			t.Errorf("%s should be sensitive", k)
		}
	}
	for _, k := range no {
		if sensitiveKey(k, false) {
			t.Errorf("%s should not be sensitive", k)
		}
	}
	if !sensitiveKey("STRIPE_KEY", true) || sensitiveKey("STRIPE_KEY", false) {
		t.Error("plain KEY counts in env lines only")
	}
}
