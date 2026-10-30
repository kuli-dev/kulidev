// Typed client for the kuli serve JSON API.

export type Count = { Label: string; N: number; Lines: number };
export type Hit = { Kind: string; Name: string; Path: string; Line: number; EndLine: number; Detail: string };

export type Overview = {
  repo: string; root: string; commit: string; head: string; stale: boolean; scannedAt: string;
  files: number; parsed: number; languages: Count[] | null; symbols: Count[] | null;
  internalEdges: number; externalMods: number; topInternal: Count[] | null; topExternal: Count[] | null;
  manifests: Count[] | null; llm: boolean;
};

export type GraphNode = { name: string; files: number; lines: number; lang: string; hub: string; hubImporters: number; hubDoc?: string };
export type Graph = { components: GraphNode[] | null; edges: { from: string; to: string; n: number }[] | null; total: number };

export type Component = {
  name: string; lines: number; lang: string; langs: Record<string, number>;
  files: { path: string; lines: number; importers: number; doc?: string }[];
  symbols: { Kind: string; Name: string; Path: string; Line: number; EndLine: number; Doc: string }[] | null;
  dependsOn: Record<string, number>; usedBy: Record<string, number>;
};

export type FileView = {
  path: string; lang: string; doc: string; lines: string[]; tooLarge?: boolean;
  symbols: Hit[]; importers: Hit[]; imports: Hit[];
};

export type DocItem = {
  id: string; file: string; title: string; mode: string; citations: number; citations_removed: number;
  citation_coverage: number; status: string; changed?: string[];
};

export type AskResult = { answer: string; keywords: string[]; citations: number; removed: number; coverage: number; hits: number };

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  const body = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(body.error || `${res.status} ${res.statusText}`);
  return body as T;
}

const q = (s: string) => encodeURIComponent(s);

export const api = {
  overview: () => call<Overview>("api/overview"),
  graph: (limit: number) => call<Graph>(`api/graph?limit=${limit}`),
  component: (name: string) => call<Component>(`api/component?name=${q(name)}`),
  search: (text: string) => call<{ hits: Hit[] }>(`api/search?q=${q(text)}&limit=60`),
  file: (path: string) => call<FileView>(`api/file?path=${q(path)}`),
  broken: () => call<{ hits: Hit[] }>("api/broken"),
  docs: () => call<{ docs: DocItem[] | null; hint?: string }>("api/docs"),
  doc: (name: string) => call<{ markdown: string }>(`api/docs/${q(name)}`),
  ask: (question: string) =>
    call<AskResult>("api/ask", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ question }) }),
  rescan: () => call<{ files: number; ms: number }>("api/rescan", { method: "POST" }),
};
