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
check("labels that need quotes", writeFlow({ dir: "TD", nodes: [{ id: "A", label: "a [b]", shape: "rect" }], edges: [], extra: [] }).includes('A["a [b]"]'));
const codeOnly = (src: string) => { try { parseFlow(src); return false; } catch (e) { return e instanceof Unsupported; } };
check("sequence diagrams stay code-only", codeOnly("sequenceDiagram\n A->>B: hi"));
check("subgraphs stay code-only", codeOnly("flowchart TD\n subgraph x\n A-->B\n end"));
const bl = mermaidBlocks("# t\n```mermaid\ngraph TD\nA-->B\n```\ntext\n~~~mermaid\nflowchart LR\nX\n~~~\n");
check("finds ``` and ~~~ blocks", bl.length === 2 && bl[0].start === 1 && bl[0].end === 4 && bl[1].code === "flowchart LR\nX", bl);

if (failed) process.exit(1);
