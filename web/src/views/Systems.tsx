import dagre from "@dagrejs/dagre";
import { Background, Controls, MarkerType, ReactFlow, ReactFlowProvider, type Edge, type Node } from "@xyflow/react";
import { useMemo, useState } from "react";
import { api, type SysLink, type SysProject, type SystemsMap } from "../api";
import { fmt, useLoad } from "../hooks";
import { href } from "../router";
import { ErrorNote } from "./Overview";

const W = 230;
const H = 74;
const KINDS = ["imports", "calls API", "depends on", "configures"] as const;
const KIND_TEXT: Record<SysLink["kind"], string> = {
  imports: "imports code from",
  "calls API": "calls HTTP routes of",
  "depends on": "declares a dependency on",
  configures: "deploy or config files refer to",
};
const id = (path: string) => path || ".";

// One edge per pair of projects; its kinds and counts make the label.
type Pair = { from: string; to: string; links: SysLink[]; total: number };

function pairs(m: SystemsMap): Pair[] {
  const by = new Map<string, Pair>();
  for (const l of m.links) {
    const k = `${l.from}\u0000${l.to}`;
    const p = by.get(k) ?? { from: l.from, to: l.to, links: [], total: 0 };
    p.links.push(l);
    p.total += l.count;
    by.set(k, p);
  }
  return [...by.values()];
}

function layout(m: SystemsMap, ps: Pair[], sel: Sel): { nodes: Node[]; edges: Edge[] } {
  const d = new dagre.graphlib.Graph();
  d.setGraph({ rankdir: "TB", nodesep: 70, ranksep: 120, marginx: 30, marginy: 30 });
  d.setDefaultEdgeLabel(() => ({}));
  m.projects.forEach((p) => d.setNode(id(p.path), { width: W, height: H }));
  ps.forEach((p) => d.setEdge(id(p.from), id(p.to)));
  dagre.layout(d);
  const selNode = sel?.type === "project" ? sel.path : undefined;
  const near = new Set<string>();
  ps.forEach((p) => {
    if (p.from === selNode) near.add(p.to);
    if (p.to === selNode) near.add(p.from);
  });
  const nodes: Node[] = m.projects.map((p) => {
    const pos = d.node(id(p.path));
    const state = selNode === undefined && sel?.type !== "pair" ? "" : p.path === selNode ? "is-selected"
      : near.has(p.path) || (sel?.type === "pair" && (sel.from === p.path || sel.to === p.path)) ? "is-near" : "is-dim";
    return {
      id: id(p.path), position: { x: pos.x - W / 2, y: pos.y - H / 2 },
      data: { label: <ProjectLabel p={p} /> }, className: `comp-node sys-node ${p.path === "" ? "is-folder" : ""} ${state}`, style: { width: W },
    };
  });
  const max = Math.max(1, ...ps.map((p) => p.total));
  const edges: Edge[] = [];
  const done = new Set<string>();
  for (const p of ps) {
    const k = [p.from, p.to].sort().join("\u0000");
    if (done.has(k)) continue;
    done.add(k);
    const back = ps.find((q) => q.from === p.to && q.to === p.from);
    const all = back ? [p, back] : [p];
    const links = all.flatMap((x) => x.links);
    const total = all.reduce((a, x) => a + x.total, 0);
    const code = links.some((l) => l.kind !== "configures");
    const hot = (sel?.type === "pair" && all.some((x) => x.from === sel.from && x.to === sel.to))
      || (selNode !== undefined && (p.from === selNode || p.to === selNode));
    const label = hot
      ? all.map((x) => (back ? `${x === p ? "→" : "←"} ` : "") + x.links.map((l) => `${l.kind} ${fmt(l.count)}`).join(" · ")).join("   ")
      : fmt(total);
    const arrow = { type: MarkerType.ArrowClosed, width: 14, height: 14 };
    edges.push({
      id: `${id(p.from)}->${id(p.to)}`, source: id(p.from), target: id(p.to), label,
      className: `sys-edge ${code ? "is-code" : "is-config"} ${sel ? (hot ? "edge-hot" : "edge-dim") : ""}`,
      style: { strokeWidth: 1.2 + 3 * Math.sqrt(total / max) },
      markerEnd: arrow, markerStart: back ? arrow : undefined,
      labelBgPadding: [6, 3] as [number, number],
    });
  }
  return { nodes, edges };
}

function ProjectLabel({ p }: { p: SysProject }) {
  return (
    <div className="node-label">
      <span className="node-parent">{p.path === "" ? "this folder" : p.git ? "repository" : p.markers[0] ?? "project"}</span>
      <strong>{p.name}</strong>
      <span className="node-meta">
        {fmt(p.files)} files{p.langs.length > 0 && `, ${p.langs.slice(0, 2).map((l) => l.lang).join(", ")}`}
      </span>
    </div>
  );
}

type Sel = { type: "project"; path: string } | { type: "pair"; from: string; to: string } | null;

export function SystemsView() {
  const m = useLoad(api.systems, []);
  const [sel, setSel] = useState<Sel>(null);
  const ps = useMemo(() => (m.data ? pairs(m.data) : []), [m.data]);
  const flow = useMemo(() => (m.data ? layout(m.data, ps, sel) : null), [m.data, ps, sel]);
  const name = (path: string) => m.data?.projects.find((p) => p.path === path)?.name ?? path;

  return (
    <div className="map-page">
      <div className="map-head">
        <h1 className="title">Systems</h1>
        <p className="muted small">
          The projects in this folder and how they connect. Solid arrows: code imports, HTTP calls to another project's routes, or declared dependencies. Dashed:
          deploy and config files that refer to another project. Click anything to see the evidence.
        </p>
      </div>
      {m.error && <ErrorNote msg={m.error} />}
      {m.data && m.data.projects.length < 2 && (
        <p className="pad muted">This folder holds a single project. Open a folder that contains several repositories to see how they relate.</p>
      )}
      {m.data && m.data.projects.length >= 2 && (
        <div className="map-body">
          <div className="map-canvas">
            {flow && (
              <ReactFlowProvider>
                <ReactFlow nodes={flow.nodes} edges={flow.edges} fitView fitViewOptions={{ padding: 0.25, maxZoom: 1.1 }} minZoom={0.2}
                  nodesDraggable={false} nodesConnectable={false} proOptions={{ hideAttribution: true }}
                  onNodeClick={(_, n) => {
                    const path = n.id === "." ? "" : n.id;
                    setSel((s) => (s?.type === "project" && s.path === path ? null : { type: "project", path }));
                  }}
                  onEdgeClick={(_, e) => setSel({ type: "pair", from: e.source === "." ? "" : e.source, to: e.target === "." ? "" : e.target })}
                  onPaneClick={() => setSel(null)}>
                  <Background gap={24} size={1} />
                  <Controls showInteractive={false} />
                </ReactFlow>
              </ReactFlowProvider>
            )}
          </div>
          {sel?.type === "project" && <ProjectPanel m={m.data} path={sel.path} ps={ps} name={name} onPair={(from, to) => setSel({ type: "pair", from, to })} />}
          {sel?.type === "pair" && (
            <PairPanel pairs={ps.filter((p) => (p.from === sel.from && p.to === sel.to) || (p.from === sel.to && p.to === sel.from))} name={name} />
          )}
          {!sel && <Legend m={m.data} ps={ps} name={name} onPair={(from, to) => setSel({ type: "pair", from, to })} />}
        </div>
      )}
    </div>
  );
}

function Legend({ m, ps, name, onPair }: { m: SystemsMap; ps: Pair[]; name: (p: string) => string; onPair: (f: string, t: string) => void }) {
  return (
    <aside className="panel" aria-label="Connections">
      <h2>{m.projects.length} projects, {ps.length} connections</h2>
      {ps.length === 0 && <p className="muted small">No connections found: no shared imports, dependencies or config references.</p>}
      <ul className="plain sys-pairs">
        {[...ps].sort((a, b) => b.total - a.total).map((p) => (
          <li key={`${p.from}>${p.to}`}>
            <button className="btn-link" onClick={() => onPair(p.from, p.to)}>{name(p.from)} → {name(p.to)}</button>
            <span className="muted small"> {p.links.map((l) => `${l.kind} ${fmt(l.count)}`).join(", ")}</span>
          </li>
        ))}
      </ul>
    </aside>
  );
}

function ProjectPanel({ m, path, ps, name, onPair }: {
  m: SystemsMap; path: string; ps: Pair[]; name: (p: string) => string; onPair: (f: string, t: string) => void;
}) {
  const p = m.projects.find((x) => x.path === path);
  if (!p) return null;
  const out = ps.filter((x) => x.from === path);
  const inn = ps.filter((x) => x.to === path);
  const total = p.langs.reduce((a, l) => a + l.lines, 0) || 1;
  return (
    <aside className="panel" aria-label={`Project ${p.name}`}>
      <h2>{p.name}</h2>
      <p className="muted small">
        {p.path === "" ? "Files in the folder itself" : p.path}
        {p.git && " · git repository"}
      </p>
      <p className="small">{fmt(p.files)} files, {fmt(p.lines)} lines</p>
      {p.langs.length > 0 && (
        <p className="small">{p.langs.map((l) => { const pc = (l.lines / total) * 100; return `${l.lang} ${pc < 1 ? "<1" : Math.round(pc)}%`; }).join(", ")}</p>
      )}
      {p.markers.length > 0 && <p className="small muted">Project files: {p.markers.join(", ")}</p>}
      {p.packages.length > 0 && (
        <p className="small muted">Imported as: {p.packages.map((k) => k.replace(/^pydist:/, "py package ").replace(/^(\w+):/, "$1 ")).join(", ")}</p>
      )}
      {out.length > 0 && (
        <>
          <h3>Uses</h3>
          <ul className="plain">
            {out.map((x) => (
              <li key={x.to}>
                <button className="btn-link" onClick={() => onPair(x.from, x.to)}>{name(x.to)}</button>
                <span className="muted small"> {x.links.map((l) => l.kind).join(", ")}</span>
              </li>
            ))}
          </ul>
        </>
      )}
      {inn.length > 0 && (
        <>
          <h3>Used by</h3>
          <ul className="plain">
            {inn.map((x) => (
              <li key={x.from}>
                <button className="btn-link" onClick={() => onPair(x.from, x.to)}>{name(x.from)}</button>
                <span className="muted small"> {x.links.map((l) => l.kind).join(", ")}</span>
              </li>
            ))}
          </ul>
        </>
      )}
      {out.length + inn.length === 0 && <p className="muted small">No connections to the other projects were found.</p>}
    </aside>
  );
}

function PairPanel({ pairs, name }: { pairs: Pair[]; name: (p: string) => string }) {
  if (pairs.length === 0) return null;
  return (
    <aside className="panel" aria-label="Connection evidence">
      <h2>{name(pairs[0].from)} {pairs.length > 1 ? "↔" : "→"} {name(pairs[0].to)}</h2>
      {pairs.map((pair) => <PairLinks key={`${pair.from}>${pair.to}`} pair={pair} name={name} />)}
    </aside>
  );
}

function PairLinks({ pair, name }: { pair: Pair; name: (p: string) => string }) {
  return (
    <>
      {KINDS.map((k) => pair.links.find((l) => l.kind === k)).filter(Boolean).map((l) => (
        <section key={l!.kind}>
          <h3>{name(pair.from)} {KIND_TEXT[l!.kind]} {name(pair.to)}</h3>
          <p className="muted small">{fmt(l!.count)} {l!.count === 1 ? "place" : "places"}{l!.count > l!.evidence.length && `, first ${l!.evidence.length} shown`}</p>
          <ul className="plain evidence">
            {l!.evidence.map((e) => (
              <li key={`${e.path}:${e.line}`}>
                <a href={href.file(e.path, e.line, e.line)}>{e.path}:{e.line}</a>
                {e.text && <code>{e.text}</code>}
                {e.target && (
                  <span className="ev-target">
                    reaches <a href={href.file(e.target.split(":")[0], +e.target.split(":")[1].split(" ")[0])}>{e.target.split("  ")[0]}</a>
                    <code>{e.target.split("  ").slice(1).join("  ")}</code>
                  </span>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </>
  );
}
