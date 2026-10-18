package runvars

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func lookupFrom(m map[string]string) func(string) (Value, error) {
	return func(name string) (Value, error) {
		v, ok := m[name]
		if !ok {
			return Value{}, fmt.Errorf("%w: {{%s}}", ErrMissing, name)
		}
		return Value{Value: v, From: "test"}, nil
	}
}

func TestRefs(t *testing.T) {
	cases := map[string][]string{
		"kubeadm join --token {{join_token}} --hash {{ca_hash|q}}":                  {"join_token", "ca_hash|q"},
		"docker ps --format '{{.Names}}'":                                           nil,
		"echo {{ x }} {{json .}} {{-x}}":                                            nil,
		"kubectl get po -o go-template='{{range .items}}{{.metadata.name}}{{end}}'": nil,
		"{{else}}{{true}}{{nil}}":                                                   nil,
		"{{true|q}}":                                                                {"true|q"},
		"{{FOO}} {{a}}{{a}}":                                                        {"FOO", "a", "a"},
		"{{toolongnameaaaaaaaaaaaaaaaaaaaaaaaaa}}":                                  nil,
	}
	for cmd, want := range cases {
		var got []string
		for _, r := range Refs(cmd) {
			n := r.Name
			if r.Quoted {
				n += "|q"
			}
			got = append(got, n)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q: %v, want %v", cmd, got, want)
		}
		if HasRefs(cmd) != (len(want) > 0) {
			t.Errorf("%q: HasRefs wrong", cmd)
		}
	}
	if n := Names("{{a}} {{b}} {{a|q}}"); strings.Join(n, ",") != "a,b" {
		t.Fatalf("names %v", n)
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"a", "join_token", "_x", "k8s_version"} {
		if !ValidName(n) {
			t.Errorf("%q refused", n)
		}
	}
	for _, n := range []string{"", "9a", "A", "a-b", "a.b", "end", "true", strings.Repeat("a", 33)} {
		if ValidName(n) {
			t.Errorf("%q accepted", n)
		}
	}
}

// Shell safety: unquoted values are one plain word; quoted values may hold
// shell characters but never a quote, backslash or line break.
func TestFillShellSafety(t *testing.T) {
	vals := map[string]string{
		"tok":     "abcdef.0123456789abcdef",
		"hash":    "sha256:4f1c9a0b",
		"url":     "https://x.example/a_b-c?d", // ? is a glob
		"space":   "a b",
		"semi":    "a;b",
		"dollar":  "$HOME",
		"tick":    "`id`",
		"glob":    "*",
		"dash":    "--upload-pack=evil",
		"tilde":   "~root",
		"eq":      "=ls",
		"nl":      "a\nb",
		"cr":      "a\rb",
		"tab":     "a\tb",
		"esc":     "a\x1b[31mb",
		"bidi":    "a‮b",
		"zw":      "a​b",
		"ls":      "a b",
		"nbsp":    "a b",
		"quote":   "it's",
		"bslash":  `a\b`,
		"long":    strings.Repeat("a", MaxValueBytes+1),
		"utf8":    "\xff",
		"empty":   "",
		"spaced":  "--force; rm -rf /",
		"brace":   "{a,b}",
		"hist":    "a!b",
		"hash2":   "#x",
		"caret":   "a^b",
		"percent": "%self",
	}
	ok := map[string]string{
		"echo {{tok}}":                   "echo abcdef.0123456789abcdef",
		"echo {{hash}}":                  "echo sha256:4f1c9a0b",
		"echo {{spaced|q}}":              "echo '--force; rm -rf /'",
		"echo {{space|q}}":               "echo 'a b'",
		"echo {{dollar|q}}":              "echo '$HOME'",
		"echo {{tick|q}}":                "echo '`id`'",
		"echo {{tok}}{{tok}} {{hash|q}}": "echo abcdef.0123456789abcdefabcdef.0123456789abcdef 'sha256:4f1c9a0b'",
		"no vars here":                   "no vars here",
	}
	for cmd, want := range ok {
		got, _, err := Fill(cmd, lookupFrom(vals))
		if err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", cmd, got, err, want)
		}
	}
	for _, name := range []string{"url", "space", "semi", "dollar", "tick", "glob", "dash", "tilde", "eq", "spaced", "brace", "hist", "hash2", "caret", "percent", "quote", "bslash"} {
		got, _, err := Fill("echo {{"+name+"}}", lookupFrom(vals))
		if err == nil || got != "" || !errors.Is(err, ErrUnsafe) || !strings.Contains(err.Error(), "{{"+name+"|q}}") {
			t.Errorf("%s unquoted: %q %v", name, got, err)
		}
	}
	for _, name := range []string{"nl", "cr", "tab", "esc", "bidi", "zw", "ls", "nbsp", "quote", "bslash", "long", "utf8", "empty"} {
		for _, q := range []string{"", "|q"} {
			got, _, err := Fill("echo {{"+name+q+"}}", lookupFrom(vals))
			if err == nil || got != "" {
				t.Errorf("%s%s: %q %v", name, q, got, err)
			}
		}
	}
	// A missing variable names itself; nothing is filled.
	got, _, err := Fill("a {{tok}} {{gone}}", lookupFrom(vals))
	if !errors.Is(err, ErrMissing) || got != "" || !strings.Contains(err.Error(), "{{gone}}") {
		t.Fatalf("missing: %q %v", got, err)
	}
	// Upper case is a reference no one can declare: refused, not typed.
	if _, _, err := Fill("{{TOK}}", lookupFrom(vals)); !errors.Is(err, ErrMissing) {
		t.Fatalf("upper case: %v", err)
	}
	// used lists each name once, with where it came from, never the value.
	_, used, _ := Fill("{{tok}} {{tok}} {{hash|q}}", lookupFrom(vals))
	if len(used) != 2 || used[0].Name != "tok" || !used[1].Quoted || used[0].From != "test" {
		t.Fatalf("used %+v", used)
	}
	if fmt.Sprint(used) != fmt.Sprint([]Used{{Name: "tok", From: "test"}, {Name: "hash", From: "test", Quoted: true}}) {
		t.Fatalf("used %+v", used)
	}
}

func TestQuote(t *testing.T) {
	q, err := Quote("a b;c")
	if err != nil || q != "'a b;c'" {
		t.Fatalf("%q %v", q, err)
	}
}

func TestPatterns(t *testing.T) {
	for _, bad := range []string{"", "no group", "(", strings.Repeat("(a)", 100), "a\n(b)", "(a)\x00"} {
		if _, err := CompilePattern(bad); err == nil {
			t.Errorf("%q compiled", bad)
		}
	}
	re, err := CompilePattern(`--token\s+([a-z0-9]{6}\.[a-z0-9]{16})`)
	if err != nil {
		t.Fatal(err)
	}
	out := "kubeadm join 10.20.0.10:6443 --token abcdef.0123456789abcdef \\\n"
	if v, ok := Extract(re, "join_token", out); !ok || v != "abcdef.0123456789abcdef" {
		t.Fatalf("%q %v", v, ok)
	}
	if _, ok := Extract(re, "join_token", "nothing"); ok {
		t.Fatal("matched nothing")
	}
	named, _ := CompilePattern(`(sha256):(?P<ca_hash>[a-f0-9]+)`)
	if v, ok := Extract(named, "ca_hash", "hash sha256:4f1c"); !ok || v != "4f1c" {
		t.Fatalf("named group: %q", v)
	}
	if v, _ := Extract(named, "other", "hash sha256:4f1c"); v != "sha256" {
		t.Fatalf("first group: %q", v)
	}
	empty, _ := CompilePattern(`x(a*)y`)
	if _, ok := Extract(empty, "v", "xy"); ok {
		t.Fatal("an empty group counts as a value")
	}
}
