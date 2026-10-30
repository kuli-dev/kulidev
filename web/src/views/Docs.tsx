import { api } from "../api";
import { useLoad } from "../hooks";
import { Markdown } from "../markdown";
import { href } from "../router";
import { ErrorNote } from "./Overview";

export function DocsView({ name }: { name?: string }) {
  const list = useLoad(api.docs, []);
  const docs = list.data?.docs ?? [];
  const current = name ?? docs[0]?.file;
  const doc = useLoad(() => (current ? api.doc(current) : Promise.resolve(null)), [current]);
  const meta = docs.find((d) => d.file === current);

  if (list.error) return <article className="page"><ErrorNote msg={list.error} /></article>;
  if (list.data && docs.length === 0) {
    return (
      <article className="page">
        <h1 className="title">Docs</h1>
        <p>No handover docs yet. Generate them from the repository with:</p>
        <pre className="code-block"><code>kuli docs</code></pre>
        <p className="muted small">Set KULI_LLM_BASE_URL and KULI_LLM_MODEL for written docs, or add --no-llm for facts only.</p>
      </article>
    );
  }
  return (
    <div className="docs-page">
      <nav className="docs-nav" aria-label="Documents">
        <h1 className="title">Docs</h1>
        <ul className="plain">
          {docs.map((d) => (
            <li key={d.file}>
              <a href={href.doc(d.file)} aria-current={d.file === current ? "page" : undefined}>{d.title}</a>
              {d.status === "STALE" && <span className="badge stale">stale</span>}
            </li>
          ))}
        </ul>
      </nav>
      <article className="doc-body">
        {meta && (
          <p className="doc-meta muted small">
            {meta.mode === "deterministic"
              ? "Built directly from the code graph."
              : meta.mode === "facts-only"
                ? "Facts only: no model was configured when this was generated."
                : `${meta.citations} citations; ${Math.round(meta.citation_coverage * 100)}% of statements cite the code.`}
          </p>
        )}
        {meta?.status === "STALE" && (
          <p className="notice warn">
            Code this page cites has changed since it was written: {meta.changed?.slice(0, 4).join(", ")}
            {(meta.changed?.length ?? 0) > 4 ? " and more" : ""}. Regenerate with <code>kuli docs --only {meta.id}</code>.
          </p>
        )}
        {doc.error && <ErrorNote msg={doc.error} />}
        {doc.data && <Markdown source={doc.data.markdown} />}
      </article>
    </div>
  );
}
