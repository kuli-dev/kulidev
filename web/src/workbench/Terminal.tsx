import { FitAddon } from "@xterm/addon-fit";
import { Terminal as XTerm } from "@xterm/xterm";
import "@xterm/xterm/css/xterm.css";
import { useEffect, useRef, useState } from "react";
import { api, type TermInfo } from "../api";
import { Icon } from "./Icons";

const css = (v: string) => getComputedStyle(document.documentElement).getPropertyValue(v).trim();

// The bottom panel: persistent shells. Closing the panel or the browser leaves them
// running on the server; reopening replays their recent output.
export function TerminalPanel({ height, onResize, onClose }: { height: number; onResize: (h: number) => void; onClose: () => void }) {
  const [list, setList] = useState<TermInfo[]>([]);
  const [active, setActive] = useState<string>("");
  const [error, setError] = useState("");

  async function refresh(select?: string) {
    try {
      const r = await api.terminals();
      setList(r.terminals);
      setActive((cur) => select ?? (r.terminals.some((t) => t.id === cur) ? cur : r.terminals.at(-1)?.id ?? ""));
      return r.terminals;
    } catch (e) {
      setError((e as Error).message);
      return [];
    }
  }

  async function create() {
    setError("");
    try {
      const t = await api.startTerminal(100, 30);
      await refresh(t.id);
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function kill(id: string) {
    await api.killTerminal(id).catch(() => {});
    refresh();
  }

  useEffect(() => {
    refresh().then((ts) => {
      if (ts.length === 0) create();
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function drag(e: React.PointerEvent) {
    const startY = e.clientY;
    const startH = height;
    const move = (m: PointerEvent) => onResize(Math.max(120, Math.min(innerHeight - 160, startH + startY - m.clientY)));
    const up = () => {
      removeEventListener("pointermove", move);
      removeEventListener("pointerup", up);
    };
    addEventListener("pointermove", move);
    addEventListener("pointerup", up);
  }

  return (
    <section className="panel-bottom" style={{ height }} aria-label="Terminal">
      <div className="panel-grip" onPointerDown={drag} role="separator" aria-orientation="horizontal" aria-label="Resize terminal panel" />
      <header className="panel-bar">
        <div className="term-tabs" role="tablist">
          {list.map((t) => (
            <div key={t.id} role="tab" aria-selected={t.id === active} className={`term-tab ${t.id === active ? "on" : ""}`} onClick={() => setActive(t.id)}>
              {t.title}{t.exited ? ` (exited ${t.exitCode})` : ""}
              <button className="tab-x" aria-label={`Kill ${t.title}`} onClick={(e) => { e.stopPropagation(); kill(t.id); }}><Icon.close /></button>
            </div>
          ))}
        </div>
        <button className="icon-btn" onClick={create} title="New terminal" aria-label="New terminal">+</button>
        <button className="icon-btn" onClick={onClose} title="Hide panel (Ctrl+`)" aria-label="Hide terminal panel"><Icon.close /></button>
      </header>
      {error && <p className="notice warn pad">{error}</p>}
      <div className="term-body">{active && <TermView key={active} id={active} onExit={() => refresh()} />}</div>
    </section>
  );
}

function TermView({ id, onExit }: { id: string; onExit: () => void }) {
  const host = useRef<HTMLDivElement>(null);
  const [state, setState] = useState<"connecting" | "open" | "closed">("connecting");
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const term = new XTerm({
      fontFamily: "'Martian Mono', ui-monospace, monospace", fontSize: 12.5, cursorBlink: true, scrollback: 5000,
      theme: { background: css("--concrete"), foreground: css("--ink"), cursor: css("--safety"), selectionBackground: "#34505a" },
    });
    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(host.current!);
    fit.fit();

    const url = new URL(`api/terminals/${id}/ws`, location.href);
    url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(url);
    ws.binaryType = "arraybuffer";
    const send = (m: object) => ws.readyState === WebSocket.OPEN && ws.send(JSON.stringify(m));
    ws.onopen = () => {
      setState("open");
      send({ type: "resize", cols: term.cols, rows: term.rows });
      term.focus();
    };
    ws.onmessage = (e) => term.write(new Uint8Array(e.data as ArrayBuffer));
    ws.onclose = (e) => {
      setState("closed");
      if (e.reason === "session ended") onExit();
    };
    const input = term.onData((data) => send({ type: "input", data }));
    const ro = new ResizeObserver(() => {
      try {
        fit.fit();
        send({ type: "resize", cols: term.cols, rows: term.rows });
      } catch { /* hidden */ }
    });
    ro.observe(host.current!);
    return () => {
      ro.disconnect();
      input.dispose();
      ws.close();
      term.dispose();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, attempt]);

  return (
    <>
      <div className="term-host" ref={host} />
      {state === "closed" && (
        <div className="term-overlay">
          <span>Disconnected. The shell may still be running on the server.</span>
          <button className="btn" onClick={() => { setState("connecting"); setAttempt((a) => a + 1); }}>Reconnect</button>
        </div>
      )}
    </>
  );
}
