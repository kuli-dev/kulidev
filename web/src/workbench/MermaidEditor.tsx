import dagre from "@dagrejs/dagre";
import {
  Background, ConnectionMode, Controls, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, applyEdgeChanges, applyNodeChanges,
  useReactFlow, type Connection, type Edge, type EdgeChange, type Node, type NodeChange, type NodeProps,
} from "@xyflow/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type EdgeKind, type FEdge, type FGroup, type FNode, type Flow, type Shape, parseFlow, writeFlow } from "../flowchart";
import { cleanSvg } from "../markdown";
import { renderMermaid } from "../mermaid";

// The Mermaid editor: code, a visual canvas (flowcharts) and a live preview, kept in
// sync. On the canvas: drag shapes in from the palette, drag nodes to arrange them,
// drag from a node's edge to another node to connect, select and press Delete to
// remove. Subgraphs are boxes around their members: drag a node in or out of a box to
// change where it belongs, drag a box to move it with its contents. Other diagram types
// are edited as code with the live preview.

const SHAPES: { shape: Shape; label: string }[] = [
  { shape: "rect", label: "Step" }, { shape: "round", label: "Rounded" }, { shape: "stadium", label: "Start / end" },
  { shape: "diamond", label: "Decision" }, { shape: "circle", label: "Circle" }, { shape: "cylinder", label: "Database" },
  { shape: "hexagon", label: "Prepare" }, { shape: "subroutine", label: "Subroutine" },
];
const KINDS: { kind: EdgeKind; label: string }[] = [
  { kind: "arrow", label: "Arrow" }, { kind: "open", label: "Line" }, { kind: "dotted", label: "Dotted" }, { kind: "thick", label: "Thick" },
];
const W = 150;
const H = 48;
const PAD = 22;    // space between a box and its contents
const HEAD = 26;   // a box's title row
const DRAG_TYPE = "application/x-kuli-shape";
const SIDES = [Position.Top, Position.Right, Position.Bottom, Position.Left];

const nodeSize = (n: FNode) => (n.shape === "circle" ? { w: 96, h: 96 } : n.shape === "diamond" ? { w: W, h: 70 } : { w: W, h: H });

function ShapeNode({ data, selected }: NodeProps<Node<{ label: string; shape: Shape }>>) {
  return (
    <div className={`fc-node fc-${data.shape} ${selected ? "is-sel" : ""}`}>
      <span className="fc-bg" aria-hidden="true" />
      <span className="fc-label">{data.label}</span>
      {SIDES.map((p) => <Handle key={p} id={p} type="source" position={p} className="fc-handle" />)}
    </div>
  );
}

function GroupNode({ data, selected }: NodeProps<Node<{ label: string; w: number; h: number }>>) {
  return (
    <div className={`fc-group ${selected ? "is-sel" : ""}`} style={{ width: data.w, height: data.h }}>
      <span className="fc-group-title">{data.label}</span>
      {SIDES.map((p) => <Handle key={p} id={p} type="source" position={p} className="fc-handle" />)}
    </div>
  );
}
const nodeTypes = { shape: ShapeNode, group: GroupNode };

type Box = { x: number; y: number; w: number; h: number };

// boxes: each group's rectangle, around its members and nested groups; an empty group
// keeps the box it was given.
function boxes(f: Flow): Map<string, Box> {
  const out = new Map<string, Box>();
  const visit = (g: FGroup, seen: Set<string>): Box => {
    const done = out.get(g.id);
    if (done) return done;
    seen.add(g.id);
    const parts: Box[] = [];
    for (const n of f.nodes) {
      if (n.group === g.id && n.x !== undefined) parts.push({ x: n.x, y: n.y!, ...nodeSize(n) });
    }
    for (const c of f.groups) if (c.parent === g.id && !seen.has(c.id)) parts.push(visit(c, seen));
    let b: Box;
    if (parts.length) {
      const x0 = Math.min(...parts.map((p) => p.x)), y0 = Math.min(...parts.map((p) => p.y));
      const x1 = Math.max(...parts.map((p) => p.x + p.w)), y1 = Math.max(...parts.map((p) => p.y + p.h));
      b = { x: x0 - PAD, y: y0 - PAD - HEAD, w: x1 - x0 + 2 * PAD, h: y1 - y0 + 2 * PAD + HEAD };
    } else {
      b = { x: g.x ?? 0, y: g.y ?? 0, w: g.w ?? 260, h: g.h ?? 150 };
    }
    out.set(g.id, b);
    return b;
  };
  for (const g of f.groups) visit(g, new Set());
  return out;
}

const depth = (f: Flow, id?: string): number => {
  let d = 0;
  for (let g = f.groups.find((x) => x.id === id); g?.parent && d < 20; g = f.groups.find((x) => x.id === g!.parent)) d++;
  return d;
};

// descendants: the groups inside g (any depth), g included.
function descendants(f: Flow, id: string): Set<string> {
  const out = new Set([id]);
  for (let grew = true; grew;) {
    grew = false;
    for (const g of f.groups) if (g.parent && out.has(g.parent) && !out.has(g.id)) { out.add(g.id); grew = true; }
  }
  return out;
}

// Lay out nodes that have no saved position, in the diagram's direction, keeping each
// subgraph's members together.
function place(f: Flow): Flow {
  if (f.nodes.every((n) => n.x !== undefined)) return f;
  const g = new dagre.graphlib.Graph({ compound: true });
  g.setGraph({ rankdir: f.dir === "TD" ? "TB" : f.dir, nodesep: 50, ranksep: 70 });
  g.setDefaultEdgeLabel(() => ({}));
  f.groups.forEach((x) => g.setNode(x.id, {}));
  f.groups.forEach((x) => x.parent && g.setParent(x.id, x.parent));
  f.nodes.forEach((n) => { g.setNode(n.id, { width: nodeSize(n).w, height: nodeSize(n).h }); if (n.group) g.setParent(n.id, n.group); });
  const isNode = new Set(f.nodes.map((n) => n.id));
  f.edges.forEach((e) => isNode.has(e.from) && isNode.has(e.to) && g.setEdge(e.from, e.to)); // dagre cannot route to a cluster
  dagre.layout(g);
  return {
    ...f,
    nodes: f.nodes.map((n) => (n.x !== undefined ? n : { ...n, x: g.node(n.id).x - nodeSize(n).w / 2, y: g.node(n.id).y - nodeSize(n).h / 2 })),
  };
}

function toNodes(f: Flow, selected: (id: string) => boolean): Node[] {
  const bx = boxes(f);
  const groups: Node[] = f.groups.map((g) => {
    const b = bx.get(g.id)!;
    return {
      id: g.id, type: "group", position: { x: b.x, y: b.y }, data: { label: g.label || g.id, w: b.w, h: b.h },
      selected: selected(g.id), zIndex: depth(f, g.id) - 10, width: b.w, height: b.h,
    };
  });
  const nodes: Node[] = f.nodes.map((n) => ({
    id: n.id, type: "shape", position: { x: n.x ?? 0, y: n.y ?? 0 }, data: { label: n.label, shape: n.shape }, selected: selected(n.id), zIndex: 1,
  }));
  return [...groups, ...nodes];
}

const toEdge = (e: FEdge, selected?: boolean): Edge => ({
  id: e.id, source: e.from, target: e.to, label: e.label || undefined, selected, zIndex: 2,
  className: `fc-edge fc-${e.kind}`,
  markerEnd: e.kind === "open" ? undefined : { type: MarkerType.ArrowClosed, width: 16, height: 16 },
  labelBgPadding: [6, 3] as [number, number],
});

function freeId(f: Flow, prefix: string): string {
  for (let i = 1; ; i++) {
    const id = `${prefix}${i}`;
    if (!f.nodes.some((n) => n.id === id) && !f.groups.some((g) => g.id === id)) return id;
  }
}

export function MermaidEditor({ code: initial, title, readOnly, onApply, onClose }: {
  code: string; title: string; readOnly?: boolean;
  onApply: (code: string, save: boolean) => void; onClose: () => void;
}) {
  const [code, setCode] = useState(initial);
  const [flow, setFlow] = useState<Flow | null>(null);
  const [visualNote, setVisualNote] = useState("");
  const [svg, setSvg] = useState("");
  const [renderError, setRenderError] = useState("");
  const [confirmClose, setConfirmClose] = useState(false);
  const fromCanvas = useRef(false); // the code change came from the canvas: don't re-parse it

  // Code → canvas (when the code is a flowchart the canvas can model).
  useEffect(() => {
    if (fromCanvas.current) { fromCanvas.current = false; return; }
    const t = setTimeout(() => {
      try {
        setFlow(place(parseFlow(code)));
        setVisualNote("");
      } catch (e) {
        setFlow(null);
        setVisualNote((e as Error).message);
      }
    }, 250);
    return () => clearTimeout(t);
  }, [code]);

  // Code → preview.
  useEffect(() => {
    const t = setTimeout(() => {
      renderMermaid(code).then((s) => { setSvg(cleanSvg(s)); setRenderError(""); }, (e: Error) => setRenderError(e.message.split("\n").slice(0, 4).join("\n")));
    }, 300);
    return () => clearTimeout(t);
  }, [code]);

  // Canvas → code.
  const commit = useCallback((f: Flow) => {
    setFlow(f);
    fromCanvas.current = true;
    setCode(writeFlow(f));
  }, []);

  const dirty = code !== initial;
  const close = () => (dirty && !readOnly ? setConfirmClose(true) : onClose());
  useEffect(() => {
    const key = (e: KeyboardEvent) => { if (e.key === "Escape" && !(e.target as HTMLElement).closest?.("input,select,textarea")) close(); };
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
  });

  return (
    <div className="modal-backdrop mmd-backdrop">
      <div className="mmd-editor" role="dialog" aria-modal="true" aria-label={`Edit diagram: ${title}`}>
        <header className="mmd-head">
          <h2>Diagram editor</h2>
          <span className="muted small">{title}</span>
          <span className="st-gap" />
          {confirmClose ? (
            <>
              <span className="small">Discard your changes to this diagram?</span>
              <button className="btn" onClick={onClose}>Discard</button>
              <button className="btn" onClick={() => setConfirmClose(false)}>Keep editing</button>
            </>
          ) : (
            <>
              {!readOnly && <button className="btn" disabled={!dirty} onClick={() => onApply(code, false)}>Apply</button>}
              {!readOnly && <button className="btn primary" disabled={!dirty} onClick={() => onApply(code, true)}>Apply and save</button>}
              <button className="btn" onClick={close}>{dirty && !readOnly ? "Cancel" : "Close"}</button>
            </>
          )}
        </header>
        <div className="mmd-body">
          <section className="mmd-pane mmd-code">
            <h3>Mermaid</h3>
            <textarea value={code} onChange={(e) => setCode(e.target.value)} spellCheck={false} readOnly={readOnly} aria-label="Mermaid code" />
          </section>
          <section className="mmd-pane mmd-canvas">
            {flow ? (
              <ReactFlowProvider>
                <Canvas flow={flow} readOnly={readOnly} onChange={commit} />
              </ReactFlowProvider>
            ) : (
              <p className="muted small pad">{visualNote || "Reading the diagram…"}</p>
            )}
          </section>
          <section className="mmd-pane mmd-preview">
            <h3>Preview</h3>
            {renderError ? <pre className="notice warn mmd-error">{renderError}</pre> : <div className="mmd-svg" dangerouslySetInnerHTML={{ __html: svg }} />}
          </section>
        </div>
      </div>
    </div>
  );
}

function Canvas({ flow, readOnly, onChange }: { flow: Flow; readOnly?: boolean; onChange: (f: Flow) => void }) {
  const rf = useReactFlow();
  const sel = useRef(new Set<string>());
  const [nodes, setNodes] = useState<Node[]>(() => toNodes(flow, () => false));
  const [edges, setEdges] = useState<Edge[]>(() => flow.edges.map((e) => toEdge(e)));
  const flowRef = useRef(flow);
  flowRef.current = flow;
  const drag = useRef<{ boxes: Map<string, Box>; start: Map<string, { x: number; y: number }> } | null>(null);

  // Keep the canvas in step with the flow (code edits, property edits), keeping selection.
  useEffect(() => {
    setNodes(toNodes(flow, (id) => sel.current.has(id)));
    setEdges((cur) => flow.edges.map((e) => toEdge(e, cur.find((c) => c.id === e.id)?.selected)));
  }, [flow]);
  useEffect(() => { sel.current = new Set(nodes.filter((n) => n.selected).map((n) => n.id)); }, [nodes]);

  const selected = nodes.filter((n) => n.selected);
  const selEdge = edges.filter((e) => e.selected);
  const one = selected.length === 1 ? selected[0] : undefined;
  const node = one?.type === "shape" ? flow.nodes.find((n) => n.id === one.id) : undefined;
  const group = one?.type === "group" ? flow.groups.find((g) => g.id === one.id) : undefined;
  const edge = selEdge.length === 1 && selected.length === 0 ? flow.edges.find((e) => e.id === selEdge[0].id) : undefined;
  const selectedShapes = selected.filter((n) => n.type === "shape").map((n) => n.id);

  const update = (f: Partial<Flow>) => onChange({ ...flowRef.current, ...f });
  const select = (id: string) => setTimeout(() => setNodes((cur) => cur.map((n) => ({ ...n, selected: n.id === id }))), 0);
  const center = () => rf.screenToFlowPosition({ x: innerWidth / 2, y: innerHeight / 2 });
  // The innermost group whose box holds the point (boxes as they were when dragging began).
  const groupAt = (bx: Map<string, Box>, x: number, y: number, skip: Set<string> = new Set()) => {
    let best: string | undefined, area = Infinity;
    for (const [id, b] of bx) {
      if (skip.has(id) || x < b.x || x > b.x + b.w || y < b.y || y > b.y + b.h) continue;
      if (b.w * b.h < area) { best = id; area = b.w * b.h; }
    }
    return best;
  };

  const addNode = (shape: Shape, pos = center()) => {
    const f = flowRef.current;
    const id = freeId(f, "N");
    const label = SHAPES.find((s) => s.shape === shape)?.label ?? "Step";
    const x = pos.x - W / 2, y = pos.y - H / 2;
    update({ nodes: [...f.nodes, { id, label, shape, x, y, group: groupAt(boxes(f), pos.x, pos.y) }] });
    select(id);
  };
  const addGroup = (pos = center(), members: string[] = []) => {
    const f = flowRef.current;
    const id = freeId(f, "G");
    const parent = members.length ? undefined : groupAt(boxes(f), pos.x, pos.y);
    // Wrapping a selection: the new group sits where its members were.
    const shared = members.length ? f.nodes.find((n) => n.id === members[0])?.group : undefined;
    update({
      groups: [...f.groups, { id, label: "Group", parent: members.length ? shared : parent, x: pos.x - 130, y: pos.y - 75, w: 260, h: 150 }],
      nodes: f.nodes.map((n) => (members.includes(n.id) ? { ...n, group: id } : n)),
    });
    select(id);
  };
  const ungroup = (id: string) => {
    const f = flowRef.current;
    const parent = f.groups.find((g) => g.id === id)?.parent;
    update({
      groups: f.groups.filter((g) => g.id !== id).map((g) => (g.parent === id ? { ...g, parent } : g)),
      nodes: f.nodes.map((n) => (n.group === id ? { ...n, group: parent } : n)),
      edges: f.edges.filter((e) => e.from !== id && e.to !== id),
    });
  };
  const deleteGroupAndContents = (id: string) => {
    const f = flowRef.current;
    const gone = descendants(f, id);
    const nodesGone = new Set(f.nodes.filter((n) => n.group && gone.has(n.group)).map((n) => n.id));
    update({
      groups: f.groups.filter((g) => !gone.has(g.id)),
      nodes: f.nodes.filter((n) => !nodesGone.has(n.id)),
      edges: f.edges.filter((e) => !nodesGone.has(e.from) && !nodesGone.has(e.to) && !gone.has(e.from) && !gone.has(e.to)),
    });
  };
  const removeNodes = (ids: string[]) => {
    const f = flowRef.current;
    update({ nodes: f.nodes.filter((n) => !ids.includes(n.id)), edges: f.edges.filter((e) => !ids.includes(e.from) && !ids.includes(e.to)) });
  };
  const removeEdges = (ids: string[]) => update({ edges: flowRef.current.edges.filter((e) => !ids.includes(e.id)) });
  const setGroup = (id: string, patch: Partial<FGroup>) =>
    update({ groups: flowRef.current.groups.map((g) => (g.id === id ? { ...g, ...patch } : g)) });

  const nodeTypesMemo = useMemo(() => nodeTypes, []);
  return (
    <div className="fc-wrap">
      <div className="fc-toolbar">
        {!readOnly && (
          <div className="fc-palette" aria-label="Shapes: drag onto the canvas or click to add">
            {SHAPES.map((s) => (
              <button key={s.shape} className={`fc-chip fc-${s.shape}`} draggable title={`${s.label}: drag onto the canvas, or click`}
                onDragStart={(e) => { e.dataTransfer.setData(DRAG_TYPE, s.shape); e.dataTransfer.effectAllowed = "copy"; }}
                onClick={() => addNode(s.shape)}>
                <span className="fc-bg" aria-hidden="true" /><span className="fc-label">{s.label}</span>
              </button>
            ))}
            <button className="fc-chip fc-groupchip" draggable title="Group (subgraph): drag onto the canvas, or click"
              onDragStart={(e) => { e.dataTransfer.setData(DRAG_TYPE, "group"); e.dataTransfer.effectAllowed = "copy"; }}
              onClick={() => addGroup()}>
              <span className="fc-bg" aria-hidden="true" /><span className="fc-label">Group</span>
            </button>
          </div>
        )}
        <span className="st-gap" />
        {!readOnly && selectedShapes.length > 0 && (
          <button className="btn small-btn" onClick={() => addGroup(center(), selectedShapes)}>Group selection</button>
        )}
        <label className="small">Direction{" "}
          <select value={flow.dir} disabled={readOnly} onChange={(e) => update({ dir: e.target.value, nodes: flowRef.current.nodes.map((n) => ({ ...n, x: undefined, y: undefined })) })}>
            <option value="TD">Top to bottom</option><option value="LR">Left to right</option>
            <option value="BT">Bottom to top</option><option value="RL">Right to left</option>
          </select>
        </label>
        {!readOnly && (
          <button className="btn small-btn" onClick={() => onChange(place({ ...flowRef.current, nodes: flowRef.current.nodes.map((n) => ({ ...n, x: undefined, y: undefined })) }))}>
            Tidy layout
          </button>
        )}
      </div>
      <div className="fc-main">
        <div className="fc-flow" onDragOver={(e) => { if (e.dataTransfer.types.includes(DRAG_TYPE)) { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; } }}
          onDrop={(e) => {
            const kind = e.dataTransfer.getData(DRAG_TYPE);
            if (!kind || readOnly) return;
            e.preventDefault();
            const at = rf.screenToFlowPosition({ x: e.clientX, y: e.clientY });
            if (kind === "group") addGroup(at);
            else addNode(kind as Shape, at);
          }}>
          <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypesMemo} connectionMode={ConnectionMode.Loose} fitView fitViewOptions={{ padding: 0.15, maxZoom: 1.2 }} minZoom={0.1}
            nodesDraggable={!readOnly} nodesConnectable={!readOnly} elementsSelectable deleteKeyCode={readOnly ? null : ["Delete", "Backspace"]}
            onNodesChange={(ch: NodeChange[]) => setNodes((n) => applyNodeChanges(ch.filter((c) => c.type !== "remove"), n))}
            onEdgesChange={(ch: EdgeChange[]) => setEdges((e) => applyEdgeChanges(ch.filter((c) => c.type !== "remove"), e))}
            onNodeDragStart={() => {
              drag.current = { boxes: boxes(flowRef.current), start: new Map(rf.getNodes().map((n) => [n.id, { ...n.position }])) };
            }}
            onNodeDrag={(_, __, dragged) => {
              // A dragged box carries its contents along.
              const d = drag.current;
              if (!d) return;
              const f = flowRef.current;
              const moves = new Map<string, { x: number; y: number }>();
              for (const g of dragged.filter((n) => n.type === "group")) {
                const s0 = d.start.get(g.id)!;
                const dx = g.position.x - s0.x, dy = g.position.y - s0.y;
                const inside = descendants(f, g.id);
                for (const n of f.nodes) if (n.group && inside.has(n.group)) moves.set(n.id, { x: d.start.get(n.id)!.x + dx, y: d.start.get(n.id)!.y + dy });
                for (const c of inside) if (c !== g.id) moves.set(c, { x: d.start.get(c)!.x + dx, y: d.start.get(c)!.y + dy });
              }
              if (moves.size) setNodes((cur) => cur.map((n) => (moves.has(n.id) ? { ...n, position: moves.get(n.id)! } : n)));
            }}
            onNodeDragStop={(_, __, dragged) => {
              const d = drag.current;
              drag.current = null;
              if (!d) return;
              const f = flowRef.current;
              const pos = new Map(rf.getNodes().map((n) => [n.id, n.position]));
              const movedGroups = dragged.filter((n) => n.type === "group").map((n) => n.id);
              const carried = new Set<string>();
              for (const id of movedGroups) for (const c of descendants(f, id)) carried.add(c);
              const draggedShapes = new Set(dragged.filter((n) => n.type === "shape").map((n) => n.id));
              update({
                nodes: f.nodes.map((n) => {
                  if (draggedShapes.has(n.id)) {
                    // Dropped inside a box: it belongs to that group now; outside all: top level.
                    const p = pos.get(n.id)!, sz = nodeSize(n);
                    return { ...n, x: p.x, y: p.y, group: groupAt(d.boxes, p.x + sz.w / 2, p.y + sz.h / 2) };
                  }
                  if (n.group && carried.has(n.group)) return { ...n, x: pos.get(n.id)!.x, y: pos.get(n.id)!.y };
                  return n;
                }),
                groups: f.groups.map((g) => {
                  if (!carried.has(g.id)) return g;
                  const p = pos.get(g.id)!;
                  return { ...g, x: p.x, y: p.y, w: d.boxes.get(g.id)?.w, h: d.boxes.get(g.id)?.h };
                }),
              });
            }}
            onNodesDelete={(del) => {
              for (const g of del.filter((n) => n.type === "group")) ungroup(g.id); // Delete removes the box, not its contents
              const shapes = del.filter((n) => n.type === "shape").map((n) => n.id);
              if (shapes.length) removeNodes(shapes);
            }}
            onEdgesDelete={(del) => removeEdges(del.map((d) => d.id))}
            onConnect={(c: Connection) => {
              if (!c.source || !c.target || c.source === c.target) return;
              const f = flowRef.current;
              let n = f.edges.length + 1;
              while (f.edges.some((e) => e.id === `e${n}`)) n++;
              update({ edges: [...f.edges, { id: `e${n}`, from: c.source, to: c.target, label: "", kind: "arrow" }] });
            }}
            proOptions={{ hideAttribution: true }}>
            <Background gap={20} size={1} />
            <Controls showInteractive={false} />
          </ReactFlow>
        </div>
        <aside className="fc-props" aria-label="Selection">
          {node && (
            <>
              <h3>Node {node.id}</h3>
              <label>Label<input value={node.label} disabled={readOnly} autoFocus
                onChange={(e) => update({ nodes: flowRef.current.nodes.map((n) => (n.id === node.id ? { ...n, label: e.target.value } : n)) })} /></label>
              <label>Shape<select value={node.shape} disabled={readOnly}
                onChange={(e) => update({ nodes: flowRef.current.nodes.map((n) => (n.id === node.id ? { ...n, shape: e.target.value as Shape } : n)) })}>
                {SHAPES.map((s) => <option key={s.shape} value={s.shape}>{s.label}</option>)}
              </select></label>
              <label>In group<select value={node.group ?? ""} disabled={readOnly}
                onChange={(e) => update({ nodes: flowRef.current.nodes.map((n) => (n.id === node.id ? { ...n, group: e.target.value || undefined } : n)) })}>
                <option value="">None</option>
                {flow.groups.map((g) => <option key={g.id} value={g.id}>{g.label || g.id}</option>)}
              </select></label>
              {!readOnly && <button className="btn danger" onClick={() => removeNodes([node.id])}>Delete node</button>}
            </>
          )}
          {group && (
            <>
              <h3>Group {group.id}</h3>
              <label>Title<input value={group.label} disabled={readOnly} autoFocus onChange={(e) => setGroup(group.id, { label: e.target.value })} /></label>
              <label>Direction inside<select value={group.dir ?? ""} disabled={readOnly} onChange={(e) => setGroup(group.id, { dir: e.target.value || undefined })}>
                <option value="">Same as the diagram</option><option value="TB">Top to bottom</option><option value="LR">Left to right</option>
                <option value="BT">Bottom to top</option><option value="RL">Right to left</option>
              </select></label>
              <label>Inside<select value={group.parent ?? ""} disabled={readOnly} onChange={(e) => setGroup(group.id, { parent: e.target.value || undefined })}>
                <option value="">Nothing (top level)</option>
                {flow.groups.filter((g) => !descendants(flow, group.id).has(g.id)).map((g) => <option key={g.id} value={g.id}>{g.label || g.id}</option>)}
              </select></label>
              {!readOnly && (
                <>
                  <button className="btn" onClick={() => ungroup(group.id)}>Ungroup (keep the nodes)</button>
                  <button className="btn danger" onClick={() => deleteGroupAndContents(group.id)}>Delete group and contents</button>
                </>
              )}
            </>
          )}
          {edge && (
            <>
              <h3>Connection {edge.from} → {edge.to}</h3>
              <label>Label<input value={edge.label} disabled={readOnly} autoFocus
                onChange={(e) => update({ edges: flowRef.current.edges.map((x) => (x.id === edge.id ? { ...x, label: e.target.value } : x)) })} /></label>
              <label>Line<select value={edge.kind} disabled={readOnly}
                onChange={(e) => update({ edges: flowRef.current.edges.map((x) => (x.id === edge.id ? { ...x, kind: e.target.value as EdgeKind } : x)) })}>
                {KINDS.map((k) => <option key={k.kind} value={k.kind}>{k.label}</option>)}
              </select></label>
              {!readOnly && (
                <>
                  <button className="btn" onClick={() => update({ edges: flowRef.current.edges.map((x) => (x.id === edge.id ? { ...x, from: x.to, to: x.from } : x)) })}>Reverse direction</button>
                  <button className="btn danger" onClick={() => removeEdges([edge.id])}>Delete connection</button>
                </>
              )}
            </>
          )}
          {!node && !edge && !group && (
            <p className="muted small">
              {readOnly ? "Read-only." : selected.length > 1
                ? `${selected.length} selected. Group them, or press Delete.`
                : "Drag a shape or a group onto the canvas. Drag from a node's edge to another node to connect them. Drag nodes into a group's box to put them in it. Select something to edit it, or press Delete."}
            </p>
          )}
        </aside>
      </div>
    </div>
  );
}
