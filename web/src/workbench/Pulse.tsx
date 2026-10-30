import { useEffect, useState } from "react";
import { api, type GitStatus, type TermInfo } from "../api";
import { ago, disableNotifications, enableNotifications, notificationsWanted, stateOf } from "./attention";

const STATE_LABEL = { working: "Working", waiting: "Waiting for you", exited: "Finished" };

// Pulse: what your agents are doing and what they changed — built to be checked from a
// phone. Open an agent to answer it; review the changes before committing.
export function PulsePane({ terminals, git, onOpenTerminal, onReview }: {
  terminals: TermInfo[] | null; git: GitStatus | null;
  onOpenTerminal: (id: string) => void; onReview: () => void;
}) {
  const [now, setNow] = useState(Date.now());
  const [notifyOn, setNotifyOn] = useState(notificationsWanted);
  const [notifyMsg, setNotifyMsg] = useState("");
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const agents = (terminals ?? []).filter((t) => t.kind === "agent");
  const shells = (terminals ?? []).filter((t) => t.kind !== "agent" && !t.exited);
  const changes = git?.git ? git.changes.length : 0;

  async function toggleNotify() {
    if (notifyOn) {
      disableNotifications();
      setNotifyOn(false);
      return;
    }
    const err = await enableNotifications();
    setNotifyMsg(err);
    setNotifyOn(!err);
  }

  return (
    <div className="pane pulse">
      <h2 className="pane-title">Pulse</h2>

      <section className="pulse-card">
        <div className="pulse-head">
          <strong>{changes === 0 ? "No pending changes" : `${changes} changed file${changes === 1 ? "" : "s"}`}</strong>
          {git?.git && <span className="small muted">on {git.branch}</span>}
        </div>
        {changes > 0 && <button className="btn primary" onClick={onReview}>Review changes</button>}
      </section>

      <h3 className="pane-sub">Agents</h3>
      {agents.length === 0 && (
        <p className="muted small pad-x">No agents running. Start one from the Agents view; you can follow it from here, even on your phone.</p>
      )}
      {agents.map((t) => {
        const st = stateOf(t, now);
        return (
          <section key={t.id} className={`pulse-card agent-${st}`}>
            <div className="pulse-head">
              <strong>{t.title}</strong>
              <span className={`chip chip-${st}`}>{STATE_LABEL[st]}{st === "exited" ? ` (exit ${t.exitCode})` : ""}</span>
            </div>
            <p className="small muted">
              {st === "exited" ? `ran ${ago(t.started, now)} ago` : `started ${ago(t.started, now)} ago, last output ${ago(t.lastOutput, now)} ago`}
            </p>
            {t.tail && t.tail.length > 0 && <pre className="tail">{t.tail.join("\n")}</pre>}
            {st !== "exited" && <QuickReply id={t.id} />}
            <button className="btn" onClick={() => onOpenTerminal(t.id)}>Open terminal</button>
          </section>
        );
      })}

      {shells.length > 0 && (
        <p className="small muted pad-x">{shells.length} terminal{shells.length === 1 ? "" : "s"} open.</p>
      )}

      <div className="pad-x notify">
        <label className="small">
          <input type="checkbox" checked={notifyOn} onChange={toggleNotify} /> Notify me when an agent is waiting or finishes
        </label>
        {notifyMsg && <p className="notice warn">{notifyMsg}</p>}
      </div>
    </div>
  );
}

// Keys a coding agent's prompts and menus need, sent straight to its terminal.
const KEYS: { label: string; seq: string; title: string }[] = [
  { label: "↑", seq: "\x1b[A", title: "Up" },
  { label: "↓", seq: "\x1b[B", title: "Down" },
  { label: "Enter", seq: "\r", title: "Enter" },
  { label: "Esc", seq: "\x1b", title: "Escape" },
  { label: "Ctrl+C", seq: "\x03", title: "Interrupt" },
];

function QuickReply({ id }: { id: string }) {
  const [text, setText] = useState("");
  const [err, setErr] = useState("");
  const send = async (data: string) => {
    setErr("");
    try {
      await api.termInput(id, data);
    } catch (e) {
      setErr((e as Error).message);
    }
  };
  return (
    <div className="quick-reply">
      <form onSubmit={(e) => { e.preventDefault(); if (text) { send(text + "\r"); setText(""); } }}>
        <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Reply" aria-label="Reply to the agent" />
        <button className="btn" disabled={!text}>Send</button>
      </form>
      <div className="keys-row">
        {KEYS.map((k) => (
          <button key={k.label} type="button" className="key-btn" title={k.title} aria-label={`Send ${k.title}`} onClick={() => send(k.seq)}>{k.label}</button>
        ))}
      </div>
      {err && <p className="notice warn">{err}</p>}
    </div>
  );
}
