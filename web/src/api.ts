// Typed client for the kuli serve JSON API.

export type Count = { Label: string; N: number; Lines: number };
export type Hit = { Kind: string; Name: string; Path: string; Line: number; EndLine: number; Detail: string };

export type Overview = {
  repo: string; root: string; commit: string; head: string; stale: boolean; scannedAt: string;
  files: number; parsed: number; languages: Count[] | null; symbols: Count[] | null;
  internalEdges: number; externalMods: number; topInternal: Count[] | null; topExternal: Count[] | null;
  manifests: Count[] | null; llm: boolean; model?: string; readOnly?: boolean; terminals?: boolean;
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
  content?: string; hash?: string; writable?: boolean;
  symbols: Hit[]; importers: Hit[]; imports: Hit[];
};

export type DocItem = {
  id: string; file: string; title: string; mode: string; citations: number; citations_removed: number;
  citation_coverage: number; status: string; changed?: string[];
};

export type TreeFile = { p: string; l?: string; n: number };

export type TermInfo = { id: string; title: string; started: string; exited: boolean; exitCode: number };

export type AgentInfo = { id: string; name: string; installed: boolean; mcp: boolean; hint?: string };

export type Device = { id: string; name: string; created: string; lastSeen: string };

export type GitChange = { path: string; origPath?: string; index: string; worktree: string; conflict?: boolean };
export type GitStatus = { git: boolean; branch: string; upstream?: string; ahead: number; behind: number; initial: boolean; changes: GitChange[] };

export type AskResult = { answer: string; keywords: string[]; citations: number; removed: number; coverage: number; hits: number };

export class ApiError extends Error {
  constructor(message: string, readonly status: number, readonly body: Record<string, unknown>) {
    super(message);
  }
}

async function call<T>(path: string, init?: RequestInit): Promise<T> {
  // The server requires this header on writes: other origins can't send it.
  const headers = { ...(init?.headers ?? {}), "X-Kuli-Request": "1" };
  const res = await fetch(path, { ...init, headers });
  const body = await res.json().catch(() => ({}));
  // Remote workspaces: an unpaired (or revoked) device is sent to the pairing screen.
  if (res.status === 401 && body.auth) dispatchEvent(new Event("kuli-auth-required"));
  if (!res.ok) throw new ApiError(body.error || `${res.status} ${res.statusText}`, res.status, body);
  return body as T;
}

const q = (s: string) => encodeURIComponent(s);

export const api = {
  overview: () => call<Overview>("api/overview"),
  graph: (limit: number) => call<Graph>(`api/graph?limit=${limit}`),
  component: (name: string) => call<Component>(`api/component?name=${q(name)}`),
  search: (text: string) => call<{ hits: Hit[] }>(`api/search?q=${q(text)}&limit=60`),
  file: (path: string) => call<FileView>(`api/file?path=${q(path)}`),
  tree: () => call<{ files: TreeFile[] }>("api/tree"),
  definition: (name: string) => call<{ hits: Hit[] }>(`api/definition?name=${q(name)}`),
  broken: () => call<{ hits: Hit[] }>("api/broken"),
  docs: () => call<{ docs: DocItem[] | null; hint?: string }>("api/docs"),
  doc: (name: string) => call<{ markdown: string }>(`api/docs/${q(name)}`),
  ask: (question: string) =>
    call<AskResult>("api/ask", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ question }) }),
  save: (path: string, content: string, baseHash: string, force = false) =>
    call<{ hash: string; bytes: number }>("api/file", {
      method: "PUT", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ path, content, baseHash, force }),
    }),
  terminals: () => call<{ enabled: boolean; terminals: TermInfo[] }>("api/terminals"),
  startTerminal: (cols: number, rows: number) =>
    call<TermInfo>("api/terminals", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ cols, rows }) }),
  killTerminal: (id: string) => call<{ ok: boolean }>(`api/terminals/${q(id)}`, { method: "DELETE" }),
  agents: () => call<{ enabled: boolean; agents: AgentInfo[] }>("api/agents"),
  startAgent: (id: string, cols: number, rows: number) =>
    call<TermInfo>("api/agents", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ id, cols, rows }) }),
  authState: () => call<{ required: boolean; paired: boolean; device?: Device }>("api/auth/state"),
  pair: (code: string, name: string) =>
    call<{ device: Device }>("api/pair", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ code, name }) }),
  logout: () => call<{ ok: boolean }>("api/logout", { method: "POST" }),
  devices: () => call<{ required: boolean; current: string; devices: Device[] }>("api/devices"),
  newPairingCode: () => call<{ code: string; expiresIn: number }>("api/devices/code", { method: "POST" }),
  revokeDevice: (id: string) => call<{ ok: boolean }>(`api/devices/${q(id)}`, { method: "DELETE" }),
  gitStatus: () => call<{ status: GitStatus; writable: boolean }>("api/git/status"),
  gitDiff: (path: string, staged: boolean) =>
    call<{ original: string; modified: string; binary: boolean }>(`api/git/diff?path=${q(path)}${staged ? "&staged=1" : ""}`),
  git: (op: "stage" | "unstage" | "discard" | "commit", body: { paths?: string[]; message?: string; untracked?: boolean }) =>
    call<{ ok: boolean; commit?: string }>(`api/git/${op}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
  rescan: () => call<{ files: number; ms: number }>("api/rescan", { method: "POST" }),
};
