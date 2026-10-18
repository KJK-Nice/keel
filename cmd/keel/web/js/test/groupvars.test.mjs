import { test } from "node:test";
import assert from "node:assert/strict";
import * as V from "../lib/groupvars.js";
import { bytesHTML } from "../lib/agent.js";
import { toInput, checkInput } from "../lib/runbooks.js";
import { readFileSync } from "node:fs";

test("refs: like internal/runvars, never Go templates, Jinja or reserved words", () => {
  const names = (cmd) => V.refs(cmd).map((r) => r.name + (r.quoted ? "|q" : ""));
  assert.deepEqual(names("kubeadm join --token {{join_token}} --hash {{ca_hash|q}}"), ["join_token", "ca_hash|q"]);
  assert.deepEqual(names("docker ps --format '{{.Names}}' {{ x }} {{json .}}"), []);
  assert.deepEqual(names("kubectl get po -o go-template='{{range .items}}{{.metadata.name}}{{end}}'"), []);
  assert.deepEqual(names("{{FOO}}"), ["FOO"]);
  assert.equal(V.validName("join_token"), true);
  for (const bad of ["9x", "a-b", "end", "A", ""]) assert.equal(V.validName(bad), false, bad);
});

test("cleanVar: names and one-line values as keel serve keeps them", () => {
  assert.deepEqual(V.cleanVar(" API_VIP ", " 10.20.0.10 "), { ok: true, name: "api_vip", value: "10.20.0.10" });
  assert.equal(V.cleanVar("9x", "a").ok, false);
  for (const bad of ["a\nb", "a\tb", "a​b", "a b", "a‮b", "a﻿b", "x".repeat(257)]) assert.equal(V.cleanVar("v", bad).ok, false, JSON.stringify(bad));
});

test("bytesWithVars: chips for {{name}}, every other byte escaped as bytesHTML draws it", () => {
  const html = V.bytesWithVars("\u0015echo <b>{{tok}}\nnext {{x|q}}\n", bytesHTML);
  assert.match(html, /<span class="k-gx-var">\{\{tok\}\}<\/span>/);
  assert.match(html, /<span class="k-gx-var">\{\{x\|q\}\}<\/span>/);
  assert.match(html, /&lt;b&gt;/);
  assert.doesNotMatch(html, /<b>/);
  // The line break between the chips is kept.
  assert.match(html, /\{\{tok\}\}<\/span><span class="k-ctl">[^<]+<\/span>\nnext/);
  assert.equal(V.bytesWithVars("plain\n", bytesHTML), bytesHTML("plain\n"));
});

test("usesLine and captures: names only, captured values never shown", () => {
  const steps = [{ captures: [{ name: "join_token", pattern: "--token\\s+(\\S+)" }] }, { vars: [{ name: "join_token" }, { name: "k8s_version" }, { name: "join_token", quoted: true }] }];
  assert.equal(V.usesLine(steps[1], steps, 1), "Uses 1 group variable and 1 captured value · filled in at Type");
  assert.equal(V.usesLine({ vars: [] }, steps, 1), "");
  assert.deepEqual(V.captureView({ name: "t", state: "captured", bytes: 23 }), { mask: "••••••••••••", text: "23 bytes", state: "captured" });
  assert.match(V.captureView({ state: "blocked" }).text, /vault/);
  assert.equal(V.captureView(undefined).text, "not captured yet");
  assert.match(V.expiryLine({ cells: [] }), /after 1 hour/);
  assert.equal(V.usedLine([{ name: "a", from: "group variable" }, { name: "b", quoted: true, from: "captured on cp-1 · step 1" }]), "{{a}} group variable, {{b|q}} captured on cp-1 · step 1");
});

test("fillRows: exact filled bytes per host; a failed fill cannot be ticked", () => {
  const rows = [
    { proposalId: "p1", host: "wk-1", bytes: "\u0015x {{t}}\n", checked: true, needsShift: false },
    { proposalId: "p2", host: "wk-2", bytes: "\u0015x {{t}}\n", checked: false, needsShift: false },
    { proposalId: "p3", host: "wk-3", bytes: "\u0015x {{t}}\n", checked: false, needsShift: false },
  ];
  const out = V.fillRows(rows, [
    { proposalId: "p1", bytes: "\u0015x abc\n", used: [{ name: "t", from: "group variable" }], needsShift: true },
    { proposalId: "p2", error: "{{t}} has no value for wk-2" },
  ]);
  assert.equal(out[0].bytes, "\u0015x abc\n");
  assert.equal(out[0].needsShift, true);
  assert.equal(out[1].bytes, "");
  assert.equal(out[1].checked, false);
  assert.match(out[1].error, /no value/);
  assert.match(out[2].error, /no longer waiting/);
  assert.deepEqual(
    V.approveGroups([
      { host: "a", bytes: "1" },
      { host: "b", bytes: "1" },
      { host: "c", bytes: "2" },
    ]).map((g) => g.hosts),
    [["a", "b"], ["c"]],
  );
});

test("runbook captures in the editor's input", () => {
  const input = toInput({ name: "k8s", steps: [{ title: "init", command: "kubeadm init", captures: [{ name: " Join_Token ", pattern: " --token\\s+(\\S+) " }, { name: "", pattern: "" }] }] });
  assert.deepEqual(input.steps[0].captures, [{ name: "join_token", pattern: "--token\\s+(\\S+)" }]);
  assert.equal(checkInput(input), "");
  const dup = toInput({ name: "k", steps: [{ title: "a", captures: [{ name: "x", pattern: "(a)" }] }, { title: "b", captures: [{ name: "x", pattern: "(b)" }] }] });
  assert.match(checkInput(dup), /captured by another step/);
  assert.match(checkInput(toInput({ name: "k", steps: [{ title: "a", captures: [{ name: "x", pattern: "" }] }] })), /needs a pattern/);
  assert.match(checkInput(toInput({ name: "k", steps: [{ title: "a", captures: [{ name: "9", pattern: "(a)" }] }] })), /name/);
});

test("trace: values sent as {{name}} are counted apart from redactions", async () => {
  const A = await import("../lib/agent.js");
  assert.equal(A.traceText({ status: "read", host: "cp-1", lines: 40, redactions: { captured: 2 } }), "Read cp-1 · last 40 lines · 2 values withheld by Keel");
  assert.equal(A.traceText({ status: "read", host: "cp-1", lines: 1, redactions: { aws_key: 1, variable: 1 } }), "Read cp-1 · last 1 line · 1 redacted · 1 value withheld by Keel");
});

test("exportQuery and the dry-run hint", () => {
  assert.equal(V.exportQuery({ format: "yaml" }), "format=yaml");
  assert.equal(V.exportQuery({ format: "ini", runbookId: "rb_1", mode: "parallel", user: false, jump: true }), "format=ini&runbookId=rb_1&mode=parallel&user=0&jump=1");
  assert.equal(V.dryRun([{ kind: "inventory", name: "inventory.ini" }, { kind: "playbook", name: "playbooks/x.yml" }]), "ansible-playbook -i inventory.ini playbooks/x.yml --check");
  assert.match(V.fileMeta({ name: "inventory.yml", lines: 1, kind: "inventory" }), /^1 line · YAML/);
});

// The run page asks keel serve to fill the values only after Type into N
// panes was pressed, then shows them in the approve dialog, and types each
// ticked host with the bytes the dialog showed.
test("run page: fill before approve, type the approved bytes", () => {
  const src = readFileSync(new URL("../ui/grouprun.js", import.meta.url), "utf8");
  const fill = src.indexOf("/fill`");
  const approve = src.indexOf("await approve(rows)");
  const type = src.indexOf("deps.typeGroup({ runId: run.id, proposal: cell.proposal, bytes: r.bytes");
  assert.ok(fill > 0 && approve > fill && type > approve, "fill → approve → type");
  // Copy never puts a value on the clipboard: it copies the saved command.
  assert.match(src, /writeText\(st\.command\)/);
});
