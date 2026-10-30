import dagre from "@dagrejs/dagre";
import {
  Background, ConnectionMode, Controls, Handle, MarkerType, Position, ReactFlow, ReactFlowProvider, applyEdgeChanges, applyNodeChanges,
  useReactFlow, type Connection, type Edge, type EdgeChange, type Node, type NodeChange, type NodeProps,
} from "@xyflow/react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type EdgeKind, type FEdge, type FNode, type Flow, type Shape, parseFlow, writeFlow } from "../flowchart";
import { cleanSvg } from "../markdown";
import { renderMermaid } from "../mermaid";

// The Mermaid editor: code, a visual canvas (flowcharts) and a live preview, kept in
// sync. On the canvas: drag shapes in from the palette, drag nodes to arrange them,
// drag from a node's edge to another node to connect, select and press Delete to
// remove. Other diagram types are edited as code with the live preview.

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
const DRAG_TYPE = "application/x-kuli-shape";

function ShapeNode({ data, selected }: NodeProps<Node<{ label: string; shape: Shape }>>) {
  const sides = [Position.Top, Position.Right, Position.Bottom, Position.Left];
  return (
    <div className={`fc-node fc-${data.shape} ${selected ? "is-sel" : ""}`}>
      <span className="fc-bg" aria-hidden="true" />
      <span className="fc-label">{data.label}</span>
      {sides.map((p) => <Handle key={p} id={p} type="source" position={p} className="fc-handle" />)}
    </div>
  );
}
const nodeTypes = { shape: ShapeNode };

// Lay out nodes that have no saved position, in the diagram's direction.
function place(f: Flow): Flow {
  if (f.nodes.every((n) => n.x !== undefined)) return f;
  const g = new dagre.graphlib.Graph();
  g.setGraph({ rankdir: f.dir === "TD" ? "TB" : f.dir, nodesep: 50, ranksep: 70 });
  g.setDefaultEdgeLabel(() => ({}));
  f.nodes.forEach((n) => g.setNode(n.id, { width: W, height: H }));
  f.edges.forEach((e) => g.setEdge(e.from, e.to));
  dagre.layout(g);
  return { ...f, nodes: f.nodes.map((n) => (n.x !== undefined ? n : { ...n, x: g.node(n.id).x - W / 2, y: g.node(n.id).y - H / 2 })) };
}

const toNode = (n: FNode, selected?: boolean): Node => ({
  id: n.id, type: "shape", position: { x: n.x ?? 0, y: n.y ?? 0 }, data: { label: n.label, shape: n.shape }, selected,
});
const toEdge = (e: FEdge, selected?: boolean): Edge => ({
  id: e.id, source: e.from, target: e.to, label: e.label || undefined, selected,
  className: `fc-edge fc-${e.kind}`,
  markerEnd: e.kind === "open" ? undefined : { type: MarkerType.ArrowClosed, width: 16, height: 16 },
  labelBgPadding: [6, 3] as [number, number],
});

function nextId(f: Flow): string {
  for (let i = f.nodes.length + 1; ; i++) {
    const id = `N${i}`;
    if (!f.nodes.some((n) => n.id === id)) return id;
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
  const [nodes, setNodes] = useState<Node[]>(() => flow.nodes.map((n) => toNode(n)));
  const [edges, setEdges] = useState<Edge[]>(() => flow.edges.map((e) => toEdge(e)));
  const flowRef = useRef(flow);
  flowRef.current = flow;

  // Keep the canvas in step with the flow (code edits, property edits), keeping selection.
  useEffect(() => {
    setNodes((cur) => flow.nodes.map((n) => toNode(n, cur.find((c) => c.id === n.id)?.selected)));
    setEdges((cur) => flow.edges.map((e) => toEdge(e, cur.find((c) => c.id === e.id)?.selected)));
  }, [flow]);

  const selNode = nodes.filter((n) => n.selected);
  const selEdge = edges.filter((e) => e.selected);
  const node = selNode.length === 1 ? flow.nodes.find((n) => n.id === selNode[0].id) : undefined;
  const edge = selEdge.length === 1 && selNode.length === 0 ? flow.edges.find((e) => e.id === selEdge[0].id) : undefined;

  const update = (f: Partial<Flow>) => onChange({ ...flowRef.current, ...f });
  const addNode = (shape: Shape, pos?: { x: number; y: number }) => {
    const f = flowRef.current;
    const id = nextId(f);
    const at = pos ?? rf.screenToFlowPosition({ x: innerWidth / 2, y: innerHeight / 2 });
    const label = SHAPES.find((s) => s.shape === shape)?.label ?? "Step";
    update({ nodes: [...f.nodes, { id, label, shape, x: at.x - W / 2, y: at.y - H / 2 }] });
    setTimeout(() => setNodes((cur) => cur.map((n) => ({ ...n, selected: n.id === id }))), 0);
  };
  const removeNodes = (ids: string[]) => {
    const f = flowRef.current;
    update({ nodes: f.nodes.filter((n) => !ids.includes(n.id)), edges: f.edges.filter((e) => !ids.includes(e.from) && !ids.includes(e.to)) });
  };
  const removeEdges = (ids: string[]) => update({ edges: flowRef.current.edges.filter((e) => !ids.includes(e.id)) });

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
          </div>
        )}
        <span className="st-gap" />
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
            const shape = e.dataTransfer.getData(DRAG_TYPE) as Shape;
            if (!shape || readOnly) return;
            e.preventDefault();
            addNode(shape, rf.screenToFlowPosition({ x: e.clientX, y: e.clientY }));
          }}>
          <ReactFlow nodes={nodes} edges={edges} nodeTypes={nodeTypesMemo} connectionMode={ConnectionMode.Loose} fitView fitViewOptions={{ padding: 0.3, maxZoom: 1.2 }}
            nodesDraggable={!readOnly} nodesConnectable={!readOnly} elementsSelectable deleteKeyCode={readOnly ? null : ["Delete", "Backspace"]}
            onNodesChange={(ch: NodeChange[]) => setNodes((n) => applyNodeChanges(ch.filter((c) => c.type !== "remove"), n))}
            onEdgesChange={(ch: EdgeChange[]) => setEdges((e) => applyEdgeChanges(ch.filter((c) => c.type !== "remove"), e))}
            onNodeDragStop={(_, __, dragged) => {
              const moved = new Map(dragged.map((d) => [d.id, d.position]));
              update({ nodes: flowRef.current.nodes.map((n) => (moved.has(n.id) ? { ...n, x: moved.get(n.id)!.x, y: moved.get(n.id)!.y } : n)) });
            }}
            onNodesDelete={(del) => removeNodes(del.map((d) => d.id))}
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
              {!readOnly && <button className="btn danger" onClick={() => removeNodes([node.id])}>Delete node</button>}
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
          {!node && !edge && (
            <p className="muted small">
              {readOnly ? "Read-only." : "Drag a shape onto the canvas. Drag from a node's edge to another node to connect them. Select something to rename, restyle or delete it (or press Delete)."}
            </p>
          )}
        </aside>
      </div>
    </div>
  );
}
