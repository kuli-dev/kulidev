import { Background, Controls, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, type Edge, type Node, type NodeProps } from "@xyflow/react";
import { useMemo, useState } from "react";
import type { SysService, SystemsMap } from "../api";
import { href } from "../router";

// Services: what gets deployed and run (docker-compose services, Kubernetes workloads,
// images), in a box for the project whose code they run, with backing services
// (databases, caches, queues) apart, and arrows where one depends on or calls another.

const W = 200;
const H = 54;
const BACKING = new Set(["database", "cache", "queue", "storage", "proxy"]);
const KIND_LABEL: Record<SysService["kind"], string> = {
  api: "API", worker: "worker", cronjob: "cron job", job: "job", frontend: "front end",
  database: "database", cache: "cache", queue: "queue", storage: "storage", proxy: "proxy",
};
const BACKING_BOX = "\u0000backing";
const OTHER_BOX = "\u0000other";

function ServiceNode({ data, selected }: NodeProps<Node<{ s: SysService; state: string }>>) {
  const s = data.s;
  return (
    <div className={`svc-node svc-${s.kind} ${selected ? "is-sel" : ""} ${data.state}`}>
      <span className="svc-kind">{KIND_LABEL[s.kind]}{s.ports.length > 0 && ` :${s.ports.slice(0, 2).join(", :")}`}</span>
      <strong>{s.name}</strong>
      {s.envs.length > 0 && <span className="svc-envs">{s.envs.join(" · ")}</span>}
      <Handle type="target" position={Position.Left} className="svc-h" isConnectable={false} />
      <Handle type="source" position={Position.Right} className="svc-h" isConnectable={false} />
    </div>
  );
}
function BoxNode({ data }: NodeProps<Node<{ label: string; w: number; h: number }>>) {
  return <div className="svc-box" style={{ width: data.w, height: data.h }}><span>{data.label}</span></div>;
}
const nodeTypes = { svc: ServiceNode, box: BoxNode };

const ORDER: SysService["kind"][] = ["frontend", "api", "worker", "cronjob", "job", "proxy", "database", "cache", "queue", "storage"];
const GAP = 14;
const HEAD = 30;
const PAD = 14;
const ROW_WIDTH = 1500; // boxes wrap onto a new row past this

// A grid: services in rows inside their project's box, boxes wrapping across the page.
// (Few arrows connect services, so a graph layout would only stack them in a column.)
function layout(m: SystemsMap, services: SysService[], sel: string | null) {
  const links = (m.serviceLinks ?? []).filter((l) => services.some((s) => s.name === l.from) && services.some((s) => s.name === l.to));
  const boxOf = (s: SysService) => (BACKING.has(s.kind) ? BACKING_BOX : s.project || OTHER_BOX);
  const name = (p: string) => p === BACKING_BOX ? "Backing services" : p === OTHER_BOX ? "Other images" : m.projects.find((x) => x.path === p)?.name ?? p;
  const groups = new Map<string, SysService[]>();
  for (const s of services) groups.set(boxOf(s), [...(groups.get(boxOf(s)) ?? []), s]);
  const boxes = [...groups.keys()].sort((a, b) =>
    a === BACKING_BOX ? 1 : b === BACKING_BOX ? -1 : groups.get(b)!.length - groups.get(a)!.length || name(a).localeCompare(name(b)));

  const near = new Set<string>();
  links.forEach((l) => { if (l.from === sel) near.add(l.to); if (l.to === sel) near.add(l.from); });
  const nodes: Node[] = [];
  let x = 0, y = 0, rowH = 0;
  for (const box of boxes) {
    const members = groups.get(box)!.sort((a, b) => ORDER.indexOf(a.kind) - ORDER.indexOf(b.kind) || a.name.localeCompare(b.name));
    const cols = Math.min(members.length, Math.max(1, Math.ceil(Math.sqrt(members.length * 1.6))));
    const rows = Math.ceil(members.length / cols);
    const w = PAD * 2 + cols * W + (cols - 1) * GAP;
    const h = HEAD + PAD + rows * H + (rows - 1) * GAP;
    if (x > 0 && x + w > ROW_WIDTH) { x = 0; y += rowH + 40; rowH = 0; }
    nodes.push({ id: `box:${box}`, type: "box", position: { x, y }, data: { label: `${name(box)} (${members.length})`, w, h },
      selectable: false, draggable: false, zIndex: -1 });
    members.forEach((s, i) => {
      const state = !sel ? "" : s.name === sel ? "" : near.has(s.name) ? "is-near" : "is-dim";
      nodes.push({ id: s.name, type: "svc", position: { x: x + PAD + (i % cols) * (W + GAP), y: y + HEAD + Math.floor(i / cols) * (H + GAP) },
        data: { s, state }, selected: s.name === sel, zIndex: 1 });
    });
    x += w + 40;
    rowH = Math.max(rowH, h);
  }
  const edges: Edge[] = links.map((l) => {
    const hot = sel && (l.from === sel || l.to === sel);
    return {
      id: `${l.from}>${l.to}>${l.kind}`, source: l.from, target: l.to, zIndex: 2,
      className: `svc-edge ${l.kind === "calls" ? "is-call" : "is-dep"} ${sel ? (hot ? "edge-hot" : "edge-dim") : ""}`,
      label: hot ? l.kind : undefined, markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
    };
  });
  return { nodes, edges };
}

export function ServicesView({ m }: { m: SystemsMap }) {
  const [sel, setSel] = useState<string | null>(null);
  const [jobs, setJobs] = useState(true);
  const all = m.services ?? [];
  const services = useMemo(() => all.filter((s) => jobs || (s.kind !== "cronjob" && s.kind !== "job")), [all, jobs]);
  const flow = useMemo(() => layout(m, services, sel), [m, services, sel]);
  const s = all.find((x) => x.name === sel);
  const project = (p: string) => m.projects.find((x) => x.path === p)?.name ?? p;
  const count = (k: string[]) => all.filter((x) => k.includes(x.kind)).length;

  if (all.length === 0) {
    return <p className="pad muted">No services found: no docker-compose services, Kubernetes workloads or Dockerfiles in this folder.</p>;
  }
  return (
    <div className="map-body">
      <div className="map-canvas svc-canvas">
        <div className="svc-legend small">
          <span>{count(["api", "frontend"])} APIs and front ends, {count(["worker"])} workers, {count(["cronjob", "job"])} jobs, {count([...BACKING])} backing services</span>
          <label className="check"><input type="checkbox" checked={jobs} onChange={(e) => setJobs(e.target.checked)} /> Show cron jobs and jobs</label>
        </div>
        <ReactFlowProvider>
          <ReactFlow nodes={flow.nodes} edges={flow.edges} nodeTypes={nodeTypes} fitView fitViewOptions={{ padding: 0.15, maxZoom: 1.1 }} minZoom={0.1}
            nodesDraggable={false} nodesConnectable={false} proOptions={{ hideAttribution: true }}
            onNodeClick={(_, n) => n.type === "svc" && setSel((cur) => (cur === n.id ? null : n.id))} onPaneClick={() => setSel(null)}>
            <Background gap={24} size={1} />
            <Controls showInteractive={false} />
          </ReactFlow>
        </ReactFlowProvider>
      </div>
      {s && (
        <aside className="panel" aria-label={`Service ${s.name}`}>
          <h2>{s.name}</h2>
          <p className="small">{KIND_LABEL[s.kind]}{s.project ? <> running {project(s.project)}'s code</> : s.image ? <> from the stock image <code>{s.image}</code></> : null}</p>
          <ul className="plain set-facts">
            {s.envs.length > 0 && <li><span>Runs in</span>{s.envs.join(", ")}</li>}
            {s.deployed && <li><span>Deployed from</span>{project(s.deployed)}</li>}
            {s.image && <li><span>Image</span><code>{s.image}</code></li>}
            {s.ports.length > 0 && <li><span>Ports</span>{s.ports.join(", ")}</li>}
            {s.command && <li><span>Command</span><code>{s.command}</code></li>}
            <li><span>Found in</span>{s.via.join(", ")}</li>
          </ul>
          <h3>Defined at</h3>
          <ul className="plain evidence">
            {s.sources.map((e) => (
              <li key={`${e.path}:${e.line}`}><a href={href.file(e.path, e.line, e.line)}>{e.path}:{e.line}</a>{e.text && <code>{e.text}</code>}</li>
            ))}
          </ul>
          {(m.serviceLinks ?? []).filter((l) => l.from === s.name || l.to === s.name).map((l) => (
            <section key={`${l.from}>${l.to}>${l.kind}`}>
              <h3>{l.from === s.name ? <>{l.kind} <button className="btn-link" onClick={() => setSel(l.to)}>{l.to}</button></> : <><button className="btn-link" onClick={() => setSel(l.from)}>{l.from}</button> {l.kind} this</>}</h3>
              <ul className="plain evidence">
                {l.evidence.slice(0, 3).map((e) => (
                  <li key={`${e.path}:${e.line}`}><a href={href.file(e.path, e.line, e.line)}>{e.path}:{e.line}</a>{e.text && <code>{e.text}</code>}</li>
                ))}
              </ul>
            </section>
          ))}
        </aside>
      )}
    </div>
  );
}
