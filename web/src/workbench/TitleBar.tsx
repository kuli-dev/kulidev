import { useEffect, useMemo, useRef, useState } from "react";
import { fuzzyScore } from "./files";

// One list of commands drives the menu bar and the command palette, so anything in a
// menu can also be typed (Ctrl+Shift+P), like VS Code.
export type Command = {
  id: string;
  label: string;
  menu: "File" | "Edit" | "View" | "Go" | "Terminal" | "Help";
  keys?: string;
  run: () => void;
  disabled?: boolean;
  sep?: boolean; // a divider before this item in its menu
};

const MENUS: Command["menu"][] = ["File", "Edit", "View", "Go", "Terminal", "Help"];

// Fired by Edit menu items; the open editor performs them.
export const editorAction = (id: string) => dispatchEvent(new CustomEvent("kuli-editor-action", { detail: id }));

const isLocal = () => ["localhost", "127.0.0.1", "[::1]", "::1"].includes(location.hostname);

export function TitleBar({ commands, center, onCenter, toggles, onSettings }: {
  commands: Command[];
  center: string;
  onCenter: () => void;
  toggles?: { side: boolean; panel?: boolean; chat: boolean; onSide: () => void; onPanel?: () => void; onChat: () => void };
  onSettings?: () => void;
}) {
  const [open, setOpen] = useState<Command["menu"] | null>(null);
  const bar = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const down = (e: MouseEvent) => { if (!bar.current?.contains(e.target as Node)) setOpen(null); };
    const key = (e: KeyboardEvent) => { if (e.key === "Escape") setOpen(null); };
    addEventListener("mousedown", down);
    addEventListener("keydown", key);
    return () => { removeEventListener("mousedown", down); removeEventListener("keydown", key); };
  }, [open]);
  const local = isLocal();
  const menus = MENUS.filter((m) => commands.some((c) => c.menu === m));

  return (
    <header className="titlebar" ref={bar}>
      <img className="tb-logo" src="/mark.png" alt="KuliDev" />
      <nav className="tb-menus" role="menubar" aria-label="Menu">
        {menus.map((m) => (
          <div key={m} className="tb-menu">
            <button role="menuitem" aria-haspopup="menu" aria-expanded={open === m} className={open === m ? "on" : ""}
              onClick={() => setOpen((o) => (o === m ? null : m))} onMouseEnter={() => open && setOpen(m)}>
              {m}
            </button>
            {open === m && (
              <div className="tb-drop" role="menu" aria-label={m}>
                {commands.filter((c) => c.menu === m).map((c) => (
                  <div key={c.id}>
                    {c.sep && <div className="tb-sep" role="separator" />}
                    <button role="menuitem" disabled={c.disabled} onClick={() => { setOpen(null); c.run(); }}>
                      <span>{c.label}</span>
                      {c.keys && <kbd>{c.keys}</kbd>}
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}
      </nav>
      <button className="tb-center" onClick={onCenter} title="Go to file (Ctrl+P)">
        <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true">
          <circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.2-4.2" />
        </svg>
        <span>{center}</span>
      </button>
      <div className="tb-right">
        <span className={`tb-conn ${local ? "" : "remote"}`} title={local ? "This KuliDev runs on this computer" : `Connected to ${location.host}`}>
          <span className="tb-dot" aria-hidden="true" />
          {local ? "Local" : `Remote: ${location.hostname}`}
        </span>
        {toggles && (
          <>
            <button className={`tb-icon ${toggles.side ? "on" : ""}`} onClick={toggles.onSide} title="Toggle side bar (Ctrl+B)" aria-pressed={toggles.side} aria-label="Toggle side bar">
              <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.3"><rect x="1.5" y="2.5" width="13" height="11" rx="1" /><path d="M6 2.5v11" />{toggles.side && <path d="M1.5 3h4.5v10H1.5z" fill="currentColor" />}</svg>
            </button>
            {toggles.onPanel && (
              <button className={`tb-icon ${toggles.panel ? "on" : ""}`} onClick={toggles.onPanel} title="Toggle terminal panel (Ctrl+`)" aria-pressed={toggles.panel} aria-label="Toggle panel">
                <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.3"><rect x="1.5" y="2.5" width="13" height="11" rx="1" /><path d="M1.5 9.5h13" />{toggles.panel && <path d="M1.5 9.5h13v4H1.5z" fill="currentColor" />}</svg>
              </button>
            )}
            <button className={`tb-icon ${toggles.chat ? "on" : ""}`} onClick={toggles.onChat} title="Toggle Ask Kuli (Ctrl+L)" aria-pressed={toggles.chat} aria-label="Toggle Ask Kuli">
              <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.3"><rect x="1.5" y="2.5" width="13" height="11" rx="1" /><path d="M10 2.5v11" />{toggles.chat && <path d="M10 3h4.5v10H10z" fill="currentColor" />}</svg>
            </button>
          </>
        )}
        {onSettings && (
          <button className="tb-icon" onClick={onSettings} title="Settings (Ctrl+,)" aria-label="Settings">
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round"><circle cx="12" cy="12" r="3" /><path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1" /></svg>
          </button>
        )}
      </div>
    </header>
  );
}

// CommandPalette lists every enabled command, filtered as you type.
export function CommandPalette({ commands, onClose }: { commands: Command[]; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const list = useMemo(() => {
    const all = commands.filter((c) => !c.disabled).map((c) => ({ c, text: `${c.menu}: ${c.label}` }));
    if (!q.trim()) return all;
    return all.map((x) => ({ ...x, s: fuzzyScore(q.trim(), x.text) })).filter((x) => x.s >= 0).sort((a, b) => b.s - a.s);
  }, [q, commands]);
  useEffect(() => setSel(0), [q]);
  const run = (c: Command) => { onClose(); c.run(); };
  function key(e: React.KeyboardEvent) {
    if (e.key === "Escape") onClose();
    else if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, list.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
    else if (e.key === "Enter" && list[sel]) run(list[sel].c);
  }
  return (
    <div className="qo-backdrop" onMouseDown={onClose}>
      <div className="qo" role="dialog" aria-label="Command palette" onMouseDown={(e) => e.stopPropagation()}>
        <input autoFocus value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={key} placeholder="Type a command" aria-label="Command" />
        <ul role="listbox">
          {list.map(({ c, text }, i) => (
            <li key={c.id} role="option" aria-selected={i === sel} className={i === sel ? "is-sel" : ""} onMouseEnter={() => setSel(i)} onClick={() => run(c)}>
              <span className="qo-cmd">{text}</span>
              {c.keys && <kbd>{c.keys}</kbd>}
            </li>
          ))}
          {list.length === 0 && <li className="muted">No matching command</li>}
        </ul>
      </div>
    </div>
  );
}
