import { useEffect, useMemo, useRef, useState } from "react";
import type { TreeFile } from "../api";
import { fuzzyScore } from "./files";

export function QuickOpen({ files, onPick, onClose }: { files: TreeFile[]; onPick: (p: string) => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const [sel, setSel] = useState(0);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    input.current?.focus();
  }, []);
  const results = useMemo(() => {
    if (!q.trim()) return files.filter((f) => f.l).slice(0, 30);
    return files
      .map((f) => ({ f, s: fuzzyScore(q.trim(), f.p) }))
      .filter((x) => x.s >= 0)
      .sort((a, b) => b.s - a.s)
      .slice(0, 40)
      .map((x) => x.f);
  }, [q, files]);
  useEffect(() => {
    setSel(0);
  }, [q]);

  function key(e: React.KeyboardEvent) {
    if (e.key === "Escape") onClose();
    else if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, results.length - 1)); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
    else if (e.key === "Enter" && results[sel]) onPick(results[sel].p);
  }

  return (
    <div className="qo-backdrop" onMouseDown={onClose}>
      <div className="qo" role="dialog" aria-label="Go to file" onMouseDown={(e) => e.stopPropagation()}>
        <input ref={input} value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={key}
          placeholder="Go to file by name" aria-label="File name" />
        <ul role="listbox">
          {results.map((f, i) => {
            const slash = f.p.lastIndexOf("/");
            return (
              <li key={f.p} role="option" aria-selected={i === sel} className={i === sel ? "is-sel" : ""}
                onMouseEnter={() => setSel(i)} onClick={() => onPick(f.p)}>
                <span>{f.p.slice(slash + 1)}</span>
                <span className="muted small">{slash > 0 ? f.p.slice(0, slash) : ""}</span>
              </li>
            );
          })}
          {results.length === 0 && <li className="muted">No file matches.</li>}
        </ul>
      </div>
    </div>
  );
}
