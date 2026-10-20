import { test } from "node:test";
import assert from "node:assert/strict";
import {
  NO_MATCHES,
  comboKey,
  comboboxHTML,
  filterOptions,
  initialActive,
  normalizeOptions,
  optionMatches,
  pickValue,
  stepActive,
} from "../lib/combobox.js";

const opts = [
  { label: "deploy-ed25519", desc: "key · ops" },
  { label: "Root password", desc: "password · root" },
  { label: "bastion", desc: "ops@10.0.0.5:22" },
  { label: "Ünïcode", desc: "" },
];

test("optionMatches: case-insensitive substring of the label or the secondary line", () => {
  assert.equal(optionMatches(opts[0], "DEPLOY"), true);
  assert.equal(optionMatches(opts[0], "ed255"), true);
  assert.equal(optionMatches(opts[1], "pass"), true);
  assert.equal(optionMatches(opts[2], "10.0.0"), true, "the address finds its host");
  assert.equal(optionMatches(opts[3], "ÜNÏ"), true);
  assert.equal(optionMatches(opts[0], "nope"), false);
  assert.equal(optionMatches(opts[0], ""), true);
  assert.equal(optionMatches(opts[0], "   "), true, "spaces only is no filter");
  assert.equal(optionMatches(opts[0], " deploy "), true, "the query is trimmed");
  assert.equal(optionMatches({ label: "a.b" }, "."), true, "no pattern syntax");
});

test("filterOptions returns the shown indexes in order", () => {
  assert.deepEqual(filterOptions(opts, "o"), [0, 1, 2, 3].filter((i) => optionMatches(opts[i], "o")));
  assert.deepEqual(filterOptions(opts, "ops"), [0, 2]);
  assert.deepEqual(filterOptions(opts, "zzz"), []);
  assert.deepEqual(filterOptions(opts, ""), [0, 1, 2, 3]);
  assert.deepEqual(filterOptions(null, "x"), []);
});

const it = (shown, disabled = false) => ({ shown, disabled });

test("stepActive skips hidden and disabled options and does not wrap", () => {
  const items = [it(true), it(false), it(true, true), it(true), it(true)];
  assert.equal(stepActive(items, 0, 1), 3);
  assert.equal(stepActive(items, 3, -1), 0);
  assert.equal(stepActive(items, 4, 1), 4, "End of the list stays");
  assert.equal(stepActive(items, 0, -1), 0, "start of the list stays");
  assert.equal(stepActive(items, -1, 1), 0, "nothing active: Down takes the first");
  assert.equal(stepActive(items, -1, -1), 4, "nothing active: Up takes the last");
  assert.equal(stepActive(items, 2, "first"), 0);
  assert.equal(stepActive(items, 0, "last"), 4);
  assert.equal(stepActive([it(false), it(true, true)], -1, "first"), -1, "nothing to pick");
});

test("initialActive opens on the chosen option, else the first enabled one", () => {
  const items = [it(true, true), it(true), it(true)];
  assert.equal(initialActive(items, 2), 2);
  assert.equal(initialActive(items, -1), 1);
  assert.equal(initialActive(items, 0), 1, "a disabled choice is not active");
});

const key = (k, extra = {}) => ({ key: k, altKey: false, shiftKey: false, metaKey: false, ctrlKey: false, ...extra });
const items = [it(true), it(true, true), it(true), it(true)];

test("comboKey: closed", () => {
  const closed = { open: false, active: 2, items };
  assert.deepEqual(comboKey(closed, key("ArrowDown")), { action: "open", active: 2, prevent: true });
  assert.deepEqual(comboKey(closed, key("ArrowUp")), { action: "open", active: 2, prevent: true });
  assert.deepEqual(comboKey(closed, key("ArrowDown", { altKey: true })), { action: "open", active: 2, prevent: true });
  assert.deepEqual(comboKey({ ...closed, active: -1 }, key("ArrowDown")), { action: "open", active: 0, prevent: true });
  // Enter submits the form, Escape closes the dialog, Home moves the caret.
  for (const k of ["Enter", "Escape", "Home", "End", "Tab", "a"]) assert.equal(comboKey(closed, key(k)).action, "", k);
  assert.equal(comboKey(closed, key("Enter")).prevent, undefined);
});

test("comboKey: open", () => {
  const open = { open: true, active: 0, items };
  assert.deepEqual(comboKey(open, key("ArrowDown")), { action: "move", active: 2, prevent: true });
  assert.deepEqual(comboKey({ ...open, active: 2 }, key("ArrowUp")), { action: "move", active: 0, prevent: true });
  assert.deepEqual(comboKey(open, key("End")), { action: "move", active: 3, prevent: true });
  assert.deepEqual(comboKey({ ...open, active: 3 }, key("Home")), { action: "move", active: 0, prevent: true });
  assert.deepEqual(comboKey(open, key("PageDown")), { action: "move", active: 3, prevent: true });
  assert.deepEqual(comboKey(open, key("Enter")), { action: "commit", active: 0, prevent: true });
  assert.deepEqual(comboKey({ ...open, active: 1 }, key("Enter")), { action: "", prevent: true }, "a disabled option is not picked");
  assert.deepEqual(comboKey({ ...open, active: -1 }, key("Enter")), { action: "", prevent: true }, "no matches: Enter does nothing and does not submit");
  assert.deepEqual(comboKey(open, key("Escape")), { action: "close", prevent: true, stop: true });
  assert.deepEqual(comboKey(open, key("ArrowUp", { altKey: true })), { action: "close", prevent: true });
  assert.equal(comboKey(open, key("Tab")).action, "close");
  assert.equal(comboKey(open, key("Tab")).prevent, undefined, "Tab still moves focus");
  assert.equal(comboKey(open, key("Home", { shiftKey: true })).action, "", "⇧Home selects text");
  assert.equal(comboKey(open, key("a")).action, "", "letters go to the input (filter)");
  assert.equal(comboKey(open, key("ArrowDown", { metaKey: true })).action, "");
  assert.equal(comboKey(open, null).action, "");
});

test("normalizeOptions adds None once, first", () => {
  assert.deepEqual(
    normalizeOptions([{ value: "a", label: "A" }], "None").map((o) => [o.value, o.label]),
    [
      ["", "None"],
      ["a", "A"],
    ],
  );
  assert.equal(normalizeOptions([{ value: "", label: "No group" }], "None").length, 1, "a caller's own empty option wins");
  assert.equal(normalizeOptions([{ value: 7 }])[0].label, "7");
});

test("comboboxHTML: ARIA wiring, hidden value, escaping", () => {
  const html = comboboxHTML({
    id: "role-h1",
    name: "role",
    label: 'Role of <web> "1"',
    value: "worker",
    none: "No role",
    mono: true,
    attrs: 'data-role-host="h1"',
    options: [
      { value: "worker", label: "worker" },
      { value: "x<y", label: "<b>x</b>", desc: "10.0.0.1", descMono: true, disabled: true },
    ],
  });
  for (const want of [
    'role="combobox"',
    'aria-expanded="false"',
    'aria-controls="role-h1-list"',
    'aria-autocomplete="list"',
    'autocapitalize="off"',
    'autocorrect="off"',
    'spellcheck="false"',
    'autocomplete="off"',
    'role="listbox" id="role-h1-list"',
    'aria-label="Role of &lt;web&gt; &quot;1&quot;"',
    'name="role" value="worker" data-combobox-value',
    'value="worker" placeholder',
    'id="role-h1-list-o0" data-value="" aria-selected="false"',
    'id="role-h1-list-o1" data-value="worker" aria-selected="true"',
    'data-value="x&lt;y" aria-selected="false" aria-disabled="true"',
    "&lt;b&gt;x&lt;/b&gt;",
    'class="k-combobox-desc k-combobox-desc--mono">10.0.0.1',
    'class="k-combobox k-combobox--mono" data-combobox data-role-host="h1"',
    'popover="manual" hidden',
    `<p class="k-combobox-empty" hidden>${NO_MATCHES}</p>`,
  ]) {
    assert.ok(html.includes(want), `missing ${want}`);
  }
  assert.ok(!html.includes("<b>x</b>"));
});

test("comboboxHTML: no options shows the empty label, an unknown value posts nothing", () => {
  const html = comboboxHTML({ id: "x", name: "hostId", value: "gone", emptyLabel: "Add an SSH host first", required: true });
  assert.ok(html.includes('placeholder="Add an SSH host first"'));
  assert.ok(html.includes('<p class="k-combobox-empty">Add an SSH host first</p>'));
  assert.ok(html.includes('name="hostId" value="" data-combobox-value'));
  assert.ok(html.includes(" required"));
  const bad = comboboxHTML({ id: "y", value: "b", options: [{ value: "a", label: "A" }] });
  assert.ok(bad.includes('value="" data-combobox-value'));
});

test("pickValue never swaps a stale value for another real one", () => {
  const hosts = [
    { value: "h1", label: "web" },
    { value: "h2", label: "db" },
  ];
  assert.equal(pickValue(hosts, "h2"), "h2", "a listed value stays");
  assert.equal(pickValue(hosts, ""), "h1", "no value (a new item) takes the first, as a select did");
  assert.equal(pickValue(hosts, undefined), "h1");
  assert.equal(pickValue(hosts, "gone"), "", "a deleted host is not replaced by web");
  assert.equal(pickValue([{ value: "", label: "None" }, ...hosts], "gone"), "", "None, not a host");
  assert.equal(pickValue([], "gone"), "");
  assert.equal(pickValue([...hosts, { value: "gone", label: "Missing host — pick another", missing: true }], "gone"), "gone", "kept when listed as missing");
});

test("a missing option is marked, so a required picker can refuse it", () => {
  const html = comboboxHTML({
    id: "tunnel-host",
    name: "hostId",
    value: "gone",
    required: true,
    options: [
      { value: "h1", label: "web" },
      { value: "gone", label: "Missing host — pick another", missing: true },
    ],
  });
  assert.ok(html.includes('class="k-menu-item k-combobox-option k-combobox-option--missing"'));
  assert.ok(html.includes('data-value="gone" aria-selected="true" data-missing'));
  assert.ok(html.includes('value="Missing host — pick another"'), "the input says it");
  assert.ok(html.includes('name="hostId" value="gone" data-combobox-value'));
  assert.equal(normalizeOptions([{ value: "a" }])[0].missing, false);
});
