// Vault host groups: a forest of { id, name, parentId } plus hosts that
// point at a group with groupId. Pure functions: no DOM, no state. Every
// walk up a parent chain stops at a cycle, so bad data can't hang the UI.

const byName = (a, b) => String(a || "").localeCompare(String(b || ""), undefined, { sensitivity: "base" });

export function groupById(groups, id) {
  return (groups || []).find((g) => g.id === id) || null;
}

// ancestry returns the group and its parents, nearest first.
export function ancestry(groups, id) {
  const out = [];
  const seen = new Set();
  let cur = id;
  while (cur && !seen.has(cur)) {
    seen.add(cur);
    const g = groupById(groups, cur);
    if (!g) break;
    out.push(g);
    cur = g.parentId;
  }
  return out;
}

// groupDepth is 1 for a top-level group; 0 for no group.
export function groupDepth(groups, id) {
  let depth = 0;
  const seen = new Set();
  let cur = id;
  while (cur && !seen.has(cur)) {
    seen.add(cur);
    depth += 1;
    cur = groupById(groups, cur)?.parentId;
  }
  return depth;
}

// groupDescendantDepth is how many levels of groups sit below id.
export function groupDescendantDepth(groups, id, seen = new Set()) {
  if (seen.has(id)) return 0;
  seen.add(id);
  let max = 0;
  for (const g of groups || []) {
    if (g.parentId === id) max = Math.max(max, 1 + groupDescendantDepth(groups, g.id, seen));
  }
  return max;
}

export function isGroupUnder(groups, id, ancestorId) {
  const seen = new Set();
  let cur = id;
  while (cur && !seen.has(cur)) {
    if (cur === ancestorId) return true;
    seen.add(cur);
    cur = groupById(groups, cur)?.parentId;
  }
  return false;
}

// subtreeGroupIds is root and every group below it; null for no root.
export function subtreeGroupIds(groups, root) {
  if (!root) return null;
  const ids = new Set([root]);
  const walk = (id) => {
    for (const g of groups || []) {
      if (g.parentId === id && !ids.has(g.id)) {
        ids.add(g.id);
        walk(g.id);
      }
    }
  };
  walk(root);
  return ids;
}

// hostSearchHay is the lowercase text a host search matches against,
// including the names of every group above the host.
export function hostSearchHay(groups, h) {
  const tags = Array.isArray(h.tags) ? h.tags.join(" ") : "";
  const path = ancestry(groups, h.groupId).map((g) => g.name || "").join(" ");
  return `${h.name} ${h.group} ${path} ${h.address} ${h.username} ${tags}`.toLowerCase();
}

const norm = (q) => String(q || "").trim().toLowerCase();

// hostsInSubtree is the hosts in focus's subtree (all hosts with no focus)
// that match query.
export function hostsInSubtree(groups, hosts, focus, query) {
  const q = norm(query);
  const allow = subtreeGroupIds(groups, focus);
  return (hosts || []).filter((h) => {
    if (allow && !allow.has(h.groupId || "")) return false;
    return !q || hostSearchHay(groups, h).includes(q);
  });
}

// nestGroupTree builds the folder tree under parentId ("" for the top):
// [{ group, hosts, kids, count, depth }], sorted by name. With a query,
// hosts are filtered and a group survives only if it (or something
// below it) matches.
export function nestGroupTree(groups, hosts, query, parentId) {
  const q = norm(query);
  const byParent = new Map();
  for (const g of groups || []) {
    const parent = g.parentId || "";
    if (!byParent.has(parent)) byParent.set(parent, []);
    byParent.get(parent).push(g);
  }
  const hostsBy = new Map();
  for (const h of hosts || []) {
    if (!h.groupId || (q && !hostSearchHay(groups, h).includes(q))) continue;
    if (!hostsBy.has(h.groupId)) hostsBy.set(h.groupId, []);
    hostsBy.get(h.groupId).push(h);
  }
  const seen = new Set();
  const build = (pid, depth) => {
    const out = [];
    for (const g of [...(byParent.get(pid) || [])].sort((a, b) => byName(a.name, b.name))) {
      if (seen.has(g.id)) continue;
      seen.add(g.id);
      const kids = build(g.id, depth + 1);
      const own = hostsBy.get(g.id) || [];
      const count = own.length + kids.reduce((n, k) => n + k.count, 0);
      if (q && count === 0 && !String(g.name || "").toLowerCase().includes(q)) continue;
      out.push({ group: g, hosts: own, kids, count, depth });
    }
    return out;
  };
  return build(String(parentId || ""), 1);
}

// groupOptionLabel is "Parent / Child / Group".
export function groupOptionLabel(groups, g) {
  return ancestry(groups, g?.id)
    .map((x) => x.name || "")
    .reverse()
    .join(" / ");
}

// groupOptionEntries lists groups for a parent picker, leaving out
// excludeId and everything under it (a group can't move into itself).
export function groupOptionEntries(groups, excludeId) {
  const skip = subtreeGroupIds(groups, excludeId) || new Set();
  return (groups || [])
    .filter((g) => !skip.has(g.id))
    .map((g) => ({ value: g.id, label: groupOptionLabel(groups, g) }))
    .sort((a, b) => byName(a.label, b.label));
}

export function parseHostTags(value) {
  return String(value || "")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean);
}

export function hostSubtitle(h) {
  if (h.authType === "local") return h.subtitle || "this machine";
  const port = h.port && h.port !== 22 ? `:${h.port}` : "";
  const ident = h.identityId ? " · identity" : "";
  return `${h.username}@${h.address}${port}${ident}`;
}
