import { Suspense, lazy, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, setGitRepo, type GitState, type TermInfo } from "../api";
import { useLoad } from "../hooks";
import { clearDefinitionCache } from "../defs";
import { parse, type Route } from "../router";

import { OverviewView } from "../views/Overview";
import { SettingsView } from "../views/Settings";
import { Chat } from "./Chat";
import type { Cursor } from "./CodeTab";
import { DocTab } from "./DocTab";
import { clearFileCache, tabTitle, type NewTab, type Tab } from "./files";
import { Icon } from "./Icons";
import { DevicesPane } from "./Pair";
import { FolderDialog, ProgressBar, RecentPicker, isOpening, progressText, useWorkspace } from "./workspace";
import { QuickOpen } from "./QuickOpen";

import { ChangesPane } from "./Changes";
import { notificationsWanted, notify, stateOf, type AgentState } from "./attention";
import { PulsePane } from "./Pulse";
import { ReviewTab } from "./Review";
import { AgentsPane, DocsPane, Explorer, IssuesPane, SearchPane } from "./SidePanes";

// The editor (Monaco, ~1 MB) loads on first file open, not with the page.
const CodeTab = lazy(() => import("./CodeTab").then((m) => ({ default: m.CodeTab })));
// Likewise the map (React Flow) and the terminal (xterm.js).
const MapView = lazy(() => import("../views/Map").then((m) => ({ default: m.MapView })));
const SystemsView = lazy(() => import("../views/Systems").then((m) => ({ default: m.SystemsView })));
const DiffTab = lazy(() => import("./DiffTab").then((m) => ({ default: m.DiffTab })));
const TerminalPanel = lazy(() => import("./Terminal").then((m) => ({ default: m.TerminalPanel })));

type View = "explorer" | "search" | "changes" | "pulse" | "docs" | "issues" | "agents" | "devices";

const WELCOME: Tab = { id: "welcome", kind: "welcome" };

const tabId = (t: NewTab): string =>
  t.kind === "file" ? `file:${t.path}` : t.kind === "doc" ? `doc:${t.name}` : t.kind === "diff" ? `diff:${t.staged ? "s" : "w"}:${t.path}` : t.kind;

function loadTabs(key: string): { tabs: Tab[]; active: string } {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? "");
    if (Array.isArray(v.tabs) && v.tabs.length) return v;
  } catch { /* no saved layout */ }
  return { tabs: [WELCOME], active: "welcome" };
}

export function Workbench() {
  const ov = useLoad(api.overview, []);
  const tree = useLoad(api.tree, []);
  const repo = ov.data?.repo ?? "";
  const storeKey = `kuli.tabs.${ov.data?.root ?? ""}`;

  const [{ tabs, active }, setState] = useState(() => loadTabs("kuli.tabs.pending"));
  // Phones: the side bar is an overlay, so it starts closed and gets out of the way.
  const narrow = useNarrow();
  const [view, setView] = useState<View | null>(() => (matchMedia(NARROW).matches ? null : "explorer"));
  const [searchQ, setSearchQ] = useState("");
  const [chat, setChat] = useState(false);
  const [quick, setQuick] = useState(false);
  const [cursor, setCursor] = useState<Cursor | null>(null);
  const [termOpen, setTermOpen] = useState(false);
  const [termH, setTermH] = useState(280);
  const [termFocus, setTermFocus] = useState<string | undefined>();
  const [git, setGit] = useState<GitState | null>(null);
  // Which repository Changes works on, when the folder holds several (kept per folder).
  const repoKey = `kuli.repo.${ov.data?.root ?? ""}`;
  const [gitRepo, setGitRepoState] = useState("");
  useEffect(() => {
    let saved = "";
    try { saved = localStorage.getItem(repoKey) ?? ""; } catch { /* storage off */ }
    setGitRepo(saved);
    setGitRepoState(saved);
  }, [repoKey]);
  const chooseRepo = (path: string) => {
    setGitRepo(path);
    setGitRepoState(path);
    try { localStorage.setItem(repoKey, path); } catch { /* storage off */ }
  };
  // Bumped whenever the git status actually changes, so open diffs re-read both sides.
  const [gitVersion, setGitVersion] = useState(0);
  const refreshGit = useCallback(() => {
    api.gitStatus().then((g) => {
      setGit((cur) => {
        if (JSON.stringify(cur) === JSON.stringify(g)) return cur;
        setGitVersion((v) => v + 1);
        return g;
      });
    }, () => {});
  }, []);
  // Keep the change count fresh: on an interval, and whenever the window regains focus.
  useEffect(() => {
    refreshGit();
    const t = setInterval(refreshGit, view === "changes" ? 3000 : 10000);
    addEventListener("focus", refreshGit);
    return () => { clearInterval(t); removeEventListener("focus", refreshGit); };
  }, [refreshGit, view, gitRepo]);

  // Terminal/agent sessions, polled for Pulse and for "needs attention" notifications.
  const [terms, setTerms] = useState<TermInfo[] | null>(null);
  const lastStates = useRef<Map<string, AgentState>>(new Map());
  useEffect(() => {
    if (!ov.data?.terminals) return;
    const poll = () => api.terminals().then((r) => {
      setTerms(r.terminals);
      const now = Date.now();
      for (const t of r.terminals.filter((x) => x.kind === "agent")) {
        const st = stateOf(t, now);
        const before = lastStates.current.get(t.id);
        lastStates.current.set(t.id, st);
        if (!before || before === st || !document.hidden || !notificationsWanted()) continue;
        if (st === "waiting") notify(`${t.title} is waiting`, t.tail?.at(-1) ?? "It may need your answer.", t.id);
        if (st === "exited") notify(`${t.title} finished`, `Exit code ${t.exitCode}. Review its changes.`, t.id);
      }
    }, () => {});
    poll();
    const iv = setInterval(poll, view === "pulse" ? 3000 : 8000);
    return () => clearInterval(iv);
  }, [ov.data?.terminals, view]);

  async function startAgent(id: string) {
    const t = await api.startAgent(id, 110, Math.max(10, Math.floor(termH / 18)));
    setTermFocus(t.id);
    setTermOpen(true);
  }
  const [scan, setScan] = useState<"idle" | "busy" | string>("idle");
  const [dirty, setDirty] = useState<Set<string>>(() => new Set());
  // Open Folder: the current folder stays usable until the new one is ready.
  const ws = useWorkspace();
  const canSwitch = !!ws.info?.canOpen;
  const [browse, setBrowse] = useState(false);
  const [switcher, setSwitcher] = useState(false);
  const job = ws.job;
  const root = ov.data?.root;
  const lastPhase = useRef(job?.phase);
  useEffect(() => {
    if (!job || !root) return;
    if (job.ready && job.path !== root && ws.info?.root === job.path) {
      location.hash = "#/";
      location.reload(); // the new folder is ready: start fresh on it
    } else if (job.path === root && job.phase === "done" && lastPhase.current !== "done") {
      clearFileCache(); // caught up with changes since last time
      clearDefinitionCache();
      ov.reload();
      tree.reload();
    }
    lastPhase.current = job.phase;
  }, [job, root, ws.info?.root]); // eslint-disable-line react-hooks/exhaustive-deps
  // Several projects in the folder (repositories, or apps of a monorepo): the Systems view.
  const sys = useLoad(api.systems, [ov.data?.root, ov.data?.scannedAt]);
  const projects = sys.data && sys.data.projects.length >= 2 ? sys.data : null;
  function pickFolder(path: string) {
    setBrowse(false);
    setSwitcher(false);
    if (dirty.size > 0 && !confirm(`${dirty.size} file(s) have unsaved changes that will be lost. Open another folder anyway?`)) return;
    ws.open(path);
  }
  const openingNow = isOpening(job) ? job : null;
  const markDirty = useCallback((path: string, d: boolean) => {
    setDirty((cur) => {
      if (cur.has(path) === d) return cur;
      const n = new Set(cur);
      if (d) n.add(path); else n.delete(path);
      return n;
    });
  }, []);

  // Unsaved edits: warn before the page is closed or reloaded.
  useEffect(() => {
    if (dirty.size === 0) return;
    const warn = (e: BeforeUnloadEvent) => e.preventDefault();
    addEventListener("beforeunload", warn);
    return () => removeEventListener("beforeunload", warn);
  }, [dirty.size]);

  // Restore this repository's tabs once we know which repository it is.
  // Tabs opened before that (e.g. from a deep link) are kept and stay active.
  useEffect(() => {
    if (!ov.data) return;
    setState((cur) => {
      const saved = loadTabs(storeKey);
      const extra = cur.tabs.filter((t) => t.id !== "welcome" && !saved.tabs.some((s) => s.id === t.id));
      const tabs = saved.tabs.map((s) => cur.tabs.find((t) => t.id === s.id && t.id !== "welcome") ?? s).concat(extra);
      const active = cur.active !== "welcome" && tabs.some((t) => t.id === cur.active) ? cur.active : saved.active;
      return { tabs, active };
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ov.data?.root]);
  useEffect(() => {
    if (!ov.data) return;
    try { localStorage.setItem(storeKey, JSON.stringify({ tabs, active })); } catch { /* storage unavailable */ }
  }, [tabs, active, storeKey, ov.data]);

  const openTab = useCallback((t: NewTab) => {
    const id = tabId(t);
    setState((s) => {
      const tab = { ...t, id } as Tab;
      const i = s.tabs.findIndex((x) => x.id === id);
      const tabs = i >= 0 ? s.tabs.map((x, j) => (j === i ? tab : x)) : [...s.tabs, tab];
      return { tabs, active: id };
    });
  }, []);

  const openFile = useCallback((path: string, from?: number, to?: number) => {
    openTab({ kind: "file", path, from, to });
    if (matchMedia(NARROW).matches) setView(null);
  }, [openTab]);

  const closeTab = (id: string) => {
    const t = tabs.find((x) => x.id === id);
    if (t?.kind === "file" && dirty.has(t.path) && !confirm(`${t.path} has unsaved changes. Close it and keep them only in this browser tab?`)) return;
    setState((s) => {
      const i = s.tabs.findIndex((t) => t.id === id);
      const tabs = s.tabs.filter((t) => t.id !== id);
      const next = s.active === id ? (tabs[Math.min(i, tabs.length - 1)]?.id ?? "") : s.active;
      return { tabs, active: next };
    });
  };

  // Deep links (#/file/…#L10-L20 from citations, #/map?c=…, #/docs/…) open tabs.
  useEffect(() => {
    const go = (r: Route) => {
      if (r.page === "file") openFile(r.path, r.from, r.to);
      else if (r.page === "map") openTab({ kind: "map", component: r.component });
      else if (r.page === "docs" && r.name) openTab({ kind: "doc", name: r.name });
      else if (r.page === "docs") setView("docs");
      else if (r.page === "search") { setView("search"); setSearchQ(r.q); }
      else if (r.page === "issues") setView("issues");
      else if (r.page === "ask") setChat(true);
      else if (r.page === "pulse") setView("pulse");
    };
    const on = () => {
      if (location.hash && location.hash !== "#/") go(parse(location.hash));
      history.replaceState(null, "", "#/");
    };
    on();
    addEventListener("hashchange", on);
    return () => removeEventListener("hashchange", on);
  }, [openFile, openTab]);

  // VS Code keys: Ctrl/Cmd+P quick open, Ctrl+Shift+F search, Ctrl+B side bar, Ctrl+L chat.
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey;
      if (!mod) return;
      const k = e.key.toLowerCase();
      if (e.key === "`" || e.code === "Backquote") { e.preventDefault(); if (ov.data?.terminals) setTermOpen((o) => !o); }
      else if (k === "p" && !e.shiftKey) { e.preventDefault(); setQuick(true); }
      else if (k === "f" && e.shiftKey) { e.preventDefault(); setView("search"); }
      else if (k === "g" && e.shiftKey) { e.preventDefault(); setView("changes"); }
      else if (k === "b") { e.preventDefault(); setView((v) => (v ? null : "explorer")); }
      else if (k === "l") { e.preventDefault(); setChat((c) => !c); }
      else if (e.key === ",") { e.preventDefault(); openTab({ kind: "settings" }); }
      else if (k === "o" && canSwitch) { e.preventDefault(); setBrowse(true); }
      else if (k === "r" && canSwitch && !e.shiftKey) { e.preventDefault(); setSwitcher(true); }
      else if (k === "w" && e.altKey) { e.preventDefault(); closeTab(active); }
    };
    addEventListener("keydown", key);
    return () => removeEventListener("keydown", key);
  });

  async function rescan() {
    setScan("busy");
    try {
      const r = await api.rescan();
      clearFileCache();
      clearDefinitionCache();
      ov.reload();
      tree.reload();
      setScan(`Rescanned ${r.files.toLocaleString()} files`);
    } catch (e) {
      setScan((e as Error).message);
    }
  }

  const current = tabs.find((t) => t.id === active);
  const activePath = current?.kind === "file" ? current.path : undefined;
  const files = useMemo(() => tree.data?.files ?? [], [tree.data]);

  const gitChanges = git?.repos ? git.repos.reduce((n, r) => n + r.changes, 0) : git?.status.git ? git.status.changes.length : 0;
  const activeRepo = git?.repos?.find((r) => r.path === git.repo);
  const activity: { v: View; label: string; icon: () => React.ReactNode }[] = [
    { v: "explorer", label: "Explorer (Ctrl+B)", icon: Icon.files },
    { v: "search", label: "Search (Ctrl+Shift+F)", icon: Icon.search },
    { v: "changes", label: "Changes (Ctrl+Shift+G)", icon: Icon.git },
    ...(ov.data?.terminals ? [{ v: "pulse" as View, label: "Pulse: agents and changes", icon: Icon.pulse }] : []),
    { v: "docs", label: "Docs", icon: Icon.docs },
    { v: "issues", label: "Issues", icon: Icon.issues },
    ...(ov.data?.terminals ? [{ v: "agents" as View, label: "Agents", icon: Icon.agent }] : []),
    { v: "devices", label: "Devices", icon: Icon.devices },
  ];

  return (
    <div className={`wb ${view ? "" : "no-side"} ${chat ? "with-chat" : ""}`}>
      <nav className="activity" aria-label="Views">
        <a className="act-mark" href="#/" onClick={(e) => { e.preventDefault(); openTab({ kind: "welcome" }); }} title="Welcome"><img src="/mark.png" alt="KuliDev" /></a>
        {activity.map((a) => (
          <button key={a.v} className={`act ${view === a.v ? "on" : ""}`} title={a.label} aria-label={a.label}
            aria-pressed={view === a.v} onClick={() => setView((v) => (v === a.v ? null : a.v))}>
            <a.icon />
            {a.v === "changes" && gitChanges > 0 && (
              <span className="act-badge" aria-label={`${gitChanges} changes`}>{Math.min(gitChanges, 99)}</span>
            )}
          </button>
        ))}
        <button className="act" title="Map" aria-label="Open map" onClick={() => openTab({ kind: "map" })}><Icon.map /></button>
        {projects && (
          <button className="act" title={`Systems: ${projects.projects.length} projects and how they connect`} aria-label="Open systems diagram"
            onClick={() => openTab({ kind: "systems" })}><Icon.systems /></button>
        )}
        <span className="act-gap" />
        <button className="act" title="Settings (Ctrl+,)" aria-label="Settings" onClick={() => openTab({ kind: "settings" })}><Icon.gear /></button>
        <button className={`act ${chat ? "on" : ""}`} title="Ask Kuli (Ctrl+L)" aria-label="Ask Kuli" aria-pressed={chat} onClick={() => setChat((c) => !c)}>
          <Icon.chat />
        </button>
      </nav>

      {view && narrow && <div className="side-backdrop" onClick={() => setView(null)} aria-hidden="true" />}
      {view && (
        <aside className="side" aria-label="Side bar">
          {view === "explorer" && <Explorer files={files} repo={repo} active={activePath} onOpen={openFile} />}
          {view === "search" && <SearchPane initial={searchQ} onOpen={openFile} />}
          {view === "docs" && <DocsPane onOpenDoc={(name) => { openTab({ kind: "doc", name }); if (narrow) setView(null); }} />}
          {view === "issues" && <IssuesPane onOpen={openFile} />}
          {view === "changes" && (
            <ChangesPane status={git?.status ?? null} writable={git?.writable ?? false} onChanged={refreshGit}
              repos={git?.repos} repo={git?.repo ?? ""} onRepo={chooseRepo}
              onOpenDiff={(path, staged) => { openTab({ kind: "diff", path, staged }); if (narrow) setView(null); }} />
          )}
          {view === "agents" && <AgentsPane onStart={startAgent} />}
          {view === "pulse" && (
            <PulsePane terminals={terms} git={git?.status ?? null}
              onOpenTerminal={(id) => { setTermFocus(id); setTermOpen(true); if (narrow) setView(null); }}
              onReview={() => { openTab({ kind: "review" }); if (narrow) setView(null); }} />
          )}
          {view === "devices" && <DevicesPane />}
        </aside>
      )}

      <section className="editor-area" aria-label="Editor">
        {openingNow && <ProgressBar job={openingNow} className="edge" />}
        <div className="tabs" role="tablist">
          {tabs.map((t) => (
            <div key={t.id} role="tab" aria-selected={t.id === active} className={`tab ${t.id === active ? "on" : ""}`}
              onClick={() => setState((s) => ({ ...s, active: t.id }))}
              onAuxClick={(e) => e.button === 1 && closeTab(t.id)}
              title={t.kind === "file" ? t.path : tabTitle(t)}>
              <span className="tab-name">{tabTitle(t)}</span>
              {t.kind === "file" && dirty.has(t.path) && <span className="tab-dirty" aria-label="unsaved changes">●</span>}
              <button className="tab-x" aria-label={`Close ${tabTitle(t)}`} onClick={(e) => { e.stopPropagation(); closeTab(t.id); }}>
                <Icon.close />
              </button>
            </div>
          ))}
        </div>
        {current?.kind === "file" && (
          <div className="crumbs" aria-label="Path">
            {current.path.split("/").map((p, i, a) => <span key={i} className={i === a.length - 1 ? "last" : ""}>{p}</span>)}
          </div>
        )}
        <div className="editor-body">
          {!current && <EmptyEditor onQuick={() => setQuick(true)} />}
          {current?.kind === "file" && (
            <Suspense fallback={<p className="muted pad">Loading the editor…</p>}>
            <CodeTab key={current.path} path={current.path} from={current.from} to={current.to} onOpen={openFile} onCursor={setCursor} onDirty={markDirty} />
            </Suspense>
          )}
          {current?.kind === "map" && (
            <Suspense fallback={<p className="muted pad">Loading the map…</p>}>
              <MapView selected={current.component} />
            </Suspense>
          )}
          {current?.kind === "systems" && (
            <Suspense fallback={<p className="muted pad">Loading the diagram…</p>}>
              <SystemsView />
            </Suspense>
          )}
          {current?.kind === "settings" && (
            <SettingsView root={ov.data?.root} onRescan={rescan} onOpenFolder={() => setBrowse(true)} onSaved={ov.reload} />
          )}
          {current?.kind === "doc" && <DocTab name={current.name} />}
          {current?.kind === "review" && <ReviewTab git={git?.status ?? null} writable={git?.writable ?? false} onChanged={refreshGit} />}
          {current?.kind === "diff" && (
            <Suspense fallback={<p className="muted pad">Loading the diff…</p>}>
              <DiffTab key={`${current.id}@${gitVersion}`} path={current.path} staged={current.staged} narrow={narrow} />
            </Suspense>
          )}
          {current?.kind === "welcome" && (
            <div className="welcome-tab">
              {canSwitch && <button className="btn switch-folder" onClick={() => setBrowse(true)}>Open Folder…</button>}
              {projects && (
                <section className="projects-strip">
                  <h2>{projects.projects.length} projects in this folder</h2>
                  <p className="muted small">
                    {projects.links.length > 0
                      ? `${new Set(projects.links.map((l) => `${l.from}>${l.to}`)).size} connections found between them, each with the code that proves it.`
                      : "No connections found between them yet."}
                  </p>
                  <ul className="plain">
                    {projects.projects.map((p) => (
                      <li key={p.path || "."}>{p.name}<span className="muted">{p.langs[0]?.lang ?? ""}</span></li>
                    ))}
                  </ul>
                  <button className="btn primary" onClick={() => openTab({ kind: "systems" })}>Open the systems diagram</button>
                </section>
              )}
              <OverviewView ov={ov} onRescan={rescan} />
            </div>
          )}
        </div>
        {termOpen && ov.data?.terminals && (
          <Suspense fallback={<p className="muted pad">Loading the terminal…</p>}>
            <TerminalPanel height={termH} focus={termFocus} onResize={setTermH} onClose={() => setTermOpen(false)} />
          </Suspense>
        )}
      </section>

      {chat && <Chat enabled={ov.data?.llm ?? false} model={ov.data?.model} onClose={() => setChat(false)} />}

      <footer className="status" aria-label="Status">
        {canSwitch ? (
          <button className="st-btn st-repo" onClick={() => setSwitcher(true)} title="Switch folder (Ctrl+R)">{repo} ▾</button>
        ) : (
          <span className="st-repo">{repo}</span>
        )}
        {activeRepo ? (
          <button className="st-btn st-wide" onClick={() => setView("changes")} title={`${git?.repos?.length} repositories in this folder`}>
            {activeRepo.name}: {activeRepo.branch || "no branch"}
          </button>
        ) : (
          ov.data && <span className="st-wide">{git?.status.branch ? `${git.status.branch} ` : ""}{ov.data.commit ? ov.data.commit.slice(0, 8) : "no commit"}</span>
        )}
        {ov.data?.stale && <span className="st-warn">{narrow ? "Stale" : "New commits since scan"}</span>}
        <button className="st-btn" onClick={rescan} disabled={scan === "busy"}>{scan === "busy" ? "Scanning…" : "Rescan"}</button>
        {scan !== "idle" && scan !== "busy" && <span>{scan}</span>}
        {openingNow && (
          <span className="st-progress" title={progressText(openingNow)}>
            <ProgressBar job={openingNow} />
            <span>{openingNow.path === root ? progressText(openingNow) : `Opening ${openingNow.name}: ${progressText(openingNow)}`}</span>
          </span>
        )}
        <span className="st-gap" />
        {cursor && <span className="st-wide">Ln {cursor.line}, Col {cursor.col}</span>}
        {cursor && <span className="st-wide">{cursor.lang}</span>}
        <span className="st-wide">{ov.data?.llm ? `Model: ${ov.data.model}` : "No model"}</span>
        {dirty.size > 0 && <span className="st-warn">{dirty.size} unsaved</span>}
        {ov.data?.terminals && (
          <button className="st-btn" onClick={() => setTermOpen((o) => !o)} title="Toggle terminal (Ctrl+`)">Terminal</button>
        )}
        <span className="st-wide">{ov.data?.readOnly ? "Read-only" : "Ctrl+S saves"}</span>
      </footer>

      {browse && <FolderDialog onPick={pickFolder} onClose={() => setBrowse(false)} />}
      {switcher && (
        <RecentPicker recent={ws.recent} current={root} onPick={pickFolder} onClose={() => setSwitcher(false)}
          onBrowse={() => { setSwitcher(false); setBrowse(true); }} />
      )}
      {quick && <QuickOpen files={files} onClose={() => setQuick(false)} onPick={(p) => { setQuick(false); openFile(p); }} />}
    </div>
  );
}

const NARROW = "(max-width: 900px)";

function useNarrow(): boolean {
  const [narrow, setNarrow] = useState(() => matchMedia(NARROW).matches);
  useEffect(() => {
    const mq = matchMedia(NARROW);
    const on = () => setNarrow(mq.matches);
    mq.addEventListener("change", on);
    return () => mq.removeEventListener("change", on);
  }, []);
  return narrow;
}

function EmptyEditor({ onQuick }: { onQuick: () => void }) {
  return (
    <div className="empty-editor">
      <p className="wordmark-big">Kuli<span>Dev</span></p>
      <ul className="plain keys">
        <li><button className="btn-link" onClick={onQuick}>Go to file</button> <kbd>Ctrl</kbd> <kbd>P</kbd></li>
        <li>Search definitions <kbd>Ctrl</kbd> <kbd>Shift</kbd> <kbd>F</kbd></li>
        <li>Ask Kuli <kbd>Ctrl</kbd> <kbd>L</kbd></li>
        <li>Toggle side bar <kbd>Ctrl</kbd> <kbd>B</kbd></li>
        <li>Terminal <kbd>Ctrl</kbd> <kbd>`</kbd></li>
      </ul>
    </div>
  );
}
