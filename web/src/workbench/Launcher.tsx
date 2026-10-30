import { useEffect, useState } from "react";
import { api, type DirList, type WorkspaceInfo } from "../api";

// "Open folder": recent folders and a folder browser. Picking a folder scans it (fast
// when it was scanned before) and opens the workbench on it.
export function Launcher({ onOpened }: { onOpened: () => void }) {
  const [info, setInfo] = useState<WorkspaceInfo | null>(null);
  const [list, setList] = useState<DirList | null>(null);
  const [pathInput, setPathInput] = useState("");
  const [busy, setBusy] = useState("");
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

  useEffect(() => {
    api.workspace().then(setInfo, () => {});
    browse("");
  }, []);

  async function open(path: string) {
    const name = path.split(/[\\/]/).filter(Boolean).pop() ?? path;
    setBusy(name);
    setError("");
    try {
      await api.openWorkspace(path);
      onOpened();
    } catch (e) {
      setError((e as Error).message);
      setBusy("");
    }
  }

  const recent = (info?.recent ?? []).filter((r) => r.exists);
  return (
    <main className="launcher">
      <div className="launcher-col">
        <p className="wordmark-big">kuli<span>dev</span></p>
        <p className="muted">Open a project folder. KuliDev reads it once and keeps its map up to date; folders you opened before open instantly.</p>
        {error && <p className="notice warn" role="alert">{error}</p>}

        {recent.length > 0 && (
          <section>
            <h2>Recent</h2>
            <ul className="plain recent">
              {recent.map((r) => (
                <li key={r.path}>
                  <button className="recent-row" disabled={!!busy} onClick={() => open(r.path)}>
                    <strong>{r.name}</strong>
                    <span className="muted small">{r.path}</span>
                  </button>
                </li>
              ))}
            </ul>
          </section>
        )}

        <section>
          <h2>Open a folder</h2>
          <form className="path-bar" onSubmit={(e) => { e.preventDefault(); browse(pathInput); }}>
            <input value={pathInput} onChange={(e) => setPathInput(e.target.value)} aria-label="Folder path" spellCheck={false} />
            <button className="btn" disabled={!!busy}>Go</button>
          </form>
          {list?.drives && list.drives.length > 0 && (
            <div className="drives">
              {list.drives.map((d) => <button key={d} className="key-btn" onClick={() => browse(d)}>{d}</button>)}
            </div>
          )}
          <div className="browser" role="listbox" aria-label="Folders">
            {list?.parent && (
              <button className="dir-row" onClick={() => browse(list.parent)}>
                <span className="dir-icon" aria-hidden="true">↰</span> ..
              </button>
            )}
            {list?.dirs.map((d) => (
              <div key={d.path} className={`dir-row-wrap ${d.isRepo ? "is-repo" : ""}`}>
                <button className="dir-row" onClick={() => browse(d.path)} title={d.path}>
                  <span className="dir-icon" aria-hidden="true">{d.isRepo ? "◆" : "▸"}</span> {d.name}
                </button>
                {d.isRepo && <button className="btn small-btn" disabled={!!busy} onClick={() => open(d.path)}>Open</button>}
              </div>
            ))}
            {list && list.dirs.length === 0 && <p className="muted small pad-x">No sub-folders here.</p>}
          </div>
          {list && (
            <div className="open-here">
              <button className="btn primary" disabled={!!busy} onClick={() => open(list.path)}>
                Open {list.path.split(/[\\/]/).filter(Boolean).pop() ?? list.path}
              </button>
              <span className="muted small">{list.isRepo ? "This folder is a repository." : "◆ marks repositories."}</span>
            </div>
          )}
        </section>
      </div>
      {busy && (
        <div className="launcher-busy" role="status">
          <p className="wordmark-big">kuli<span>dev</span></p>
          <p>Reading <strong>{busy}</strong>…</p>
          <p className="muted small">The first time takes a few seconds for big repositories; after that it's instant.</p>
        </div>
      )}
    </main>
  );
}
