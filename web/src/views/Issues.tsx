import { api } from "../api";
import { useLoad } from "../hooks";
import { href } from "../router";
import { ErrorNote } from "./Overview";

export function IssuesView() {
  const b = useLoad(api.broken, []);
  const groups = new Map<string, { line: number; target: string }[]>();
  for (const h of b.data?.hits ?? []) {
    const list = groups.get(h.Path) ?? [];
    list.push({ line: h.Line, target: h.Name });
    groups.set(h.Path, list);
  }
  return (
    <article className="page">
      <h1 className="title">Issues</h1>
      <h2>Broken imports</h2>
      <p className="muted small">Imports that point at a file that does not exist in the repository.</p>
      {b.error && <ErrorNote msg={b.error} />}
      {b.data && groups.size === 0 && <p>None. Every relative import resolves to a real file.</p>}
      <ul className="plain issues">
        {[...groups].map(([file, items]) => (
          <li key={file}>
            <a href={href.file(file)}>{file}</a>
            <ul className="plain">
              {items.map((it, i) => (
                <li key={i}>
                  <a href={href.file(file, it.line)}>line {it.line}</a>{" "}
                  <span className="muted">imports</span> <code>{it.target}</code>
                </li>
              ))}
            </ul>
          </li>
        ))}
      </ul>
    </article>
  );
}
