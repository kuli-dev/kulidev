import dagre from "@dagrejs/dagre";
import { Background, Controls, MarkerType, ReactFlow, ReactFlowProvider, useReactFlow, type Edge, type Node } from "@xyflow/react";
import { useEffect, useMemo, useState } from "react";
import { api, type Graph } from "../api";
import { fmt, useLoad } from "../hooks";
import { href } from "../router";
import { ErrorNote } from "./Overview";

const W = 220;
const H = 62;

function layout(g: Graph, selected?: string): { nodes: Node[]; edges: Edge[]; focus: string[] } {
  const d = new dagre.graphlib.Graph();
  d.setGraph({ rankdir: "LR", nodesep: 26, ranksep: 90, marginx: 20, marginy: 20 });
  d.setDefaultEdgeLabel(() => ({}));
  const comps = g.components ?? [];
  comps.forEach((c) => d.setNode(c.name, { width: W, height: H }));
  (g.edges ?? []).forEach((e) => d.setEdge(e.from, e.to));
  dagre.layout(d);

  const near = new Set<string>();
  if (selected) {
    (g.edges ?? []).forEach((e) => {
      if (e.from === selected) near.add(e.to);
      if (e.to === selected) near.add(e.from);
    });
  }
  const maxN = Math.max(1, ...(g.edges ?? []).map((e) => e.n));
  const nodes: Node[] = comps.map((c) => {
    const p = d.node(c.name);
    const state = !selected ? "" : c.name === selected ? "is-selected" : near.has(c.name) ? "is-near" : "is-dim";
    return {
      id: c.name,
      position: { x: p.x - W / 2, y: p.y - H / 2 },
      data: { label: <NodeLabel name={c.name} files={c.files} lang={c.lang} /> },
      className: `comp-node ${state}`,
      style: { width: W },
    };
  });
  const edges: Edge[] = (g.edges ?? []).map((e) => {
    const touches = selected && (e.from === selected || e.to === selected);
    return {
      id: `${e.from}->${e.to}`,
      source: e.from,
      target: e.to,
      label: touches ? `${e.n}` : undefined,
      className: selected ? (touches ? "edge-hot" : "edge-dim") : "",
      style: { strokeWidth: 1 + 3 * Math.sqrt(e.n / maxN) },
      markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
    };
  });
  return { nodes, edges, focus: selected ? [selected, ...near] : [] };
}

const reduceMotion = () => matchMedia("(prefers-reduced-motion: reduce)").matches;

// Zoom to the selected component and its neighbours; with nothing selected, show the
// whole map but never so small that labels become unreadable specks.
function FitOnSelect({ focus, ready }: { focus: string[]; ready: boolean }) {
  const rf = useReactFlow();
  const key = focus.join("|");
  useEffect(() => {
    if (!ready) return;
    const opts = { padding: 0.3, maxZoom: 1.1, duration: reduceMotion() ? 0 : 350 };
    const t = setTimeout(() => rf.fitView(focus.length ? { ...opts, nodes: focus.map((id) => ({ id })) } : { ...opts, minZoom: 0.45 }), 0);
    return () => clearTimeout(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, ready]);
  return null;
}

function NodeLabel({ name, files, lang }: { name: string; files: number; lang: string }) {
  const parts = name.split("/");
  const leaf = parts.pop();
  return (
    <div className="node-label">
      {parts.length > 0 && <span className="node-parent">{parts.join("/")}/</span>}
      <strong>{leaf}</strong>
      <span className="node-meta">
        {fmt(files)} files, {lang}
      </span>
    </div>
  );
}

export function MapView({ selected }: { selected?: string }) {
  const [limit, setLimit] = useState(20);
  const g = useLoad(() => api.graph(limit), [limit]);
  const flow = useMemo(() => (g.data ? layout(g.data, selected) : null), [g.data, selected]);

  return (
    <div className="map-page">
      <div className="map-head">
        <h1 className="title">Map</h1>
        <p className="muted small">
          Directories as components; an arrow means files in one import the other. Click a component for details.
        </p>
        {g.data && g.data.total > 12 && (
          <label className="small">
            Show the {limit} largest of {g.data.total}{" "}
            <input type="range" min={8} max={Math.min(120, g.data.total)} step={4} value={Math.min(limit, g.data.total)}
              onChange={(e) => setLimit(+e.target.value)} aria-label="Number of components shown" />
          </label>
        )}
      </div>
      {g.error && <ErrorNote msg={g.error} />}
      <div className="map-body">
        <div className="map-canvas">
          {flow && (
            <ReactFlowProvider>
            <ReactFlow
              nodes={flow.nodes}
              edges={flow.edges}
              minZoom={0.15}
              nodesDraggable={false}
              nodesConnectable={false}
              onNodeClick={(_, n) => (location.hash = href.map(n.id === selected ? undefined : n.id))}
              onPaneClick={() => selected && (location.hash = href.map())}
              proOptions={{ hideAttribution: true }}
            >
              <Background gap={24} size={1} />
              <Controls showInteractive={false} />
              <FitOnSelect focus={flow.focus} ready={flow.nodes.length > 0} />
            </ReactFlow>
            </ReactFlowProvider>
          )}
        </div>
        {selected && <ComponentPanel name={selected} />}
      </div>
    </div>
  );
}

function ComponentPanel({ name }: { name: string }) {
  const c = useLoad(() => api.component(name), [name]);
  if (c.error) return <aside className="panel"><ErrorNote msg={c.error} /></aside>;
  if (!c.data) return <aside className="panel muted">Loading…</aside>;
  const d = c.data;
  const rel = (m: Record<string, number>) => Object.entries(m).sort((a, b) => b[1] - a[1]).slice(0, 8);
  return (
    <aside className="panel" aria-label={`Component ${name}`}>
      <h2>{d.name}</h2>
      <p className="muted small">{fmt(d.files.length)} files, {fmt(d.lines)} lines, mostly {d.lang}</p>

      {rel(d.dependsOn).length > 0 && (
        <>
          <h3>Depends on</h3>
          <ul className="plain">
            {rel(d.dependsOn).map(([k, n]) => (
              <li key={k}><a href={href.map(k)}>{k}</a> <span className="muted">{n} files</span></li>
            ))}
          </ul>
        </>
      )}
      {rel(d.usedBy).length > 0 && (
        <>
          <h3>Used by</h3>
          <ul className="plain">
            {rel(d.usedBy).map(([k, n]) => (
              <li key={k}><a href={href.map(k)}>{k}</a> <span className="muted">{n} files</span></li>
            ))}
          </ul>
        </>
      )}

      <h3>Files</h3>
      <ul className="plain files">
        {d.files.slice(0, 40).map((f) => (
          <li key={f.path}>
            <a href={href.file(f.path)}>{f.path.slice(d.name.length + 1) || f.path}</a>
            {f.importers > 0 && <span className="muted"> {f.importers} importers</span>}
            {f.doc && <p className="doc">{f.doc}</p>}
          </li>
        ))}
      </ul>
      {d.files.length > 40 && <p className="muted small">and {d.files.length - 40} more</p>}
    </aside>
  );
}
