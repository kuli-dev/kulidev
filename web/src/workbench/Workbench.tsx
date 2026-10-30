import { Suspense, lazy, useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { useLoad } from "../hooks";
import { clearDefinitionCache } from "../defs";
import { parse, type Route } from "../router";

import { OverviewView } from "../views/Overview";
import { Chat } from "./Chat";
import type { Cursor } from "./CodeTab";
import { DocTab } from "./DocTab";
import { clearFileCache, tabTitle, type NewTab, type Tab } from "./files";
import { Icon } from "./Icons";
import { QuickOpen } from "./QuickOpen";

import { AgentsPane, DocsPane, Explorer, IssuesPane, SearchPane } from "./SidePanes";

// The editor (Monaco, ~1 MB) loads on first file open, not with the page.
const CodeTab = lazy(() => import("./CodeTab").then((m) => ({ default: m.CodeTab })));
// Likewise the map (React Flow) and the terminal (xterm.js).
const MapView = lazy(() => import("../views/Map").then((m) => ({ default: m.MapView })));
const TerminalPanel = lazy(() => import("./Terminal").then((m) => ({ default: m.TerminalPanel })));

type View = "explorer" | "search" | "docs" | "issues" | "agents";

const WELCOME: Tab = { id: "welcome", kind: "welcome" };

const tabId = (t: NewTab): string => (t.kind === "file" ? `file:${t.path}` : t.kind === "doc" ? `doc:${t.name}` : t.kind);

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
  const [view, setView] = useState<View | null>("explorer");
  const [searchQ, setSearchQ] = useState("");
  const [chat, setChat] = useState(false);
  const [quick, setQuick] = useState(false);
  const [cursor, setCursor] = useState<Cursor | null>(null);
  const [termOpen, setTermOpen] = useState(false);
  const [termH, setTermH] = useState(280);
  const [termFocus, setTermFocus] = useState<string | undefined>();

  async function startAgent(id: string) {
    const t = await api.startAgent(id, 110, Math.max(10, Math.floor(termH / 18)));
    setTermFocus(t.id);
    setTermOpen(true);
  }
  const [scan, setScan] = useState<"idle" | "busy" | string>("idle");
  const [dirty, setDirty] = useState<Set<string>>(() => new Set());
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

  const openFile = useCallback((path: string, from?: number, to?: number) => openTab({ kind: "file", path, from, to }), [openTab]);

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
      else if (k === "b") { e.preventDefault(); setView((v) => (v ? null : "explorer")); }
      else if (k === "l") { e.preventDefault(); setChat((c) => !c); }
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

  const activity: { v: View; label: string; icon: () => React.ReactNode }[] = [
    { v: "explorer", label: "Explorer (Ctrl+B)", icon: Icon.files },
    { v: "search", label: "Search (Ctrl+Shift+F)", icon: Icon.search },
    { v: "docs", label: "Docs", icon: Icon.docs },
    { v: "issues", label: "Issues", icon: Icon.issues },
    ...(ov.data?.terminals ? [{ v: "agents" as View, label: "Agents", icon: Icon.agent }] : []),
  ];

  return (
    <div className={`wb ${view ? "" : "no-side"} ${chat ? "with-chat" : ""}`}>
      <nav className="activity" aria-label="Views">
        <a className="act-mark" href="#/" onClick={(e) => { e.preventDefault(); openTab({ kind: "welcome" }); }} title="Welcome">k</a>
        {activity.map((a) => (
          <button key={a.v} className={`act ${view === a.v ? "on" : ""}`} title={a.label} aria-label={a.label}
            aria-pressed={view === a.v} onClick={() => setView((v) => (v === a.v ? null : a.v))}>
            <a.icon />
          </button>
        ))}
        <button className="act" title="Map" aria-label="Open map" onClick={() => openTab({ kind: "map" })}><Icon.map /></button>
        <span className="act-gap" />
        <button className={`act ${chat ? "on" : ""}`} title="Ask Kuli (Ctrl+L)" aria-label="Ask Kuli" aria-pressed={chat} onClick={() => setChat((c) => !c)}>
          <Icon.chat />
        </button>
      </nav>

      {view && (
        <aside className="side" aria-label="Side bar">
          {view === "explorer" && <Explorer files={files} repo={repo} active={activePath} onOpen={openFile} />}
          {view === "search" && <SearchPane initial={searchQ} onOpen={openFile} />}
          {view === "docs" && <DocsPane onOpenDoc={(name) => openTab({ kind: "doc", name })} />}
          {view === "issues" && <IssuesPane onOpen={openFile} />}
          {view === "agents" && <AgentsPane onStart={startAgent} />}
        </aside>
      )}

      <section className="editor-area" aria-label="Editor">
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
          {current?.kind === "doc" && <DocTab name={current.name} />}
          {current?.kind === "welcome" && <div className="welcome-tab"><OverviewView ov={ov} onRescan={rescan} /></div>}
        </div>
        {termOpen && ov.data?.terminals && (
          <Suspense fallback={<p className="muted pad">Loading the terminal…</p>}>
            <TerminalPanel height={termH} focus={termFocus} onResize={setTermH} onClose={() => setTermOpen(false)} />
          </Suspense>
        )}
      </section>

      {chat && <Chat enabled={ov.data?.llm ?? false} model={ov.data?.model} onClose={() => setChat(false)} />}

      <footer className="status" aria-label="Status">
        <span className="st-repo">{repo}</span>
        {ov.data && <span>{ov.data.commit ? ov.data.commit.slice(0, 8) : "no commit"}</span>}
        {ov.data?.stale && <span className="st-warn">New commits since scan</span>}
        <button className="st-btn" onClick={rescan} disabled={scan === "busy"}>{scan === "busy" ? "Scanning…" : "Rescan"}</button>
        {scan !== "idle" && scan !== "busy" && <span>{scan}</span>}
        <span className="st-gap" />
        {cursor && <span>Ln {cursor.line}, Col {cursor.col}</span>}
        {cursor && <span>{cursor.lang}</span>}
        <span>{ov.data?.llm ? `Model: ${ov.data.model}` : "No model"}</span>
        {dirty.size > 0 && <span className="st-warn">{dirty.size} unsaved</span>}
        {ov.data?.terminals && (
          <button className="st-btn" onClick={() => setTermOpen((o) => !o)} title="Toggle terminal (Ctrl+`)">Terminal</button>
        )}
        <span>{ov.data?.readOnly ? "Read-only" : "Ctrl+S saves"}</span>
      </footer>

      {quick && <QuickOpen files={files} onClose={() => setQuick(false)} onPick={(p) => { setQuick(false); openFile(p); }} />}
    </div>
  );
}

function EmptyEditor({ onQuick }: { onQuick: () => void }) {
  return (
    <div className="empty-editor">
      <p className="wordmark-big">kuli<span>dev</span></p>
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
