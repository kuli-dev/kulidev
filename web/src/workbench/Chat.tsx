import { useEffect, useRef, useState } from "react";
import { api, type AskResult } from "../api";
import { Markdown } from "../markdown";
import { Icon } from "./Icons";

type Turn = { q: string; a?: AskResult; error?: string };

// The "Kuli" panel: questions about the code, answered with cited lines that open
// in the editor.
export function Chat({ enabled, model, onClose }: { enabled: boolean; model?: string; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const end = useRef<HTMLDivElement>(null);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [turns]);

  async function send(e?: React.FormEvent) {
    e?.preventDefault();
    const question = q.trim();
    if (!question || busy || !enabled) return;
    setBusy(true);
    setQ("");
    setTurns((t) => [...t, { q: question }]);
    try {
      const a = await api.ask(question);
      setTurns((t) => [...t.slice(0, -1), { q: question, a }]);
    } catch (err) {
      setTurns((t) => [...t.slice(0, -1), { q: question, error: (err as Error).message }]);
    } finally {
      setBusy(false);
    }
  }

  return (
    <aside className="chat" aria-label="Ask Kuli">
      <header className="chat-head">
        <h2 className="pane-title">Ask Kuli</h2>
        <button className="icon-btn" onClick={onClose} aria-label="Close chat"><Icon.close /></button>
      </header>
      <div className="chat-log">
        {turns.length === 0 && (
          <p className="muted small pad-x">
            {enabled
              ? "Ask how something works. Answers use only this repository and cite the lines they rely on; click a citation to open it."
              : "Asking needs a model: restart kuli serve with KULI_LLM_BASE_URL and KULI_LLM_MODEL set."}
          </p>
        )}
        {turns.map((t, i) => (
          <div key={i} className="turn-w">
            <p className="me">{t.q}</p>
            {!t.a && !t.error && <p className="muted small">Reading the code…</p>}
            {t.error && <p className="notice warn">{t.error}</p>}
            {t.a && (
              <>
                <Markdown source={t.a.answer} />
                <p className="muted small">
                  {t.a.citations} citations, {Math.round(t.a.coverage * 100)}% of statements cited
                  {t.a.removed > 0 ? `, ${t.a.removed} unsupported removed` : ""}
                </p>
              </>
            )}
          </div>
        ))}
        <div ref={end} />
      </div>
      <form className="chat-form" onSubmit={send}>
        <textarea
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => { if (e.key === "Enter" && !e.shiftKey) send(e); }}
          placeholder={enabled ? "Ask about this codebase" : "No model configured"}
          disabled={!enabled}
          rows={3}
          aria-label="Question"
        />
        <div className="chat-foot">
          <span className="muted small">{model ? `Model: ${model}` : ""}</span>
          <button className="btn primary" disabled={!enabled || busy || !q.trim()}>{busy ? "Working…" : "Ask"}</button>
        </div>
      </form>
    </aside>
  );
}
