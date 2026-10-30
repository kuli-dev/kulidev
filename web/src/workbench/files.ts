import { api, type FileView } from "../api";

// One fetch per file per session; cleared on rescan.
const cache = new Map<string, Promise<FileView>>();
export function loadFile(path: string): Promise<FileView> {
  let p = cache.get(path);
  if (!p) {
    p = api.file(path);
    p.catch(() => cache.delete(path));
    cache.set(path, p);
  }
  return p;
}
export const clearFileCache = () => cache.clear();
export const forgetFile = (path: string) => cache.delete(path);

export type Tab =
  | { id: string; kind: "welcome" }
  | { id: string; kind: "map"; component?: string }
  | { id: string; kind: "doc"; name: string }
  | { id: string; kind: "file"; path: string; from?: number; to?: number }
  | { id: string; kind: "diff"; path: string; staged: boolean }
  | { id: string; kind: "review" }
  | { id: string; kind: "systems" };

// A tab before it has an id (Omit applied to each member of the union).
export type NewTab = Tab extends infer T ? (T extends Tab ? Omit<T, "id"> : never) : never;

export const tabTitle = (t: Tab) =>
  t.kind === "welcome" ? "Welcome" : t.kind === "map" ? "Map" : t.kind === "systems" ? "Systems" : t.kind === "review" ? "Review" : t.kind === "doc" ? t.name.replace(/\.md$/, "").replace(/-/g, " ")
    : t.kind === "diff" ? `${t.path.split("/").pop()} (${t.staged ? "staged" : "changes"})` : t.path.split("/").pop()!;

// Subsequence fuzzy match for quick open: rewards consecutive and basename matches.
export function fuzzyScore(query: string, path: string): number {
  const q = query.toLowerCase();
  const p = path.toLowerCase();
  const base = p.slice(p.lastIndexOf("/") + 1);
  if (!q) return 0;
  if (base.startsWith(q)) return 1000 - p.length;
  if (base.includes(q)) return 800 - p.length;
  let score = 0, last = -2, j = 0;
  for (let i = 0; i < p.length && j < q.length; i++) {
    if (p[i] === q[j]) {
      score += i === last + 1 ? 6 : 1;
      if (i > p.lastIndexOf("/")) score += 2;
      last = i;
      j++;
    }
  }
  return j === q.length ? score - p.length / 100 : -1;
}
