import { useState } from "react";
import { api } from "./api";
import { useLoad } from "./hooks";
import { useRoute, type Route } from "./router";
import { OverviewView } from "./views/Overview";
import { MapView } from "./views/Map";
import { SearchView } from "./views/Search";
import { FileViewPage } from "./views/File";
import { DocsView } from "./views/Docs";
import { AskView } from "./views/Ask";
import { IssuesView } from "./views/Issues";

const NAV: { page: Route["page"]; label: string; href: string }[] = [
  { page: "overview", label: "Overview", href: "#/" },
  { page: "map", label: "Map", href: "#/map" },
  { page: "search", label: "Search", href: "#/search" },
  { page: "docs", label: "Docs", href: "#/docs" },
  { page: "ask", label: "Ask", href: "#/ask" },
  { page: "issues", label: "Issues", href: "#/issues" },
];

export function App() {
  const route = useRoute();
  const ov = useLoad(api.overview, []);
  const [scanning, setScanning] = useState(false);
  const [scanNote, setScanNote] = useState("");

  async function rescan() {
    setScanning(true);
    setScanNote("");
    try {
      const r = await api.rescan();
      setScanNote(`Rescanned ${r.files.toLocaleString()} files in ${(r.ms / 1000).toFixed(1)} s`);
      ov.reload();
    } catch (e) {
      setScanNote((e as Error).message);
    } finally {
      setScanning(false);
    }
  }

  return (
    <div className="shell">
      <nav className="rail" aria-label="Main">
        <a className="wordmark" href="#/" aria-label="KuliDev home">
          kuli<span>dev</span>
        </a>
        <ul>
          {NAV.map((n) => (
            <li key={n.page}>
              <a href={n.href} aria-current={route.page === n.page ? "page" : undefined}>
                {n.label}
              </a>
            </li>
          ))}
        </ul>
        <div className="rail-foot">
          {ov.data && (
            <>
              <p className="repo-name" title={ov.data.root}>{ov.data.repo}</p>
              <p className="muted small">
                Commit {ov.data.commit ? ov.data.commit.slice(0, 8) : "unknown"}
                {ov.data.stale && <span className="warn-text"><br />New commits since this scan</span>}
              </p>
            </>
          )}
          <button className="btn" onClick={rescan} disabled={scanning}>
            {scanning ? "Scanning…" : "Rescan"}
          </button>
          {scanNote && <p className="muted small" role="status">{scanNote}</p>}
        </div>
      </nav>
      <main className="main" key={route.page + ("path" in route ? route.path : "")}>
        {route.page === "overview" && <OverviewView ov={ov} onRescan={rescan} />}
        {route.page === "map" && <MapView selected={route.component} />}
        {route.page === "search" && <SearchView initial={route.q} />}
        {route.page === "file" && <FileViewPage path={route.path} from={route.from} to={route.to} />}
        {route.page === "docs" && <DocsView name={route.name} />}
        {route.page === "ask" && <AskView enabled={ov.data?.llm ?? false} />}
        {route.page === "issues" && <IssuesView />}
      </main>
    </div>
  );
}
