import DOMPurify from "dompurify";
import { Marked } from "marked";
import { useMemo } from "react";
import { href } from "./router";

// Docs and answers come from an LLM: render Markdown, then sanitize, and only allow
// in-app (#/…) or http(s) links.
const md = new Marked({
  gfm: true,
  renderer: {
    code({ text, lang }) {
      if (lang === "mermaid") {
        return `<p class="callout">This diagram is interactive on the <a href="${href.map()}">Map</a>.</p>`;
      }
      const esc = text.replace(/&/g, "&amp;").replace(/</g, "&lt;");
      return `<pre class="code-block"><code>${esc}</code></pre>`;
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

export function Markdown({ source }: { source: string }) {
  const html = useMemo(() => DOMPurify.sanitize(md.parse(source) as string), [source]);
  return <div className="prose" dangerouslySetInnerHTML={{ __html: html }} />;
}
