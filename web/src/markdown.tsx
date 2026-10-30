import DOMPurify from "dompurify";
import { Marked } from "marked";
import { useEffect, useMemo, useRef } from "react";
import { renderMermaid } from "./mermaid";
import { href } from "./router";

// Render Markdown, then sanitize, and only allow in-app (#/…) or http(s) links. Docs and
// answers come from an LLM, so nothing in the source may run. Mermaid blocks become
// diagrams (Mermaid's strict mode: no scripts, no HTML labels).

let linkBase: string | null = null; // the previewed file's folder, for relative links
let diagrams = 0;
let diagramActions = false;

const esc = (s: string) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");

function resolve(base: string, rel: string): string {
  const parts = base ? base.split("/") : [];
  for (const p of rel.split("/")) {
    if (p === "..") parts.pop();
    else if (p && p !== ".") parts.push(p);
  }
  return parts.join("/");
}

const md = new Marked({
  gfm: true,
  renderer: {
    code({ text, lang }) {
      if (lang === "mermaid") {
        const i = diagrams++;
        const tools = diagramActions
          ? `<span class="mmd-tools"><button type="button" class="btn small-btn" data-mmd-edit="${i}">Edit diagram</button><button type="button" class="btn small-btn" data-mmd-delete="${i}">Delete</button></span>`
          : "";
        return `<figure class="mmd" data-mmd="${i}">${tools}<div class="mmd-svg"><pre class="code-block"><code>${esc(text)}</code></pre></div></figure>`;
      }
      return `<pre class="code-block"><code>${esc(text)}</code></pre>`;
    },
    image({ href: h, text, title }) {
      let src = h ?? "";
      // Images beside the previewed file come from the folder (scanned files only).
      if (linkBase !== null && src && !/^[a-z]+:|^\/\/|^#/i.test(src)) {
        const rel = src.startsWith("/") ? src.slice(1) : resolve(linkBase, decodeURI(src.split(/[?#]/)[0]));
        src = `api/raw?path=${encodeURIComponent(rel)}`;
      }
      return `<img src="${esc(src)}" alt="${esc(text)}"${title ? ` title="${esc(title)}"` : ""} loading="lazy">`;
    },
    link({ href: h, text }) {
      let target = h ?? "";
      if (linkBase !== null && target && !/^[a-z]+:|^#|^\//i.test(target)) {
        const [file, frag] = target.split("#");
        const line = /^L(\d+)/.exec(frag ?? "");
        target = href.file(resolve(linkBase, decodeURI(file)), line ? +line[1] : undefined);
      }
      return `<a href="${esc(target)}">${text}</a>`;
    },
  },
});

DOMPurify.addHook("afterSanitizeAttributes", (node) => {
  if (node.tagName === "A") {
    const h = node.getAttribute("href") ?? "";
    if (!h.startsWith("#/") && !/^https?:\/\//.test(h)) node.removeAttribute("href");
    if (/^https?:\/\//.test(h)) {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer");
    }
    if (h.startsWith("#/file/")) node.setAttribute("class", "cite");
  }
});

// The rendered SVG is Mermaid's own output (strict mode); it is sanitized again as SVG.
// Flowchart labels are HTML inside <foreignObject>; HTML is allowed only there.
export const cleanSvg = (svg: string) =>
  DOMPurify.sanitize(svg, {
    USE_PROFILES: { svg: true, svgFilters: true, html: true }, ADD_TAGS: ["foreignObject"],
    HTML_INTEGRATION_POINTS: { foreignobject: true },
  } as Parameters<typeof DOMPurify.sanitize>[1]);

export function Markdown({ source, base, onEditDiagram, onDeleteDiagram, className = "prose" }: {
  source: string;
  base?: string; // folder of the file being previewed: relative links open it in KuliDev
  onEditDiagram?: (index: number) => void;
  onDeleteDiagram?: (index: number) => void;
  className?: string;
}) {
  const box = useRef<HTMLDivElement>(null);
  const html = useMemo(() => {
    linkBase = base ?? null;
    diagrams = 0;
    diagramActions = !!onEditDiagram;
    try {
      return DOMPurify.sanitize(md.parse(source) as string, { ADD_ATTR: ["data-mmd", "data-mmd-edit", "data-mmd-delete"] });
    } finally {
      linkBase = null;
    }
  }, [source, base, onEditDiagram]);

  // Draw the diagrams after each render; a failed one keeps its code and shows why.
  useEffect(() => {
    const el = box.current;
    if (!el) return;
    let cancelled = false;
    const draw = () => {
      el.querySelectorAll<HTMLElement>("figure.mmd").forEach((fig) => {
        const pre = fig.querySelector(".mmd-svg");
        const code = fig.dataset.code ?? fig.querySelector("code")?.textContent ?? "";
        fig.dataset.code = code;
        renderMermaid(code).then(
          (svg) => { if (!cancelled && pre) pre.innerHTML = cleanSvg(svg); },
          (e: Error) => {
            if (cancelled || !pre) return;
            pre.innerHTML = `<p class="notice warn">This diagram has an error: ${esc(e.message.split("\n").slice(0, 3).join(" "))}</p><pre class="code-block"><code>${esc(code)}</code></pre>`;
          },
        );
      });
    };
    draw();
    addEventListener("kuli-theme", draw);
    return () => { cancelled = true; removeEventListener("kuli-theme", draw); };
  }, [html]);

  return (
    <div className={className} ref={box} dangerouslySetInnerHTML={{ __html: html }}
      onClick={(e) => {
        const t = e.target as HTMLElement;
        const edit = t.closest<HTMLElement>("[data-mmd-edit]");
        const del = t.closest<HTMLElement>("[data-mmd-delete]");
        if (edit && onEditDiagram) onEditDiagram(+edit.dataset.mmdEdit!);
        if (del && onDeleteDiagram) onDeleteDiagram(+del.dataset.mmdDelete!);
      }} />
  );
}
