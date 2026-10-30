import { useEffect, useMemo, useState } from "react";
import { api, type GitStatus } from "../api";

type FileDiff = { path: string; lines: string[] };

function splitPatch(patch: string): FileDiff[] {
  const out: FileDiff[] = [];
  for (const chunk of patch.split(/^diff --git /m).slice(1)) {
    const lines = chunk.split("\n");
    const m = lines[0].match(/ b\/(.+)$/);
    const body = lines.slice(1).filter((l) => !/^(index |new file mode|deleted file mode|old mode|new mode|similarity|rename |--- |\+\+\+ )/.test(l));
    out.push({ path: m ? m[1] : lines[0], lines: body });
  }
  return out;
}

// Review: every pending change in one scroll, then commit or discard them all.
// Plain text diffs on purpose: fast and readable on a phone.
export function ReviewTab({ git, writable, onChanged }: { git: GitStatus | null; writable: boolean; onChanged: () => void }) {
  const [patch, setPatch] = useState<{ patch: string; truncated: boolean } | null>(null);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ text: string; warn?: boolean } | null>(null);
  const key = JSON.stringify(git?.changes ?? []);

  useEffect(() => {
    api.gitPatch().then(setPatch, (e: Error) => setNote({ text: e.message, warn: true }));
  }, [key]);
  const files = useMemo(() => (patch ? splitPatch(patch.patch) : []), [patch]);
  const paths = git?.changes.map((c) => c.path) ?? [];
  const untracked = git?.changes.filter((c) => c.worktree === "?").map((c) => c.path) ?? [];

  async function commitAll() {
    if (!msg.trim()) return;
    setBusy(true);
    setNote(null);
    try {
      await api.git("stage", { paths });
      const r = await api.git("commit", { message: msg });
      setMsg("");
      setNote({ text: `Committed ${r.commit}.` });
      onChanged();
    } catch (e) {
      setNote({ text: (e as Error).message, warn: true });
    } finally {
      setBusy(false);
    }
  }

  async function discardAll() {
    const extra = untracked.length ? ` This also deletes ${untracked.length} new file${untracked.length === 1 ? "" : "s"}.` : "";
    if (!confirm(`Discard all ${paths.length} changes? This cannot be undone.${extra}`)) return;
    setBusy(true);
    setNote(null);
    try {
      const staged = git?.changes.filter((c) => c.index !== "." && c.index !== "?").map((c) => c.path) ?? [];
      if (staged.length) await api.git("unstage", { paths: staged });
      await api.git("discard", { paths, untracked: untracked.length > 0 });
      setNote({ text: "Discarded." });
      onChanged();
    } catch (e) {
      setNote({ text: (e as Error).message, warn: true });
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="review">
      <header className="review-head">
        <h1 className="title">Review</h1>
        <p className="muted small">{paths.length} changed file{paths.length === 1 ? "" : "s"}{git?.git ? ` on ${git.branch}` : ""}</p>
      </header>
      {note && <p className={`notice pad ${note.warn ? "warn" : ""}`} role="status">{note.text}</p>}
      {paths.length === 0 && <p className="muted pad">Nothing to review: the working tree matches the last commit.</p>}
      <div className="review-files">
        {files.map((f) => (
          <section key={f.path} className="review-file">
            <h2 className="review-path">{f.path}</h2>
            <pre className="udiff">
              {f.lines.map((l, i) => (
                <span key={i} className={l.startsWith("+") ? "u-add" : l.startsWith("-") ? "u-del" : l.startsWith("@@") ? "u-hunk" : ""}>{l || " "}{"\n"}</span>
              ))}
            </pre>
          </section>
        ))}
        {patch?.truncated && <p className="notice pad">The diff is too large to show in full here; open files from the Changes view.</p>}
      </div>
      {writable && paths.length > 0 && (
        <footer className="review-bar">
          <input value={msg} onChange={(e) => setMsg(e.target.value)} placeholder="Commit message" aria-label="Commit message"
            onKeyDown={(e) => { if (e.key === "Enter") commitAll(); }} />
          <button className="btn primary" disabled={busy || !msg.trim()} onClick={commitAll}>Commit all</button>
          <button className="btn" disabled={busy} onClick={discardAll}>Discard all</button>
        </footer>
      )}
    </div>
  );
}
