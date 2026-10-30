import { useEffect, useMemo, useRef, useState } from "react";
import { api, type DocsJob, type FileView, type Hit, type TreeFile } from "../api";
import { useLoad } from "../hooks";

import { Icon } from "./Icons";
import { loadFile } from "./files";

type Dir = { name: string; path: string; dirs: Map<string, Dir>; files: TreeFile[] };

function buildTree(files: TreeFile[]): Dir {
  const root: Dir = { name: "", path: "", dirs: new Map(), files: [] };
  for (const f of files) {
    const parts = f.p.split("/");
    let d = root;
    for (let i = 0; i < parts.length - 1; i++) {
      let next = d.dirs.get(parts[i]);
      if (!next) {
        next = { name: parts[i], path: parts.slice(0, i + 1).join("/"), dirs: new Map(), files: [] };
        d.dirs.set(parts[i], next);
      }
      d = next;
    }
    d.files.push(f);
  }
  return root;
}

export function Explorer({ files, repo, active, onOpen }: {
  files: TreeFile[]; repo: string; active?: string; onOpen: (p: string, from?: number, to?: number) => void;
}) {
  const tree = useMemo(() => buildTree(files), [files]);
  const [open, setOpen] = useState<Set<string>>(() => new Set());

  // Reveal the active file in the tree, as VS Code does.
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!active) return;
    const parts = active.split("/");
    setOpen((o) => {
      const n = new Set(o);
      for (let i = 1; i < parts.length; i++) n.add(parts.slice(0, i).join("/"));
      return n;
    });
    const t = setTimeout(() => scroller.current?.querySelector(".tree-row.is-active")?.scrollIntoView({ block: "nearest" }), 0);
    return () => clearTimeout(t);
  }, [active]);

  const toggle = (p: string) => setOpen((o) => {
    const n = new Set(o);
    if (n.has(p)) n.delete(p); else n.add(p);
    return n;
  });

  function render(d: Dir, depth: number): React.ReactNode {
    const dirs = [...d.dirs.values()].sort((a, b) => a.name.localeCompare(b.name));
    const fs = [...d.files].sort((a, b) => a.p.localeCompare(b.p));
    return (
      <>
        {dirs.map((c) => (
          <li key={c.path}>
            <button className="tree-row" style={{ paddingLeft: 8 + depth * 12 }} onClick={() => toggle(c.path)} aria-expanded={open.has(c.path)}>
              <Icon.chevron open={open.has(c.path)} /> {c.name}
            </button>
            {open.has(c.path) && <ul role="group">{render(c, depth + 1)}</ul>}
          </li>
        ))}
        {fs.map((f) => {
          const name = f.p.slice(f.p.lastIndexOf("/") + 1);
          return (
            <li key={f.p}>
              <button
                className={`tree-row file ${active === f.p ? "is-active" : ""} ${f.l ? "" : "is-other"}`}
                style={{ paddingLeft: 22 + depth * 12 }}
                onClick={() => onOpen(f.p)}
                title={`${f.p} — ${f.n} lines`}
              >
                {name}
              </button>
            </li>
          );
        })}
      </>
    );
  }

  return (
    <div className="pane explorer">
      <h2 className="pane-title">{repo}</h2>
      <div className="tree-scroll" ref={scroller}>
        <ul className="tree" role="tree" aria-label="Files">{render(tree, 0)}</ul>
      </div>
      {active && <FileFacts path={active} onOpen={onOpen} />}
    </div>
  );
}

// Outline + "Imported by" for the active file: the knowledge VS Code doesn't have.
function FileFacts({ path, onOpen }: { path: string; onOpen: (p: string, from?: number, to?: number) => void }) {
  const [f, setF] = useState<FileView | null>(null);
  useEffect(() => {
    let live = true;
    setF(null);
    loadFile(path).then((v) => live && setF(v), () => {});
    return () => { live = false; };
  }, [path]);
  if (!f) return null;
  return (
    <div className="file-facts">
      {f.symbols.length > 0 && (
        <details open>
          <summary className="pane-sub">Outline</summary>
          <ul className="plain">
            {f.symbols.map((s, i) => (
              <li key={i}>
                <button className="link-row" onClick={() => onOpen(path, s.Line, s.EndLine)} title={s.Detail}>
                  <span className={`k k-${s.Kind}`}>{s.Kind.slice(0, 1)}</span>{s.Name}
                </button>
              </li>
            ))}
          </ul>
        </details>
      )}
      <details open>
        <summary className="pane-sub">Imported by ({f.importers.length})</summary>
        {f.importers.length === 0 && <p className="muted small pad-x">No scanned file imports this one.</p>}
        <ul className="plain">
          {f.importers.slice(0, 80).map((h, i) => (
            <li key={i}><button className="link-row" onClick={() => onOpen(h.Path, h.Line)} title={h.Path}>{h.Path}</button></li>
          ))}
        </ul>
      </details>
    </div>
  );
}

export function SearchPane({ initial, onOpen }: { initial: string; onOpen: (p: string, from?: number, to?: number) => void }) {
  const [q, setQ] = useState(initial);
  const [hits, setHits] = useState<Hit[] | null>(null);
  useEffect(() => {
    setQ(initial);
  }, [initial]);
  useEffect(() => {
    const text = q.trim();
    if (!text) { setHits(null); return; }
    let live = true;
    const t = setTimeout(() => api.search(text).then((r) => live && setHits(r.hits), () => live && setHits([])), 160);
    return () => { live = false; clearTimeout(t); };
  }, [q]);
  return (
    <div className="pane">
      <h2 className="pane-title">Search</h2>
      <input className="pane-input" autoFocus value={q} onChange={(e) => setQ(e.target.value)}
        placeholder="Names and doc comments" aria-label="Search definitions" />
      {hits?.length === 0 && <p className="muted small pad-x">Nothing matches.</p>}
      <ul className="plain hits">
        {hits?.map((h, i) => (
          <li key={i}>
            <button className="link-row hit" disabled={!h.Path} onClick={() => onOpen(h.Path, h.Line || undefined, h.EndLine || undefined)}>
              <span className="hit-top"><span className={`k k-${h.Kind.toLowerCase()}`}>{h.Kind.slice(0, 1).toLowerCase()}</span>{h.Name}</span>
              {h.Path && <span className="hit-path">{h.Path}{h.Line ? `:${h.Line}` : ""}</span>}
              {h.Detail && <span className="hit-doc">{h.Detail}</span>}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

const DOC_NAMES: Record<string, string> = {
  overview: "overview", architecture: "architecture", dependencies: "dependencies",
  infrastructure: "infrastructure", "getting-started": "getting started",
};

// Docs: what KuliDev wrote about this folder, and the button that writes or refreshes
// them (same engine as `kuli docs`), with progress and Stop.
// Docs: what KuliDev wrote about this folder, and the button that writes or refreshes
// them (same engine as `kuli docs`), with progress and Stop. A folder of several
// projects also has a System overview and documents for each project (the scope).
export function DocsPane({ onOpenDoc, model }: { onOpenDoc: (name: string) => void; model: string }) {
  const [scope, setScopeState] = useState(() => {
    try { return sessionStorage.getItem("kuli.docscope") ?? ""; } catch { return ""; }
  });
  const setScope = (v: string) => { setScopeState(v); try { sessionStorage.setItem("kuli.docscope", v); } catch { /* storage off */ } };
  const d = useLoad(() => api.docs(scope), [scope]);
  const docs = d.data?.docs ?? [];
  const projects = d.data?.projects ?? [];
  const [job, setJob] = useState<DocsJob | null>(null);
  const [factsOnly, setFactsOnly] = useState(false);
  const [error, setError] = useState("");
  const wasRunning = useRef(false);

  useEffect(() => {
    let t: number | undefined;
    const poll = () => api.docsJob().then((j) => {
      setJob(j);
      if (wasRunning.current && !j.running) d.reload(); // finished: show the new docs
      wasRunning.current = j.running;
      if (j.running) t = window.setTimeout(poll, 1000);
    }, () => {});
    poll();
    return () => window.clearTimeout(t);
  }, [job?.running]); // eslint-disable-line react-hooks/exhaustive-deps

  async function generate(opts: { only?: string[]; scope?: string } = {}) {
    setError("");
    try {
      wasRunning.current = true; // even a run that ends before the first poll reloads the list
      setJob(await api.generateDocs({ scope, ...opts, factsOnly: factsOnly || !model }));
    } catch (e) {
      setError((e as Error).message);
    }
  }
  const stale = docs.filter((x) => x.status === "STALE").map((x) => x.id).filter((id) => id !== "system-overview");
  const running = !!job?.running;
  const pct = job && job.total ? Math.round((job.done / job.total) * 100) : 0;
  const current = (job?.current ?? "").replace(/(^|: )([a-z-]+)$/, (_, pre, id) => pre + (DOC_NAMES[id] ?? id.replace(/-/g, " ")));
  const scopeName = scope ? projects.find((p) => p.path === scope)?.name ?? scope : "";

  return (
    <div className="pane">
      <h2 className="pane-title">Docs</h2>
      {projects.length > 0 && (
        <label className="docs-scope pad-x">
          <span className="small muted">Documents for</span>
          <select value={scope} onChange={(e) => setScope(e.target.value)}>
            <option value="">Whole folder ({projects.length} projects)</option>
            {projects.map((p) => (
              <option key={p.path} value={p.path}>{p.name}{p.docs ? ` (${p.docs} docs${p.stale ? `, ${p.stale} stale` : ""})` : " (no docs yet)"}</option>
            ))}
          </select>
        </label>
      )}
      {d.data && docs.length === 0 && !running && (
        <p className="muted small pad-x">
          {scope
            ? `KuliDev can write documents about ${scopeName} on its own: overview, architecture, dependencies, infrastructure, getting started.`
            : projects.length > 0
              ? "For this folder KuliDev writes a System overview (how the projects connect, from the Systems map) and documents about the folder as a whole. Each project can also get its own: pick it above, or generate all."
              : "KuliDev can write documents about this folder (overview, architecture, dependencies, infrastructure, getting started) from what it read, citing the code for every claim."}
        </p>
      )}
      <div className="docs-gen pad-x">
        {running ? (
          <>
            <p className="small">Writing {current || "…"} ({Math.min(job!.done + 1, job!.total)} of {job!.total})</p>
            <div className="progress" role="progressbar" aria-valuenow={pct} aria-valuemin={0} aria-valuemax={100}><span style={{ width: `${pct}%` }} /></div>
            <p className="muted small docs-line">{job!.lines[job!.lines.length - 1]}</p>
            <button className="btn small-btn" onClick={() => api.cancelDocs().catch(() => {})}>Stop</button>
          </>
        ) : (
          <>
            <div className="docs-actions">
              <button className="btn primary small-btn" onClick={() => generate()}>
                {docs.length ? "Regenerate" : "Generate docs"}{scope ? ` for ${scopeName}` : ""}
              </button>
              {stale.length > 0 && <button className="btn small-btn" onClick={() => generate({ only: stale })}>Refresh {stale.length} stale</button>}
              {!scope && projects.length > 0 && (
                <button className="btn small-btn" onClick={() => generate({ scope: "*" })}>Generate all projects</button>
              )}
            </div>
            <p className="muted small">
              {model && !factsOnly ? <>Written by {model}; code excerpts are sent to it.</> : "Facts only: no model is used."}
              {!model && <> Set a model in Settings for written explanations.</>}
              {!scope && projects.length > 0 && " The System overview never needs a model."}
            </p>
            {model && (
              <label className="small check"><input type="checkbox" checked={factsOnly} onChange={(e) => setFactsOnly(e.target.checked)} /> Facts only (don't use the model)</label>
            )}
            {job?.error && <p className="notice warn small">{job.error === "stopped" ? "Stopped." : job.error}</p>}
            {error && <p className="notice warn small">{error}</p>}
          </>
        )}
      </div>
      <ul className="plain">
        {docs.map((x) => (
          <li key={x.file}>
            <button className="link-row" onClick={() => onOpenDoc(x.file)}>
              {x.title}{x.status === "STALE" && <span className="badge stale">stale</span>}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function IssuesPane({ onOpen }: { onOpen: (p: string, from?: number) => void }) {
  const b = useLoad(api.broken, []);
  const hits = b.data?.hits ?? [];
  return (
    <div className="pane">
      <h2 className="pane-title">Issues</h2>
      <p className="muted small pad-x">Imports pointing at files that don't exist.</p>
      {b.data && hits.length === 0 && <p className="small pad-x">None found.</p>}
      <ul className="plain">
        {hits.map((h, i) => (
          <li key={i}>
            <button className="link-row hit" onClick={() => onOpen(h.Path, h.Line)}>
              <span className="hit-top warn-text">{h.Name.split("/").pop()}</span>
              <span className="hit-path">{h.Path}:{h.Line}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}


export function AgentsPane({ onStart }: { onStart: (id: string) => Promise<void> }) {
  const a = useLoad(api.agents, []);
  // Starting Cursor can attach the knowledge graph (consent dialog): show it here too.
  useEffect(() => {
    const again = () => a.reload();
    addEventListener("kuli-agents-changed", again);
    return () => removeEventListener("kuli-agents-changed", again);
  }, []); // eslint-disable-line react-hooks/exhaustive-deps
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const list = a.data?.agents ?? [];
  async function start(id: string) {
    setBusy(id);
    setError("");
    try {
      await onStart(id);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy("");
    }
  }
  return (
    <div className="pane">
      <h2 className="pane-title">Agents</h2>
      <p className="muted small pad-x">
        Runs a coding agent in this workspace's terminal. Agents marked "knows this repo" start with KuliDev's
        knowledge graph attached, so they can search code and check who depends on what before changing it.
      </p>
      {a.data && !a.data.enabled && <p className="notice pad">Agents need terminals, which are off for this workspace.</p>}
      {error && <p className="notice warn pad">{error}</p>}
      <ul className="plain agents">
        {list.map((ag) => (
          <li key={ag.id}>
            <div className="agent-row">
              <span className="agent-name">{ag.name}</span>
              {ag.installed ? (
                <button className="btn" disabled={!a.data?.enabled || busy !== ""} onClick={() => start(ag.id)}>
                  {busy === ag.id ? "Starting…" : "Start"}
                </button>
              ) : (
                <span className="muted small">not installed</span>
              )}
            </div>
            <span className="small muted">{ag.mcp ? "Knows this repo (KuliDev MCP attached)" : ag.hint}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}
