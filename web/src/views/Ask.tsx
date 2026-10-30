import { useState } from "react";
import { api, type AskResult } from "../api";
import { Markdown } from "../markdown";
import { ErrorNote } from "./Overview";

type Turn = { q: string; a?: AskResult; error?: string };

export function AskView({ enabled }: { enabled: boolean }) {
  const [q, setQ] = useState("");
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const question = q.trim();
    if (!question || busy) return;
    setBusy(true);
    setTurns((t) => [{ q: question }, ...t]);
    setQ("");
    try {
      const a = await api.ask(question);
      setTurns((t) => [{ q: question, a }, ...t.slice(1)]);
    } catch (err) {
      setTurns((t) => [{ q: question, error: (err as Error).message }, ...t.slice(1)]);
    } finally {
      setBusy(false);
    }
  }

  return (
    <article className="page ask">
      <h1 className="title">Ask</h1>
      {!enabled && (
        <p className="notice">
          Asking needs a model. Restart with <code>KULI_LLM_BASE_URL</code> and <code>KULI_LLM_MODEL</code> set (any
          OpenAI-compatible endpoint), then reload this page.
        </p>
      )}
      <form onSubmit={submit} className="ask-form">
        <textarea
          value={q}
          onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && !e.shiftKey && submit(e)}
          placeholder="How does a document get from upload to OCR?"
          rows={2}
          disabled={!enabled}
          aria-label="Your question about the code"
        />
        <button className="btn primary" disabled={!enabled || busy || !q.trim()}>{busy ? "Reading the code…" : "Ask"}</button>
      </form>
      <p className="muted small">Answers only use what KuliDev found in this repository, and cite the lines they rely on.</p>
      {turns.map((t, i) => (
        <section key={turns.length - i} className="turn">
          <h2 className="question">{t.q}</h2>
          {!t.a && !t.error && <p className="muted">Searching the code and writing an answer. This can take a minute.</p>}
          {t.error && <ErrorNote msg={t.error} />}
          {t.a && (
            <>
              <Markdown source={t.a.answer} />
              <p className="muted small grounding">
                {t.a.citations} citations; {Math.round(t.a.coverage * 100)}% of statements cite the code
                {t.a.removed > 0 ? `; ${t.a.removed} unsupported references removed` : ""}.
              </p>
            </>
          )}
        </section>
      ))}
    </article>
  );
}
