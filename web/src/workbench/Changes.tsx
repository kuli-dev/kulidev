import { useEffect, useState } from "react";
import { api, type GitChange, type GitStatus } from "../api";

const LABEL: Record<string, string> = { M: "modified", A: "added", D: "deleted", R: "renamed", C: "copied", U: "conflict", "?": "untracked", T: "type changed" };

// Source control, VS Code style: staged and unstaged changes, a commit box, and a diff
// for every row. Writes go through the user's own git.
export function ChangesPane({ status, writable, onChanged, onOpenDiff }: {
  status: GitStatus | null; writable: boolean; onChanged: () => void; onOpenDiff: (path: string, staged: boolean) => void;
}) {
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ text: string; warn?: boolean } | null>(null);

  if (!status) return <div className="pane"><h2 className="pane-title">Changes</h2><p className="muted small pad-x">Loading…</p></div>;
  if (!status.git) {
    return (
      <div className="pane">
        <h2 className="pane-title">Changes</h2>
        <p className="muted small pad-x">This folder is not a git repository. Run <code>git init</code> in the terminal to start tracking changes.</p>
      </div>
    );
  }
  const staged = status.changes.filter((c) => c.index !== "." && c.index !== "?");
  const unstaged = status.changes.filter((c) => c.worktree !== ".");

  async function run(op: Parameters<typeof api.git>[0], body: Parameters<typeof api.git>[1]) {
    setBusy(true);
    setNote(null);
    try {
      const r = await api.git(op, body);
      if (op === "commit") {
        setMsg("");
        setNote({ text: `Committed ${r.commit}.` });
      } else if (op === "fetch" || op === "pull" || op === "push") {
        setNote({ text: op === "fetch" ? "Fetched." : op === "pull" ? "Pulled (fast-forward)." : "Pushed." });
      } else if (op === "switch") {
        setNote({ text: `On ${body.branch} now.` });
      }
      onChanged();
    } catch (e) {
      setNote({ text: (e as Error).message, warn: true });
    } finally {
      setBusy(false);
    }
  }

  function discard(c: GitChange) {
    const untracked = c.worktree === "?";
    const q = untracked
      ? `Delete ${c.path}? It is untracked, so this cannot be undone.`
      : `Discard your changes to ${c.path}? This cannot be undone.`;
    if (confirm(q)) run("discard", { paths: [c.path], untracked });
  }

  function commit(e?: React.FormEvent) {
    e?.preventDefault();
    if (msg.trim() && staged.length && !busy) run("commit", { message: msg });
  }

  return (
    <div className="pane changes">
      <h2 className="pane-title">Changes</h2>
      <BranchBar status={status} writable={writable} busy={busy} run={run} />
      {writable && (
        <form className="commit-box" onSubmit={commit}>
          <textarea value={msg} onChange={(e) => setMsg(e.target.value)} rows={3} placeholder="Commit message (Ctrl+Enter to commit)"
            onKeyDown={(e) => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) commit(e); }} aria-label="Commit message" />
          <button className="btn primary" disabled={busy || !msg.trim() || staged.length === 0}
            title={staged.length === 0 ? "Stage changes first" : undefined}>
            Commit {staged.length > 0 ? `${staged.length} staged` : ""}
          </button>
        </form>
      )}
      {note && <p className={`notice pad ${note.warn ? "warn" : ""}`} role="status">{note.text}</p>}

      <Section title="Staged" items={staged} staged writable={writable} busy={busy} onOpenDiff={onOpenDiff}
        all={writable && staged.length > 0 ? { label: "Unstage all", run: () => run("unstage", { paths: staged.map((c) => c.path) }) } : undefined}
        actions={(c) => [{ label: "Unstage", icon: "−", run: () => run("unstage", { paths: [c.path] }) }]} />
      <Section title="Changes" items={unstaged} staged={false} writable={writable} busy={busy} onOpenDiff={onOpenDiff}
        all={writable && unstaged.length > 0 ? { label: "Stage all", run: () => run("stage", { paths: unstaged.map((c) => c.path) }) } : undefined}
        actions={(c) => [
          { label: "Discard", icon: "↶", run: () => discard(c) },
          { label: "Stage", icon: "+", run: () => run("stage", { paths: [c.path] }) },
        ]} />
      {status.changes.length === 0 && <p className="muted small pad-x">No changes. The working tree matches the last commit.</p>}
    </div>
  );
}

type Action = { label: string; icon: string; run: () => void };

function Section({ title, items, staged, writable, busy, all, actions, onOpenDiff }: {
  title: string; items: GitChange[]; staged: boolean; writable: boolean; busy: boolean;
  all?: { label: string; run: () => void }; actions: (c: GitChange) => Action[]; onOpenDiff: (path: string, staged: boolean) => void;
}) {
  if (items.length === 0) return null;
  return (
    <section className="git-section">
      <div className="pane-sub git-head">
        <span>{title} ({items.length})</span>
        {all && <button className="link-btn" disabled={busy} onClick={all.run}>{all.label}</button>}
      </div>
      <ul className="plain">
        {items.map((c) => {
          const letter = staged ? c.index : c.worktree === "?" ? "U" : c.worktree;
          const slash = c.path.lastIndexOf("/");
          return (
            <li key={c.path} className="git-row">
              <button className="link-row" onClick={() => onOpenDiff(c.path, staged)} title={`${c.path} — ${LABEL[staged ? c.index : c.worktree] ?? ""}`}>
                <span className="git-name">{c.path.slice(slash + 1)}</span>
                <span className="git-dir">{slash > 0 ? c.path.slice(0, slash) : ""}</span>
              </button>
              {writable && (
                <span className="git-actions">
                  {actions(c).map((a) => (
                    <button key={a.label} className="icon-btn" disabled={busy} onClick={a.run} title={a.label} aria-label={`${a.label} ${c.path}`}>{a.icon}</button>
                  ))}
                </span>
              )}
              <span className={`git-letter g-${letter}`} title={LABEL[letter === "U" && !c.conflict ? "?" : letter]}>{letter}</span>
            </li>
          );
        })}
      </ul>
    </section>
  );
}

function BranchBar({ status, writable, busy, run }: {
  status: GitStatus; writable: boolean; busy: boolean;
  run: (op: Parameters<typeof api.git>[0], body: Parameters<typeof api.git>[1]) => Promise<void>;
}) {
  const [branches, setBranches] = useState<string[]>([]);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  useEffect(() => {
    api.gitBranches().then((r) => setBranches(r.branches.map((b) => b.name)), () => {});
  }, [status.branch]);

  const sync = status.upstream
    ? `${status.ahead ? `${status.ahead} to push` : ""}${status.ahead && status.behind ? ", " : ""}${status.behind ? `${status.behind} to pull` : ""}` || "up to date"
    : "not pushed yet";
  return (
    <div className="branch-bar pad-x">
      {writable && !creating ? (
        <select value={status.branch} disabled={busy} aria-label="Branch"
          onChange={(e) => {
            if (e.target.value === "__new") setCreating(true);
            else run("switch", { branch: e.target.value });
          }}>
          {branches.map((b) => <option key={b} value={b}>{b}</option>)}
          <option value="__new">New branch…</option>
        </select>
      ) : writable ? (
        <form className="new-branch" onSubmit={(e) => { e.preventDefault(); if (name.trim()) run("switch", { branch: name.trim(), create: true }).then(() => { setCreating(false); setName(""); }); }}>
          <input autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="feature/name" aria-label="New branch name" />
          <button className="btn" disabled={busy || !name.trim()}>Create</button>
          <button type="button" className="link-btn" onClick={() => setCreating(false)}>Cancel</button>
        </form>
      ) : (
        <strong>{status.branch}</strong>
      )}
      <span className="small muted">{sync}</span>
      {writable && (
        <span className="sync-btns">
          <button className="link-btn" disabled={busy} onClick={() => run("fetch", {})} title="Fetch from the remote">Fetch</button>
          {status.behind > 0 && <button className="link-btn" disabled={busy} onClick={() => run("pull", {})} title="Fast-forward only: never makes a merge commit">Pull</button>}
          {(status.ahead > 0 || !status.upstream) && <button className="link-btn" disabled={busy} onClick={() => run("push", {})} title="Push this branch (never forces)">Push</button>}
        </span>
      )}
    </div>
  );
}
