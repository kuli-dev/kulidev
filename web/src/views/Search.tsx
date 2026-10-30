import { useEffect, useState } from "react";
import { api, type Hit } from "../api";
import { href } from "../router";
import { ErrorNote } from "./Overview";

export function SearchView({ initial }: { initial: string }) {
  const [q, setQ] = useState(initial);
  const [hits, setHits] = useState<Hit[] | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const text = q.trim();
    history.replaceState(null, "", text ? href.search(text) : "#/search");
    if (!text) {
      setHits(null);
      return;
    }
    let live = true;
    const t = setTimeout(() => {
      api.search(text).then(
        (r) => live && (setHits(r.hits), setError("")),
        (e: Error) => live && setError(e.message),
      );
    }, 180);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [q]);

  return (
    <article className="page">
      <h1 className="title">Search</h1>
      <input
        className="search-input"
        autoFocus
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Function, class, file or words from a doc comment"
        aria-label="Search the codebase"
      />
      {error && <ErrorNote msg={error} />}
      {hits && hits.length === 0 && <p className="muted">Nothing matches “{q}”. Try fewer or different words.</p>}
      {hits && hits.length > 0 && (
        <ul className="results">
          {hits.map((h, i) => (
            <li key={i}>
              <span className={`kind kind-${h.Kind.toLowerCase()}`}>{h.Kind.toLowerCase()}</span>
              {h.Path ? (
                <a href={href.file(h.Path, h.Line || undefined, h.EndLine || undefined)} className="hit-name">{h.Name}</a>
              ) : (
                <span className="hit-name">{h.Name}</span>
              )}
              {h.Path && <span className="muted small path">{h.Path}{h.Line ? `:${h.Line}` : ""}</span>}
              {h.Detail && <p className="doc">{h.Detail}</p>}
            </li>
          ))}
        </ul>
      )}
    </article>
  );
}
