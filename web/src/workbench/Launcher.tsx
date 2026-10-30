import { useEffect, useState } from "react";
import { Icon } from "./Icons";
import { TitleBar, type Command } from "./TitleBar";
import { isDark, setThemePref } from "../theme";
import { FolderDialog, OpeningCard, ProgressBar, RecentList, isOpening, progressText, useWorkspace } from "./workspace";

// The workbench with no folder open, like an empty editor window: open a folder or a
// recent one; opening shows its progress and switches to the full workbench when ready.
export function Launcher({ onOpened }: { onOpened: () => void }) {
  const ws = useWorkspace();
  const [browse, setBrowse] = useState(false);
  const job = ws.job;
  const opening = isOpening(job) && !job.ready;

  useEffect(() => {
    if (ws.info?.open && job?.ready) onOpened();
  }, [ws.info?.open, job?.ready, onOpened]);
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "o") {
        e.preventDefault();
        setBrowse(true);
      }
    };
    addEventListener("keydown", onKey);
    return () => removeEventListener("keydown", onKey);
  }, []);

  const pick = (path: string) => {
    setBrowse(false);
    ws.open(path);
  };
  const failed = job?.phase === "error" ? job.error : "";
  const commands: Command[] = [
    { id: "open-folder", menu: "File", label: "Open Folder…", keys: "Ctrl+O", run: () => setBrowse(true) },
    ...ws.recent.slice(0, 6).map((r, i) => ({ id: `recent-${r.path}`, menu: "File" as const, label: r.name, run: () => pick(r.path), sep: i === 0 })),
    { id: "t-theme", menu: "View", label: isDark() ? "Switch to Light Theme" : "Switch to Dark Theme", run: () => setThemePref(isDark() ? "light" : "dark") },
  ];
  const idle = [Icon.search, Icon.git, Icon.docs, Icon.issues];
  return (
    <div className="wb">
      <TitleBar commands={commands} version={ws.info?.version} center="Open a folder" onCenter={() => setBrowse(true)} />
      <nav className="activity" aria-label="Views">
        <button className="act on" title="Explorer" aria-label="Explorer" aria-pressed="true"><Icon.files /></button>
        {idle.map((I, i) => (
          <button key={i} className="act" disabled title="Open a folder first" aria-label="Open a folder first"><I /></button>
        ))}
        <span className="act-gap" />
      </nav>

      <aside className="side" aria-label="Side bar">
        <h2 className="pane-title">Explorer</h2>
        <div className="side-empty">
          {opening ? (
            <>
              <p>Opening <strong>{job.name}</strong></p>
              <ProgressBar job={job} />
              <p className="muted small">{progressText(job)}</p>
            </>
          ) : (
            <>
              <p className="muted small">No folder is open.</p>
              <button className="btn primary wide" onClick={() => setBrowse(true)}>Open Folder</button>
            </>
          )}
        </div>
        {ws.recent.length > 0 && (
          <>
            <h2 className="pane-title">Recent</h2>
            <div className="side-recent"><RecentList recent={ws.recent} onOpen={pick} disabled={opening} /></div>
          </>
        )}
      </aside>

      <section className="editor-area" aria-label="Editor">
        <div className="tabs" role="tablist">
          <div className="tab on" role="tab" aria-selected="true"><span className="tab-name">Welcome</span></div>
        </div>
        {opening && <ProgressBar job={job} className="edge" />}
        <div className="editor-body">
          <div className="start-page">
            <p className="wordmark-big">Kuli<span>Dev</span></p>
            <p className="start-lede">Docs-nya mana? Ini. Map a codebase, read it with evidence, and change it.</p>
            {(failed || ws.error) && <p className="notice warn" role="alert">{failed || ws.error}</p>}
            {opening ? (
              <OpeningCard job={job} />
            ) : (
              <div className="start-cols">
                <section>
                  <h2>Start</h2>
                  <ul className="plain start-actions">
                    <li>
                      <button className="btn-link" onClick={() => setBrowse(true)}>Open Folder…</button>
                      <span className="keys-inline"><kbd>Ctrl</kbd> <kbd>O</kbd></span>
                    </li>
                  </ul>
                </section>
                <section>
                  <h2>Recent</h2>
                  {ws.recent.length > 0 ? (
                    <RecentList recent={ws.recent.slice(0, 6)} onOpen={pick} />
                  ) : (
                    <p className="muted small">Folders you open show up here.</p>
                  )}
                </section>
              </div>
            )}
          </div>
        </div>
      </section>

      <footer className="status" aria-label="Status">
        <span className="st-repo">{opening ? job.name : "No folder open"}</span>
        {opening && <span>{progressText(job)}</span>}
        <span className="st-gap" />
        <span className="st-wide">KuliDev</span>
      </footer>
      {browse && <FolderDialog onPick={pick} onClose={() => setBrowse(false)} />}
    </div>
  );
}
