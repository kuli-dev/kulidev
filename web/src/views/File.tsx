import { useEffect, useRef } from "react";
import { api } from "../api";
import { useLoad } from "../hooks";
import { href } from "../router";
import { ErrorNote } from "./Overview";

export function FileViewPage({ path, from, to }: { path: string; from?: number; to?: number }) {
  const f = useLoad(() => api.file(path), [path]);
  const codeRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!f.data || !from) return;
    codeRef.current?.querySelector(`[data-line="${from}"]`)?.scrollIntoView({ block: "center" });
  }, [f.data, from, to]);

  if (f.error) return <article className="page"><h1 className="title path-title">{path}</h1><ErrorNote msg={f.error} /></article>;
  if (!f.data) return <p className="muted page">Loading…</p>;
  const d = f.data;
  const cited = (n: number) => from !== undefined && n >= from && n <= (to ?? from);
  const parent = path.split("/").slice(0, -1).join("/");

  return (
    <div className="file-page">
      <header className="file-head">
        <h1 className="title path-title">
          {parent && <span className="muted">{parent}/</span>}
          {path.split("/").pop()}
        </h1>
        {d.doc && <p className="doc">{d.doc}</p>}
        {from && (
          <p className="cited-note">
            Showing the cited lines {from}{to && to !== from ? `–${to}` : ""}.{" "}
            <a href={href.file(path)}>Clear</a>
          </p>
        )}
      </header>
      <div className="file-body">
        <div className="code" ref={codeRef} role="region" aria-label={`Source of ${path}`} tabIndex={0}>
          {d.tooLarge && <p className="notice">This file is over 2 MB and is not shown.</p>}
          {d.lines.map((text, i) => {
            const n = i + 1;
            return (
              <div key={n} data-line={n} className={cited(n) ? "ln cited" : "ln"}>
                <a className="no" href={href.file(path, n)} aria-label={`Line ${n}`}>{n}</a>
                <span className="tx">{text || " "}</span>
              </div>
            );
          })}
        </div>
        <aside className="panel file-side">
          {d.symbols.length > 0 && (
            <>
              <h3>Outline</h3>
              <ul className="plain outline">
                {d.symbols.map((s, i) => (
                  <li key={i}>
                    <a href={href.file(path, s.Line, s.EndLine)}>{s.Name}</a>{" "}
                    <span className="muted small">{s.Kind}</span>
                  </li>
                ))}
              </ul>
            </>
          )}
          <h3>Imported by {d.importers.length > 0 && <span className="muted">({d.importers.length})</span>}</h3>
          {d.importers.length === 0 ? (
            <p className="muted small">No other scanned file imports this one.</p>
          ) : (
            <ul className="plain">
              {d.importers.slice(0, 50).map((h, i) => (
                <li key={i}><a href={href.file(h.Path, h.Line)}>{h.Path}</a></li>
              ))}
            </ul>
          )}
          {d.imports.length > 0 && (
            <>
              <h3>Imports</h3>
              <ul className="plain">
                {d.imports.map((h, i) => (
                  <li key={i}>
                    {h.Kind === "FILE" ? <a href={href.file(h.Name)}>{h.Name}</a> : <span>{h.Name.replace(/^(npm|py|go):/, "")}</span>}
                    {h.Name.startsWith("unresolved:") && <span className="warn-text small"> missing</span>}
                  </li>
                ))}
              </ul>
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
