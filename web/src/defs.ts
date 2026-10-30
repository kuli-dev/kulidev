// Definition lookups shared by the editor's hover/go-to-definition. Kept out of
// monaco.ts so clearing the cache doesn't pull the editor into the first download.
import { api, type Hit } from "./api";

const cache = new Map<string, Promise<Hit[]>>();

export function definitions(name: string): Promise<Hit[]> {
  if (!/^[A-Za-z_][A-Za-z0-9_]{1,80}$/.test(name)) return Promise.resolve([]);
  let p = cache.get(name);
  if (!p) {
    p = api.definition(name).then((r) => r.hits, () => []);
    cache.set(name, p);
  }
  return p;
}

export const clearDefinitionCache = () => cache.clear();
