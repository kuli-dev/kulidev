import { useCallback, useEffect, useRef, useState } from "react";
import { api, type DirList, type OpenJob, type RecentFolder, type WorkspaceInfo } from "../api";

export const isOpening = (j?: OpenJob | null): j is OpenJob => !!j && j.phase !== "done" && j.phase !== "error";

// useWorkspace follows the open folder and, while one is opening, its progress.
export function useWorkspace() {
  const [info, setInfo] = useState<WorkspaceInfo | null>(null);
  const [error, setError] = useState("");
  const timer = useRef<number | undefined>(undefined);
  const poll = useCallback(async () => {
    window.clearTimeout(timer.current);
    try {
      const w = await api.workspace();
      setInfo(w);
      if (isOpening(w.opening)) timer.current = window.setTimeout(poll, 250);
    } catch {
      /* a fixed (remote) workspace has no launcher API */
    }
  }, []);
  useEffect(() => {
    poll();
    return () => window.clearTimeout(timer.current);
  }, [poll]);
  const open = useCallback(async (path: string) => {
    setError("");
    try {
      await api.openWorkspace(path);
    } catch (e) {
      setError((e as Error).message);
    }
    poll();
  }, [poll]);
  return { info, job: info?.opening ?? null, open, error, recent: (info?.recent ?? []).filter((r) => r.exists) };
}

const n = (v: number) => v.toLocaleString();

export function progressText(j: OpenJob): string {
  switch (j.phase) {
    case "listing":
      return j.first ? "Finding files…" : "Checking for changes…";
    case "reading":
      if (!j.total) return "Checking for changes…";
      return j.first ? `Reading code: ${n(j.done)} of ${n(j.total)} files` : `Updating ${n(j.done)} of ${n(j.total)} changed files`;
    case "saving":
      return "Building the map…";
    case "done":
      return "Ready";
    default:
      return j.error ?? "Could not open the folder";
  }
}

// ProgressBar is determinate while files are read and moving otherwise.
export function ProgressBar({ job, className = "" }: { job: OpenJob; className?: string }) {
  const known = job.phase === "reading" && job.total > 0;
  const pct = job.phase === "done" ? 100 : known ? Math.round((job.done / job.total) * 95) : 0;
  return (
    <div className={`progress ${known || job.phase === "done" ? "" : "indeterminate"} ${className}`} role="progressbar"
      aria-label={progressText(job)} aria-valuemin={0} aria-valuemax={100} aria-valuenow={known ? pct : undefined}>
      <span style={known || job.phase === "done" ? { width: `${pct}%` } : undefined} />
    </div>
  );
}

// OpeningCard is the big progress shown in the editor area while a folder opens.
export function OpeningCard({ job }: { job: OpenJob }) {
  return (
    <div className="opening-card" role="status">
      <p className="opening-name">Opening <strong>{job.name}</strong></p>
      <ProgressBar job={job} className="big" />
      <p className="opening-step">
        <span>{progressText(job)}</span>
        {job.phase === "reading" && job.total > 0 && <span className="opening-pct">{Math.round((job.done / job.total) * 100)}%</span>}
      </p>
      <p className="muted small">
        {job.first ? "KuliDev reads a folder once; next time it opens straight away." : "Showing what KuliDev read last time while it catches up."}
      </p>
    </div>
  );
}

export function RecentList({ recent, onOpen, disabled }: { recent: RecentFolder[]; onOpen: (p: string) => void; disabled?: boolean }) {
  return (
    <ul className="plain recent">
      {recent.map((r) => (
        <li key={r.path}>
          <button className="recent-row" disabled={disabled} onClick={() => onOpen(r.path)} title={r.path}>
            <span className="recent-name">{r.name}</span>
            <span className="recent-path">{r.path}</span>
          </button>
        </li>
      ))}
    </ul>
  );
}

// FolderDialog is "Open Folder": browse to a folder and open it. Repositories are marked.
export function FolderDialog({ onPick, onClose }: { onPick: (path: string) => void; onClose: () => void }) {
  const [list, setList] = useState<DirList | null>(null);
  const [pathInput, setPathInput] = useState("");
  const [error, setError] = useState("");

  async function browse(path: string) {
    setError("");
    try {
      const l = await api.fsList(path);
      setList(l);
      setPathInput(l.path);
    } catch (e) {
      setError((e as Error).message);
    }
  }
  const closeRef = useRef(onClose);
  closeRef.current = onClose;
  useEffect(() => {
    browse("");
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && closeRef.current();
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);

  const here = list ? list.path.split(/[\\/]/).filter(Boolean).pop() ?? list.path : "";
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="modal folder-dialog" role="dialog" aria-modal="true" aria-label="Open folder">
        <div className="modal-head">
          <h2>Open folder</h2>
          <button className="tab-x" aria-label="Close" onClick={onClose}>×</button>
        </div>
        <form className="path-bar" onSubmit={(e) => { e.preventDefault(); browse(pathInput); }}>
          {list?.parent && <button type="button" className="btn" onClick={() => browse(list.parent)} title="Up one folder">Up</button>}
          <input value={pathInput} onChange={(e) => setPathInput(e.target.value)} aria-label="Folder path" spellCheck={false} autoFocus />
          <button className="btn">Go</button>
        </form>
        {list?.drives && list.drives.length > 0 && (
          <div className="drives">
            {list.drives.map((d) => <button key={d} className="key-btn" onClick={() => browse(d)}>{d}</button>)}
          </div>
        )}
        {error && <p className="notice warn" role="alert">{error}</p>}
        <div className="browser" aria-label="Folders">
          {list?.dirs.map((d) => (
            <div key={d.path} className={`dir-row-wrap ${d.isRepo ? "is-repo" : ""}`}>
              <button className="dir-row" onClick={() => browse(d.path)} onDoubleClick={() => d.isRepo && onPick(d.path)} title={d.path}>
                <span className="dir-icon" aria-hidden="true">{d.isRepo ? "◆" : "▸"}</span> {d.name}
              </button>
              {d.isRepo && <button className="btn small-btn" onClick={() => onPick(d.path)}>Open</button>}
            </div>
          ))}
          {list && list.dirs.length === 0 && <p className="muted small pad-x">No folders inside this one.</p>}
        </div>
        <div className="modal-foot">
          <span className="muted small">{list?.isRepo ? "This folder is a repository." : "◆ marks a repository."}</span>
          <span className="st-gap" />
          <button className="btn" onClick={onClose}>Cancel</button>
          <button className="btn primary" disabled={!list} onClick={() => list && onPick(list.path)}>Open {here}</button>
        </div>
      </div>
    </div>
  );
}

// RecentPicker switches workspaces: recent folders first, filtered as you type
// (Ctrl+R, like VS Code's Open Recent), with "Open Folder…" for anything else.
export function RecentPicker({ recent, current, onPick, onBrowse, onClose }: {
  recent: RecentFolder[]; current?: string; onPick: (p: string) => void; onBrowse: () => void; onClose: () => void;
}) {
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const list = recent.filter((r) => r.path !== current && (r.name + " " + r.path).toLowerCase().includes(q.trim().toLowerCase()));
  const rows = [...list.map((r) => ({ key: r.path, run: () => onPick(r.path), r })), { key: "browse", run: onBrowse, r: null }];
  useEffect(() => setSel(0), [q]);
  function key(e: React.KeyboardEvent) {
    if (e.key === "Escape") onClose();
    else if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, rows.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
    else if (e.key === "Enter") rows[sel]?.run();
  }
  return (
    <div className="qo-backdrop" onMouseDown={onClose}>
      <div className="qo" role="dialog" aria-label="Switch folder" onMouseDown={(e) => e.stopPropagation()}>
        <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={key} placeholder="Switch to a recent folder" aria-label="Folder name" />
        <ul role="listbox">
          {rows.map((row, i) => (
            <li key={row.key} role="option" aria-selected={i === sel} className={`${i === sel ? "is-sel" : ""} ${row.r ? "" : "qo-action"}`}
              onMouseEnter={() => setSel(i)} onClick={row.run}>
              {row.r ? (<><span>{row.r.name}</span><span className="muted small">{row.r.path}</span></>) : <span>Open Folder…</span>}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
