import { test } from "node:test";
import assert from "node:assert/strict";
import { markdownHTML, safeLinkURL } from "../lib/markdown.js";
import { openExternal, nativeChannel, OPEN_PREFIX } from "../lib/openlink.js";

const md = (s) => markdownHTML(s, { copyIcon: "<svg></svg>" });

// Every tag the renderer may produce. Anything else in the output means
// model text reached the page as markup.
const ALLOWED = new Set([
  "p", "br", "strong", "em", "del", "code", "pre", "a", "ul", "ol", "li", "blockquote", "hr",
  "h1", "h2", "h3", "h4", "h5", "h6", "div", "span", "button", "svg", "table", "thead", "tbody", "tr", "th", "td",
]);
const ALLOWED_ATTRS = new Set(["class", "href", "title", "target", "rel", "data-k-external", "type", "data-md-copy", "aria-label", "start"]);

function assertSafe(html) {
  for (const m of html.matchAll(/<\/?([a-zA-Z][\w-]*)([^>]*)>/g)) {
    assert.ok(ALLOWED.has(m[1].toLowerCase()), `unexpected tag <${m[1]}> in ${html}`);
    for (const a of m[2].matchAll(/([^\s=]+)(?:="([^"]*)")?/g)) {
      assert.ok(ALLOWED_ATTRS.has(a[1]), `unexpected attribute ${a[1]} in ${html}`);
      if (a[1] === "href") assert.match(a[2], /^(https?:\/\/|mailto:)/, `href ${a[2]}`);
    }
  }
  assert.doesNotMatch(html, /href="(?!https?:|mailto:)/i);
}

test("prose: paragraphs, emphasis, inline code, line breaks", () => {
  assert.equal(md("Hello **world**, *you* and `df -h`."), "<p>Hello <strong>world</strong>, <em>you</em> and <code>df -h</code>.</p>");
  assert.equal(md("one\ntwo\n\nthree"), "<p>one<br>two</p><p>three</p>");
  assert.equal(md("~~old~~ new"), "<p><del>old</del> new</p>");
  assert.equal(md("snake_case_name and 2 * 3 * 4"), "<p>snake_case_name and 2 * 3 * 4</p>");
  assert.equal(md("`a **b** c`"), "<p><code>a **b** c</code></p>", "no emphasis inside code");
  assert.equal(md("``a ` b``"), "<p><code>a ` b</code></p>");
  assert.equal(md("a ** b"), "<p>a ** b</p>");
  assert.equal(md("\\*not em\\*"), "<p>*not em*</p>");
  assert.equal(md("***both***"), "<p><strong><em>both</em></strong></p>");
});

test("headings, rules, quotes", () => {
  assert.equal(md("# Big\n## Two\n###### Six"), '<h1 class="k-md-h">Big</h1><h2 class="k-md-h">Two</h2><h6 class="k-md-h">Six</h6>');
  assert.equal(md("#nospace"), "<p>#nospace</p>");
  assert.equal(md("a\n\n---\n\nb"), "<p>a</p><hr><p>b</p>");
  assert.equal(md("> quoted\n> **line**"), "<blockquote><p>quoted<br><strong>line</strong></p></blockquote>");
});

test("lists: bullets, ordered, nested, code inside an item", () => {
  assert.equal(md("- a\n- b\n  - c\n- d"), "<ul><li>a</li><li>b<ul><li>c</li></ul></li><li>d</li></ul>");
  assert.equal(md("1. one\n2. two"), "<ol><li>one</li><li>two</li></ol>");
  assert.equal(md("3. three\n4. four"), '<ol start="3"><li>three</li><li>four</li></ol>');
  assert.equal(md("Steps:\n1. a\n2. b"), "<p>Steps:</p><ol><li>a</li><li>b</li></ol>");
  assert.equal(md("In\n2026. it ran"), "<p>In<br>2026. it ran</p>", "only a list at 1 interrupts a paragraph");
  const html = md("1. Check:\n\n   ```sh\n   df -h\n   ```\n2. Next");
  assert.match(html, /^<ol><li><p>Check:<\/p><div class="k-md-code">.*<code>df -h<\/code><\/pre><\/div><\/li><li>Next<\/li><\/ol>$/);
});

test("fenced code: escaped, mono block, a Copy button and never a Type button", () => {
  const html = md("```sh\nrm -rf /tmp/x && echo \"<b>hi</b>\"\n```");
  assert.equal(
    html,
    '<div class="k-md-code"><div class="k-md-code-bar"><span class="k-md-code-lang">sh</span>' +
      '<button type="button" class="k-btn k-btn--ghost k-btn--sm k-btn--icon" data-md-copy aria-label="Copy code" title="Copy"><svg></svg></button></div>' +
      "<pre><code>rm -rf /tmp/x &amp;&amp; echo &quot;&lt;b&gt;hi&lt;/b&gt;&quot;</code></pre></div>",
  );
  assert.doesNotMatch(html, /Type into|data-prop|data-fence-run|>Run|Insert/, "a chat code block has no Type or Run (§6.1)");
  assert.equal((html.match(/<button/g) || []).length, 1);
  assert.match(md("~~~\nplain\n~~~"), /<span class="k-md-code-lang"><\/span>.*<code>plain<\/code>/);
  assert.match(md('```"><img src=x onerror=alert(1)>\nx\n```'), /<span class="k-md-code-lang"><\/span>/, "a hostile info string is dropped");
  assertSafe(md("```html\n<script>alert(1)</script>\n<img src=x onerror=alert(1)>\n```"));
  assert.match(md("```\n\tindented\ttab\n```"), /<code>\tindented\ttab<\/code>/, "tabs are kept in code");
});

test("partial text: an unclosed fence runs to the end and never breaks out", () => {
  const partial = "Try this:\n\n```sh\necho one\n<b>still code";
  const html = md(partial);
  assert.match(html, /^<p>Try this:<\/p><div class="k-md-code">.*<code>echo one\n&lt;b&gt;still code<\/code><\/pre><\/div>$/s);
  // Each prefix of a growing answer renders to well-formed, safe markup.
  const full = "# Plan\n\n1. **Check** `disk`\n2. Run:\n\n```sh\ndf -h | sort\n```\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nSee [docs](https://example.com).";
  for (let n = 0; n <= full.length; n++) assertSafe(md(full.slice(0, n)));
});

test("raw HTML is text", () => {
  for (const src of [
    "<script>alert(1)</script>",
    "<img src=x onerror=alert(1)>",
    '<a href="javascript:alert(1)">x</a>',
    "<svg onload=alert(1)>",
    "<iframe srcdoc=x></iframe>",
    "**<img src=x onerror=alert(1)>**",
    "<style>body{display:none}</style>",
    "<!-- comment --> &lt;already&gt; &amp;",
  ]) {
    const html = md(src);
    assertSafe(html);
    assert.doesNotMatch(html, /<(script|img|a |svg|iframe|style|!--)/);
  }
  assert.equal(md("<b>bold</b>"), "<p>&lt;b&gt;bold&lt;/b&gt;</p>");
  assert.equal(md("&amp; &lt;"), "<p>&amp;amp; &amp;lt;</p>", "entities are shown as typed, not decoded");
});

test("images are text and nothing is fetched", () => {
  assert.equal(md("![chart](https://evil.example/x.png)"), "<p>![chart](https://evil.example/x.png)</p>");
  assert.equal(md("![a](data:image/png;base64,AAAA)"), "<p>![a](data:image/png;base64,AAAA)</p>");
  assert.equal(md('[![x](https://e.example/i.png)](https://e.example)'), '<p><a class="k-md-link" href="https://e.example/" title="https://e.example/" target="_blank" rel="noopener noreferrer" data-k-external>![x](https://e.example/i.png)</a></p>');
  assert.doesNotMatch(md("![x](https://evil.example/x.png)"), /<img|src=/);
});

test("links: http, https and mailto only, with the URL in the tooltip", () => {
  assert.equal(
    md("See [the docs](https://example.com/a?b=1&c=2)."),
    '<p>See <a class="k-md-link" href="https://example.com/a?b=1&amp;c=2" title="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer" data-k-external>the docs</a>.</p>',
  );
  assert.match(md("[mail](mailto:ops@example.com)"), /href="mailto:ops@example.com"/);
  for (const src of [
    "[x](javascript:alert(1))",
    "[x](JaVaScRiPt:alert(1))",
    "[x](java\tscript:alert(1))",
    "[x](javascript&#58;alert(1))",
    "[x](data:text/html,<script>alert(1)</script>)",
    "[x](file:///etc/passwd)",
    "[x](vbscript:msgbox)",
    "[x](//evil.example)",
    "[x](/relative)",
    "[x](https://user:pw@evil.example)",
    "[x](https://exa\u200bmple.com)",
    "[x]( javascript:alert(1))",
    "<javascript:alert(1)>",
    "<data:text/html,hi>",
  ]) {
    const html = md(src);
    assertSafe(html);
    assert.doesNotMatch(html, /<a /, `${src} → ${html}`);
  }
  assert.equal(md("[x](javascript:alert(1))"), "<p>[x](javascript:alert(1))</p>", "a refused link stays as its text");
});

test("autolinks and bare URLs", () => {
  assert.equal(md("<https://example.com/x>"), '<p><a class="k-md-link" href="https://example.com/x" title="https://example.com/x" target="_blank" rel="noopener noreferrer" data-k-external>https://example.com/x</a></p>');
  assert.match(md("go to https://example.com/path."), /href="https:\/\/example.com\/path" [^>]*>https:\/\/example.com\/path<\/a>\.<\/p>$/);
  assert.match(md("(see https://en.wikipedia.org/wiki/A_(b))"), /href="https:\/\/en.wikipedia.org\/wiki\/A_\(b\)"/);
  assert.match(md('https://example.com/"onmouseover="alert(1)'), /href="https:\/\/example.com\/"/, "a quote ends a bare URL");
  assert.equal(md("not a link: xhttps://example.com"), "<p>not a link: xhttps://example.com</p>");
  assert.doesNotMatch(md("`https://example.com`"), /<a /, "no link inside code");
});

test("redaction placeholders and withheld values stay as written", () => {
  assert.equal(md("export TOKEN=[secret:1] on [host:2] ([ip:3])"), "<p>export TOKEN=[secret:1] on [host:2] ([ip:3])</p>");
  assert.equal(md("join with {{join_token}} or {{k8s_version|q}}"), "<p>join with {{join_token}} or {{k8s_version|q}}</p>");
  assert.equal(md("**[secret:1]** and `[secret:2]`"), "<p><strong>[secret:1]</strong> and <code>[secret:2]</code></p>");
  assert.equal(md("{{a_b_c}} _x_"), "<p>{{a_b_c}} <em>x</em></p>", "underscores in a withheld name are not emphasis");
  for (const src of ["[secret:1](https://evil.example)", "[label](https://[host:1]/x)", "https://[host:1]/x", "[go](https://example.com/{{token}})", "<https://example.com/[secret:1]>"]) {
    const html = md(src);
    assert.ok(/\[(secret|host):1\]|\{\{token\}\}/.test(html), `${src} → ${html}`);
    assert.doesNotMatch(html, /href="[^"]*(secret|host|%7B%7B|\{\{)/, `${src} → ${html}`);
  }
  assert.match(md("```sh\nexport KEEL_TOKEN=[secret:1]\n```"), /<code>export KEEL_TOKEN=\[secret:1\]<\/code>/);
});

test("tables", () => {
  assert.equal(
    md("| Host | Disk |\n|:--|--:|\n| `web-1` | **91%** |\n| a \\| b |"),
    '<div class="k-md-table"><table><thead><tr><th>Host</th><th class="k-md-right">Disk</th></tr></thead>' +
      '<tbody><tr><td><code>web-1</code></td><td class="k-md-right"><strong>91%</strong></td></tr><tr><td>a | b</td><td class="k-md-right"></td></tr></tbody></table></div>',
  );
  assertSafe(md("| <script>x</script> |\n|---|\n| <img src=x onerror=y> |"));
});

test("pathological input stays bounded", () => {
  const t0 = Date.now();
  md("*".repeat(20000) + "[".repeat(20000) + "`".repeat(5000) + "_a".repeat(10000));
  md("> ".repeat(500) + "deep");
  md("- ".repeat(500) + "deep");
  assert.ok(Date.now() - t0 < 2000, "renders in under 2 s");
});

test("safeLinkURL", () => {
  assert.equal(safeLinkURL("https://example.com"), "https://example.com/");
  assert.equal(safeLinkURL("HTTP://Example.com/a"), "http://example.com/a");
  assert.equal(safeLinkURL("mailto:a@b.c"), "mailto:a@b.c");
  for (const bad of ["", "javascript:alert(1)", "data:,x", "file:///x", "ftp://x", "https://", "mailto:", "https://a b", "https://a\n", "x".repeat(3000)]) {
    assert.equal(safeLinkURL(bad), "", bad);
  }
});

test("openExternal: native window posts to the app; a browser opens a new tab; bad URLs open nothing", () => {
  const posted = [];
  const nativeWin = {
    document: { documentElement: { dataset: { native: "1" } } },
    webkit: { messageHandlers: { external: { postMessage: (m) => posted.push(m) } } },
  };
  assert.ok(nativeChannel(nativeWin));
  assert.equal(openExternal("https://example.com/a", nativeWin), true);
  assert.deepEqual(posted, [OPEN_PREFIX + "https://example.com/a"]);
  assert.equal(openExternal("javascript:alert(1)", nativeWin), false);
  assert.equal(openExternal("file:///etc/passwd", nativeWin), false);
  assert.equal(posted.length, 1);

  const clicked = [];
  const body = { appendChild() {} };
  const browserWin = {
    document: {
      documentElement: { dataset: {} },
      body,
      createElement: () => ({ click() { clicked.push({ href: this.href, target: this.target, rel: this.rel }); }, remove() {} }),
    },
    webkit: { messageHandlers: { external: { postMessage: () => assert.fail("not native") } } },
  };
  assert.equal(nativeChannel(browserWin), null);
  assert.equal(openExternal("https://example.com/b", browserWin), true);
  assert.deepEqual(clicked, [{ href: "https://example.com/b", target: "_blank", rel: "noopener noreferrer" }]);
});
