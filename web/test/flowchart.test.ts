// Run: npm test (bundled with esbuild, run with node).
import { Unsupported, mermaidBlocks, parseFlow, writeFlow } from "../src/flowchart";

let failed = 0;
const check = (name: string, ok: boolean, got?: unknown) => {
  if (!ok) { failed++; console.log("FAIL", name, JSON.stringify(got)); } else console.log("ok  ", name);
};

const f = parseFlow(`flowchart LR
  A[Start] --> B{Is it ok?}
  B -->|yes| C(Done):::good
  B -- no --> D[(DB)]
  C-->D
  D -.-> E((End)) ==> F
  classDef good fill:#0f0
  %% a comment`);
check("direction", f.dir === "LR", f.dir);
check("nodes and shapes", f.nodes.map((n) => `${n.id}:${n.shape}:${n.label}`).join(" ") === "A:rect:Start B:diamond:Is it ok? C:round:Done D:cylinder:DB E:circle:End F:rect:F", f.nodes);
check("edges, labels, styles", f.edges.map((e) => `${e.from}${e.kind}${e.to}:${e.label}`).join(" ") === "AarrowB: BarrowC:yes BarrowD:no CarrowD: DdottedE: EthickF:", f.edges);
check("classes and styling kept", f.nodes[2].cls === "good" && f.extra.includes("classDef good fill:#0f0"), f);
f.nodes[0].x = 10;
f.nodes[0].y = 20;
const out = writeFlow(f);
check("round trip with positions", JSON.stringify(parseFlow(out)) === JSON.stringify(f), out);
check("labels that need quotes", writeFlow({ dir: "TD", nodes: [{ id: "A", label: "a [b]", shape: "rect" }], edges: [], extra: [], groups: [] }).includes('A["a [b]"]'));
const codeOnly = (src: string) => { try { parseFlow(src); return false; } catch (e) { return e instanceof Unsupported; } };
check("sequence diagrams stay code-only", codeOnly("sequenceDiagram\n A->>B: hi"));
check("click handlers stay code-only", codeOnly("flowchart TD\n A-->B\n click A callback"));
check("a subgraph without end is refused", codeOnly("flowchart TD\n subgraph x\n A-->B"));

const g = parseFlow(`flowchart LR
  subgraph api[API layer]
    direction TB
    R[Router] --> S[Service]
    subgraph db [Storage]
      P[(Postgres)]
    end
  end
  subgraph Web front end
    U[UI]
  end
  U --> R
  S --> P
  W[Worker] --> api
  A & B --> C`);
check("groups, nesting, titles", g.groups.map((x) => `${x.id}:${x.label}:${x.parent ?? "-"}:${x.dir ?? "-"}`).join(" ") === "api:API layer:-:TB db:Storage:api:- Web_front_end:Web front end:-:-", g.groups);
check("membership", g.nodes.map((n) => `${n.id}@${n.group ?? "-"}`).join(" ") === "R@api S@api P@db U@Web_front_end W@- A@- B@- C@-", g.nodes);
check("edge to a subgraph is not a node", g.edges.some((e) => e.from === "W" && e.to === "api") && !g.nodes.some((n) => n.id === "api"), g);
check("& chains expand", g.edges.filter((e) => e.to === "C").map((e) => e.from).join(",") === "A,B", g.edges);
g.groups[1].x = 5; g.groups[1].y = 6; g.groups[1].w = 300; g.groups[1].h = 120;
const gout = writeFlow(g);
// Same diagram (order aside), and writing is stable.
const norm = (f: ReturnType<typeof parseFlow>) => JSON.stringify({
  dir: f.dir,
  nodes: [...f.nodes].sort((a, b) => a.id.localeCompare(b.id)),
  edges: f.edges.map((e) => `${e.from}>${e.to}:${e.kind}:${e.label}`).sort(),
  groups: [...f.groups].sort((a, b) => a.id.localeCompare(b.id)),
  extra: f.extra,
});
check("subgraphs round trip", norm(parseFlow(gout)) === norm(g), gout);
check("writing is stable", writeFlow(parseFlow(gout)) === gout, writeFlow(parseFlow(gout)));
const bl = mermaidBlocks("# t\n```mermaid\ngraph TD\nA-->B\n```\ntext\n~~~mermaid\nflowchart LR\nX\n~~~\n");
check("finds ``` and ~~~ blocks", bl.length === 2 && bl[0].start === 1 && bl[0].end === 4 && bl[1].code === "flowchart LR\nX", bl);

if (failed) process.exit(1);
