// Hash routes: #/  #/map  #/search?q=  #/file/<path>#L10-L20  #/docs/<name>  #/ask  #/issues
export type Route =
  | { page: "overview" }
  | { page: "map"; component?: string }
  | { page: "search"; q: string }
  | { page: "file"; path: string; from?: number; to?: number }
  | { page: "docs"; name?: string }
  | { page: "ask" }
  | { page: "issues" }
  | { page: "pulse" };

export function parse(hash: string): Route {
  const h = decodeURIComponent(hash.replace(/^#/, "")) || "/";
  if (h.startsWith("/file/")) {
    const [path, anchor] = h.slice(6).split("#L");
    const m = anchor?.match(/^(\d+)(?:-L?(\d+))?/);
    return { page: "file", path, from: m ? +m[1] : undefined, to: m ? +(m[2] ?? m[1]) : undefined };
  }
  const [base, query = ""] = h.split("?");
  const params = new URLSearchParams(query);
  switch (base) {
    case "/map": return { page: "map", component: params.get("c") ?? undefined };
    case "/search": return { page: "search", q: params.get("q") ?? "" };
    case "/ask": return { page: "ask" };
    case "/issues": return { page: "issues" };
    case "/pulse": return { page: "pulse" };
  }
  if (base === "/docs" || base.startsWith("/docs/")) return { page: "docs", name: base.slice(6) || undefined };
  return { page: "overview" };
}

export const href = {
  file: (path: string, from?: number, to?: number) =>
    `#/file/${path}${from ? `#L${from}${to && to !== from ? `-L${to}` : ""}` : ""}`,
  map: (c?: string) => (c ? `#/map?c=${encodeURIComponent(c)}` : "#/map"),
  search: (q: string) => `#/search?q=${encodeURIComponent(q)}`,
  doc: (name: string) => `#/docs/${name}`,
};
