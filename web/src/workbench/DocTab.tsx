import { api } from "../api";
import { useLoad } from "../hooks";
import { Markdown } from "../markdown";
import { docScope } from "./files";

export function DocTab({ name }: { name: string }) {
  const list = useLoad(() => api.docs(docScope(name)), [name]);
  const doc = useLoad(() => api.doc(name), [name]);
  const meta = list.data?.docs?.find((d) => d.file === name);
  return (
    <div className="doc-tab">
      {meta?.status === "STALE" && (
        <p className="notice warn">
          Code this page cites has changed since it was written ({meta.changed?.slice(0, 3).join(", ")}
          {(meta.changed?.length ?? 0) > 3 ? ", …" : ""}). Refresh it from the Docs panel (Refresh stale).
        </p>
      )}
      {meta && meta.mode !== "deterministic" && meta.mode !== "facts-only" && (
        <p className="muted small">{meta.citations} citations; {Math.round(meta.citation_coverage * 100)}% of statements cite the code.</p>
      )}
      {doc.error && <p className="notice warn">{doc.error}</p>}
      {doc.data && <Markdown source={doc.data.markdown} />}
    </div>
  );
}
