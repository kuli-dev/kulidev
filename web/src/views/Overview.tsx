import type { Count, Overview } from "../api";
import { fmt, type Loadable } from "../hooks";
import { href } from "../router";

const LANG_COLORS = ["var(--lang-1)", "var(--lang-2)", "var(--lang-3)", "var(--lang-4)", "var(--lang-5)", "var(--lang-6)"];

export function OverviewView({ ov, onRescan }: { ov: Loadable<Overview>; onRescan: () => void }) {
  if (ov.error) return <ErrorNote msg={ov.error} />;
  if (!ov.data) return <p className="muted">Loading…</p>;
  const o = ov.data;
  const defs = (o.symbols ?? []).reduce((a, c) => a + c.N, 0);
  const totalLines = (o.languages ?? []).reduce((a, c) => a + c.Lines, 0);
  const langs = (o.languages ?? []).filter((l) => l.Lines > 0);
  const code = langs.slice(0, 6);
  const rest = langs.slice(6).reduce((a, c) => a + c.Lines, 0);
  const maxIn = Math.max(1, ...(o.topInternal ?? []).map((c) => c.N));

  return (
    <article className="page overview">
      <header>
        <h1 className="display">{o.repo}</h1>
        <p className="lede">
          {fmt(o.parsed)} source files read, {fmt(defs)} definitions found and {fmt(o.internalEdges)} internal
          imports traced. Every fact below links back to the code.
        </p>
        {o.stale && (
          <p className="notice warn">
            The code has new commits since this scan, so some answers may be out of date.{" "}
            <button className="btn-link" onClick={onRescan}>Rescan now</button>
          </p>
        )}
      </header>

      <section aria-labelledby="mix">
        <h2 id="mix">Language mix</h2>
        <div className="stack-bar" role="img" aria-label="Lines of code by language">
          {code.map((l, i) => (
            <span key={l.Label} style={{ flexGrow: l.Lines, background: LANG_COLORS[i] }} title={`${l.Label}: ${fmt(l.Lines)} lines`} />
          ))}
          {rest > 0 && <span style={{ flexGrow: rest, background: "var(--line)" }} title={`Other: ${fmt(rest)} lines`} />}
        </div>
        <ul className="legend">
          {code.map((l, i) => (
            <li key={l.Label}>
              <i style={{ background: LANG_COLORS[i] }} />
              {l.Label} <span className="muted">{pct(l.Lines, totalLines)}</span>
            </li>
          ))}
        </ul>
      </section>

      <div className="two-col">
        <section aria-labelledby="start">
          <h2 id="start">Start reading here</h2>
          <p className="muted small">The files and packages the rest of the code depends on most.</p>
          <ol className="bars">
            {(o.topInternal ?? []).map((c) => (
              <li key={c.Label}>
                <a href={c.Label.includes(".") ? href.file(c.Label) : href.search(c.Label)}>{c.Label}</a>
                <span className="bar" style={{ width: `${(c.N / maxIn) * 100}%` }} />
                <span className="num">{fmt(c.N)} importers</span>
              </li>
            ))}
          </ol>
        </section>

        <section aria-labelledby="deps">
          <h2 id="deps">Built with</h2>
          <p className="muted small">Third-party modules by how many files import them.</p>
          <CountList items={o.topExternal} unit="files" />
        </section>
      </div>

      <div className="two-col">
        <section aria-labelledby="infra">
          <h2 id="infra">Build and infrastructure files</h2>
          <CountList items={o.manifests} />
        </section>
        <section aria-labelledby="defs">
          <h2 id="defs">Definitions</h2>
          <CountList items={o.symbols} />
        </section>
      </div>
    </article>
  );
}

function CountList({ items, unit }: { items: Count[] | null; unit?: string }) {
  if (!items?.length) return <p className="muted">None found.</p>;
  return (
    <ul className="counts">
      {items.map((c) => (
        <li key={c.Label}>
          <span>{c.Label.replace(/^(npm|py|go):/, "")}</span>
          <span className="num">{fmt(c.N)}{unit ? ` ${unit}` : ""}</span>
        </li>
      ))}
    </ul>
  );
}

const pct = (n: number, total: number) => `${total ? Math.round((n / total) * 100) : 0}%`;

export function ErrorNote({ msg }: { msg: string }) {
  return (
    <p className="notice warn" role="alert">
      {msg}
    </p>
  );
}
