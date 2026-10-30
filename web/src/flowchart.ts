// A Mermaid flowchart as nodes and edges, for the visual editor: parse the common
// syntax, write it back. Lines the editor does not model (classDef, style, class,
// comments) are kept as they are; diagrams using constructs it cannot round-trip
// (subgraphs, "&" chains, click handlers) are reported so they stay code-only.

export type Shape = "rect" | "round" | "stadium" | "circle" | "diamond" | "hexagon" | "cylinder" | "subroutine";
export type EdgeKind = "arrow" | "open" | "dotted" | "thick";
export type FNode = { id: string; label: string; shape: Shape; cls?: string; x?: number; y?: number };
export type FEdge = { id: string; from: string; to: string; label: string; kind: EdgeKind };
export type Flow = { dir: string; nodes: FNode[]; edges: FEdge[]; extra: string[] };

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

export function parseFlow(code: string): Flow {
  const lines = code.split("\n");
  let i = 0;
  while (i < lines.length && (!lines[i].trim() || lines[i].trim().startsWith("%%"))) i++;
  const head = /^(?:flowchart|graph)\s*(TD|TB|BT|RL|LR)?\s*;?$/.exec(lines[i]?.trim() ?? "");
  if (!head) throw new Unsupported("Only flowcharts (flowchart TD/LR …) can be edited visually; edit this diagram as code.");
  const flow: Flow = { dir: head[1] ?? "TD", nodes: [], edges: [], extra: [] };
  const byId = new Map<string, FNode>();
  const put = (n: FNode & { implicit?: boolean }) => {
    const cur = byId.get(n.id);
    if (!cur) {
      const { implicit, ...clean } = n;
      void implicit;
      byId.set(n.id, clean);
      flow.nodes.push(clean);
    } else if (!n.implicit) {
      cur.label = n.label;
      cur.shape = n.shape;
      if (n.cls) cur.cls = n.cls;
    }
  };
  const pos = new Map<string, [number, number]>();
  for (const raw of lines.slice(i + 1)) {
    const line = raw.trim().replace(/;$/, "");
    if (!line) continue;
    if (line.startsWith(LAYOUT)) {
      for (const p of line.slice(LAYOUT.length).trim().split(/\s+/)) {
        const m = /^([\w-]+)=(-?\d+),(-?\d+)$/.exec(p);
        if (m) pos.set(m[1], [+m[2], +m[3]]);
      }
      continue;
    }
    if (/^(%%|classDef\s|style\s|class\s|linkStyle\s)/.test(line)) {
      if (line.startsWith("linkStyle")) throw new Unsupported("linkStyle refers to edges by position, so this diagram stays code-only.");
      flow.extra.push(line);
      continue;
    }
    if (/^(subgraph|end$|click\s|direction\s)/.test(line) || line.includes(" & ")) {
      throw new Unsupported("Subgraphs, click handlers and “&” chains are not supported by the visual editor yet; edit this diagram as code.");
    }
    // A statement: node (link node)*
    let s = line;
    const first = node(s);
    if (!first) throw new Unsupported(`Could not read “${line}”; edit this diagram as code.`);
    put(first.n);
    let prev = first.n.id;
    s = first.rest.trim();
    while (s) {
      const l = link(s);
      if (!l) throw new Unsupported(`Could not read “${line}”; edit this diagram as code.`);
      const next = node(l.rest.trim());
      if (!next) throw new Unsupported(`Could not read “${line}”; edit this diagram as code.`);
      put(next.n);
      flow.edges.push({ id: `e${flow.edges.length + 1}`, from: prev, to: next.n.id, label: l.label, kind: l.kind });
      prev = next.n.id;
      s = next.rest.trim();
    }
  }
  for (const n of flow.nodes) {
    const p = pos.get(n.id);
    if (p) [n.x, n.y] = p;
  }
  return flow;
}

const needsQuotes = (s: string) => /[[\](){}|<>"#;:]/.test(s);
const q = (s: string) => (needsQuotes(s) ? `"${s.replace(/"/g, "#quot;")}"` : s);

export function writeFlow(f: Flow): string {
  const out = [`flowchart ${f.dir}`];
  for (const n of f.nodes) {
    const [a, b] = OPEN[n.shape];
    out.push(`    ${n.id}${a}${q(n.label || n.id)}${b}${n.cls ? `:::${n.cls}` : ""}`);
  }
  for (const e of f.edges) {
    out.push(`    ${e.from} ${ARROW[e.kind]}${e.label ? `|${q(e.label)}|` : ""} ${e.to}`);
  }
  for (const x of f.extra) out.push(`    ${x}`);
  const placed = f.nodes.filter((n) => n.x !== undefined && n.y !== undefined);
  if (placed.length) out.push(`    ${LAYOUT} ${placed.map((n) => `${n.id}=${Math.round(n.x!)},${Math.round(n.y!)}`).join(" ")}`);
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
