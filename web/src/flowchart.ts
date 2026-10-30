// A Mermaid flowchart as nodes, edges and groups (subgraphs), for the visual editor:
// parse the common syntax, write it back. Lines the editor does not model (classDef,
// style, class, comments) are kept as they are; diagrams using constructs it cannot
// round-trip (click handlers, linkStyle) are reported so they stay code-only.

export type Shape = "rect" | "round" | "stadium" | "circle" | "diamond" | "hexagon" | "cylinder" | "subroutine";
export type EdgeKind = "arrow" | "open" | "dotted" | "thick";
export type FNode = { id: string; label: string; shape: Shape; cls?: string; group?: string; x?: number; y?: number };
// A subgraph. Its box is drawn around its members; x/y/w/h is kept for empty groups.
export type FGroup = { id: string; label: string; parent?: string; dir?: string; x?: number; y?: number; w?: number; h?: number };
export type FEdge = { id: string; from: string; to: string; label: string; kind: EdgeKind };
export type Flow = { dir: string; nodes: FNode[]; edges: FEdge[]; extra: string[]; groups: FGroup[] };

const SHAPES: [Shape, string, string][] = [
  ["cylinder", "[(", ")]"], ["stadium", "([", "])"], ["subroutine", "[[", "]]"], ["circle", "((", "))"],
  ["hexagon", "{{", "}}"], ["rect", "[", "]"], ["round", "(", ")"], ["diamond", "{", "}"],
];
const OPEN: Record<Shape, [string, string]> = Object.fromEntries(SHAPES.map(([s, a, b]) => [s, [a, b]])) as Record<Shape, [string, string]>;
const ARROW: Record<EdgeKind, string> = { arrow: "-->", open: "---", dotted: "-.->", thick: "==>" };
const LAYOUT = "%% kuli-layout:";

export class Unsupported extends Error {}

// node parses "A", "A[Label]", "A(Label):::cls" at the start of s.
function node(s: string): { n: FNode; rest: string } | null {
  const m = /^([A-Za-z0-9_]+(?:-[A-Za-z0-9_]+)*)/.exec(s); // "A-->B": the id stops before the arrow
  if (!m) return null;
  const id = m[1];
  let rest = s.slice(id.length);
  let label = id, shape: Shape = "rect", explicit = false;
  for (const [sh, a, b] of SHAPES) {
    if (rest.startsWith(a)) {
      const end = rest.indexOf(b, a.length);
      if (end < 0) return null;
      label = rest.slice(a.length, end).trim().replace(/^"(.*)"$/, "$1");
      shape = sh;
      rest = rest.slice(end + b.length);
      explicit = true;
      break;
    }
  }
  let cls: string | undefined;
  const c = /^:::([\w-]+)/.exec(rest);
  if (c) { cls = c[1]; rest = rest.slice(c[0].length); }
  return { n: { id, label, shape: explicit ? shape : "rect", cls, ...(explicit ? {} : { implicit: true }) } as FNode, rest };
}

// link parses an edge operator (with an optional label) at the start of s.
function link(s: string): { kind: EdgeKind; label: string; rest: string } | null {
  const ops: [RegExp, EdgeKind][] = [
    [/^-\.+->/, "dotted"], [/^-\.+-/, "dotted"], [/^==+>/, "thick"], [/^==+/, "thick"], [/^--+>/, "arrow"], [/^--+-/, "open"],
  ];
  const text = /^--\s+([^-][^>]*?)\s+-->/.exec(s) ?? /^-\.\s+(.+?)\s+\.->/.exec(s) ?? /^==\s+(.+?)\s+==>/.exec(s);
  if (text) {
    const kind: EdgeKind = s.startsWith("-.") ? "dotted" : s.startsWith("==") ? "thick" : "arrow";
    return { kind, label: text[1], rest: s.slice(text[0].length) };
  }
  for (const [re, kind] of ops) {
    const m = re.exec(s);
    if (m) {
      let rest = s.slice(m[0].length);
      let label = "";
      const l = /^\|([^|]*)\|/.exec(rest);
      if (l) { label = l[1].trim().replace(/^"(.*)"$/, "$1"); rest = rest.slice(l[0].length); }
      return { kind, label, rest };
    }
  }
  return null;
}

// subgraphHead parses "subgraph id[Label]", "subgraph id", "subgraph \"Label\"" and
// "subgraph Some title" (a title without an id gets one made from it).
function subgraphHead(rest: string, taken: (id: string) => boolean): { id: string; label: string } {
  rest = rest.trim();
  const withId = /^([A-Za-z0-9_][\w-]*)\s*\[\s*"?(.*?)"?\s*\]$/.exec(rest);
  if (withId) return { id: withId[1], label: withId[2] };
  if (/^[A-Za-z0-9_][\w-]*$/.test(rest)) return { id: rest, label: rest };
  const label = rest.replace(/^"(.*)"$/, "$1");
  const base = label.replace(/[^\w]+/g, "_").replace(/^_+|_+$/g, "") || "group";
  let id = base;
  for (let i = 2; taken(id); i++) id = `${base}_${i}`;
  return { id, label };
}

export function parseFlow(code: string): Flow {
  const lines = code.split("\n");
  let i = 0;
  while (i < lines.length && (!lines[i].trim() || lines[i].trim().startsWith("%%"))) i++;
  const head = /^(?:flowchart|graph)\s*(TD|TB|BT|RL|LR)?\s*;?$/.exec(lines[i]?.trim() ?? "");
  if (!head) throw new Unsupported("Only flowcharts (flowchart TD/LR …) can be edited visually; edit this diagram as code.");
  const flow: Flow = { dir: head[1] ?? "TD", nodes: [], edges: [], extra: [], groups: [] };
  const byId = new Map<string, FNode & { implicit?: boolean }>();
  const stack: FGroup[] = [];
  const unreadable = (line: string) => new Unsupported(`Could not read “${line}”; edit this diagram as code.`);
  const put = (n: FNode & { implicit?: boolean }) => {
    const cur = byId.get(n.id);
    const inGroup = stack[stack.length - 1]?.id;
    if (!cur) {
      const fresh = { ...n, group: inGroup };
      byId.set(n.id, fresh);
      flow.nodes.push(fresh);
      return;
    }
    if (inGroup) cur.group = inGroup; // mentioned inside a subgraph: it belongs there
    if (!n.implicit) {
      cur.label = n.label;
      cur.shape = n.shape;
      cur.implicit = false;
      if (n.cls) cur.cls = n.cls;
    }
  };
  // "A & B" → [A, B]
  const nodeList = (s: string): { ids: string[]; rest: string } | null => {
    const ids: string[] = [];
    let rest = s;
    for (;;) {
      const n = node(rest.trim());
      if (!n) return null;
      put(n.n);
      ids.push(n.n.id);
      rest = n.rest.trim();
      if (!rest.startsWith("&")) return { ids, rest };
      rest = rest.slice(1);
    }
  };
  const pos = new Map<string, number[]>();
  for (const raw of lines.slice(i + 1)) {
    const line = raw.trim().replace(/;$/, "");
    if (!line) continue;
    if (line.startsWith(LAYOUT)) {
      for (const p of line.slice(LAYOUT.length).trim().split(/\s+/)) {
        const m = /^([\w-]+)=(-?\d+(?:,-?\d+){1,3})$/.exec(p);
        if (m) pos.set(m[1], m[2].split(",").map(Number));
      }
      continue;
    }
    if (/^(%%|classDef\s|style\s|class\s|linkStyle\s)/.test(line)) {
      if (line.startsWith("linkStyle")) throw new Unsupported("linkStyle refers to edges by position, so this diagram stays code-only.");
      flow.extra.push(line);
      continue;
    }
    const sub = /^subgraph(?:\s+(.*))?$/.exec(line);
    if (sub) {
      const { id, label } = subgraphHead(sub[1] ?? "", (x) => flow.groups.some((g) => g.id === x) || byId.has(x));
      const g: FGroup = { id, label, parent: stack[stack.length - 1]?.id };
      flow.groups.push(g);
      stack.push(g);
      continue;
    }
    if (line === "end") {
      if (!stack.pop()) throw unreadable(line);
      continue;
    }
    const dir = /^direction\s+(TD|TB|BT|RL|LR)$/.exec(line);
    if (dir) {
      if (stack.length) stack[stack.length - 1].dir = dir[1];
      else flow.dir = dir[1];
      continue;
    }
    if (/^click\s/.test(line)) throw new Unsupported("Click handlers are not supported by the visual editor; edit this diagram as code.");
    // A statement: nodes (link nodes)*, where "nodes" may be "A & B".
    const first = nodeList(line);
    if (!first) throw unreadable(line);
    let prev = first.ids;
    let s = first.rest;
    while (s) {
      const l = link(s);
      if (!l) throw unreadable(line);
      const next = nodeList(l.rest);
      if (!next) throw unreadable(line);
      for (const a of prev) for (const b of next.ids) {
        flow.edges.push({ id: `e${flow.edges.length + 1}`, from: a, to: b, label: l.label, kind: l.kind });
      }
      prev = next.ids;
      s = next.rest;
    }
  }
  if (stack.length) throw new Unsupported("A subgraph is missing its “end”; fix the code first.");
  // An edge to a subgraph names it like a node: that is the group, not a new node.
  const groupIds = new Set(flow.groups.map((g) => g.id));
  flow.nodes = flow.nodes.filter((n) => !(groupIds.has(n.id) && (n as { implicit?: boolean }).implicit));
  for (const n of flow.nodes) {
    delete (n as { implicit?: boolean }).implicit;
    if (!n.group) delete n.group;
    const p = pos.get(n.id);
    if (p) [n.x, n.y] = p;
  }
  for (const g of flow.groups) {
    if (!g.parent) delete g.parent;
    const p = pos.get(g.id);
    if (p && p.length === 4) [g.x, g.y, g.w, g.h] = p;
  }
  return flow;
}

const needsQuotes = (s: string) => /[[\](){}|<>"#;:]/.test(s);
const q = (s: string) => (needsQuotes(s) ? `"${s.replace(/"/g, "#quot;")}"` : s);

export function writeFlow(f: Flow): string {
  const out = [`flowchart ${f.dir}`];
  const nodeLine = (n: FNode, pad: string) => {
    const [a, b] = OPEN[n.shape];
    return `${pad}${n.id}${a}${q(n.label || n.id)}${b}${n.cls ? `:::${n.cls}` : ""}`;
  };
  const groups = f.groups ?? [];
  const known = new Set(groups.map((g) => g.id));
  // Members are written inside their subgraph; nodes of a group that no longer exists
  // go to the top level.
  for (const n of f.nodes) if (!n.group || !known.has(n.group)) out.push(nodeLine(n, "    "));
  const writeGroup = (g: FGroup, depth: number) => {
    const pad = "    ".repeat(depth);
    out.push(`${pad}subgraph ${g.id}${g.label && g.label !== g.id ? `[${q(g.label)}]` : ""}`);
    if (g.dir) out.push(`${pad}    direction ${g.dir}`);
    for (const n of f.nodes) if (n.group === g.id) out.push(nodeLine(n, pad + "    "));
    for (const c of groups) if (c.parent === g.id) writeGroup(c, depth + 1);
    out.push(`${pad}end`);
  };
  for (const g of groups) if (!g.parent || !known.has(g.parent)) writeGroup(g, 1);
  for (const e of f.edges) {
    out.push(`    ${e.from} ${ARROW[e.kind]}${e.label ? `|${q(e.label)}|` : ""} ${e.to}`);
  }
  for (const x of f.extra) out.push(`    ${x}`);
  const placed = [
    ...f.nodes.filter((n) => n.x !== undefined && n.y !== undefined).map((n) => `${n.id}=${Math.round(n.x!)},${Math.round(n.y!)}`),
    ...groups.filter((g) => g.x !== undefined && g.w !== undefined)
      .map((g) => `${g.id}=${Math.round(g.x!)},${Math.round(g.y!)},${Math.round(g.w!)},${Math.round(g.h!)}`),
  ];
  if (placed.length) out.push(`    ${LAYOUT} ${placed.join(" ")}`);
  return out.join("\n");
}

// mermaidBlocks finds ```mermaid fences in Markdown: 0-based line ranges of the fences
// and the code between them.
export type Block = { start: number; end: number; code: string };
export function mermaidBlocks(md: string): Block[] {
  const lines = md.split("\n");
  const out: Block[] = [];
  for (let i = 0; i < lines.length; i++) {
    const open = /^(\s*)(```|~~~)\s*mermaid\s*$/.exec(lines[i]);
    if (!open) continue;
    for (let j = i + 1; j < lines.length; j++) {
      if (lines[j].trim().startsWith(open[2])) {
        out.push({ start: i, end: j, code: lines.slice(i + 1, j).join("\n") });
        i = j;
        break;
      }
    }
  }
  return out;
}
