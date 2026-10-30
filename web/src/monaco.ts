import { isDark } from "./theme";
// Monaco set up for KuliDev: core editor + syntax highlighting only (no language
// servers), local workers, KuliDev themes, and knowledge-graph hover/definition.
import "monaco-editor/esm/vs/editor/edcore.main";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import "monaco-editor/esm/vs/basic-languages/go/go.contribution";
import "monaco-editor/esm/vs/basic-languages/python/python.contribution";
import "monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution";
import "monaco-editor/esm/vs/basic-languages/javascript/javascript.contribution";
import "monaco-editor/esm/vs/basic-languages/yaml/yaml.contribution";
import "monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution";
import "monaco-editor/esm/vs/basic-languages/sql/sql.contribution";
import "monaco-editor/esm/vs/basic-languages/shell/shell.contribution";
import "monaco-editor/esm/vs/basic-languages/dockerfile/dockerfile.contribution";
import "monaco-editor/esm/vs/basic-languages/html/html.contribution";
import "monaco-editor/esm/vs/basic-languages/css/css.contribution";
import "monaco-editor/esm/vs/basic-languages/scss/scss.contribution";
import "monaco-editor/esm/vs/basic-languages/hcl/hcl.contribution";
import "monaco-editor/esm/vs/basic-languages/ini/ini.contribution";
import "monaco-editor/esm/vs/basic-languages/java/java.contribution";
import "monaco-editor/esm/vs/basic-languages/kotlin/kotlin.contribution";
import "monaco-editor/esm/vs/basic-languages/php/php.contribution";
import "monaco-editor/esm/vs/basic-languages/ruby/ruby.contribution";
import "monaco-editor/esm/vs/basic-languages/rust/rust.contribution";
import "monaco-editor/esm/vs/basic-languages/csharp/csharp.contribution";
import "monaco-editor/esm/vs/basic-languages/cpp/cpp.contribution";
import "monaco-editor/esm/vs/basic-languages/xml/xml.contribution";
import "monaco-editor/esm/vs/basic-languages/protobuf/protobuf.contribution";
import { api, type Hit } from "./api";

self.MonacoEnvironment = { getWorker: () => new EditorWorker() };

export { monaco };

const EXT: Record<string, string> = {
  go: "go", py: "python", ts: "typescript", tsx: "typescript", mts: "typescript", cts: "typescript",
  js: "javascript", jsx: "javascript", mjs: "javascript", cjs: "javascript", json: "javascript",
  yml: "yaml", yaml: "yaml", md: "markdown", sql: "sql", sh: "shell", bash: "shell",
  html: "html", css: "css", scss: "scss", tf: "hcl", toml: "ini", ini: "ini", cfg: "ini",
  java: "java", kt: "kotlin", php: "php", rb: "ruby", rs: "rust", cs: "csharp",
  c: "cpp", h: "cpp", cc: "cpp", cpp: "cpp", hpp: "cpp", xml: "xml", proto: "protobuf",
};

export function languageOf(path: string): string {
  const base = path.split("/").pop() ?? "";
  if (/^Dockerfile/.test(base) || base.endsWith(".Dockerfile")) return "dockerfile";
  if (base === "Makefile" || base.startsWith(".env")) return "shell";
  return EXT[base.split(".").pop()?.toLowerCase() ?? ""] ?? "plaintext";
}

export const uriOf = (path: string) => monaco.Uri.from({ scheme: "kuli", path: "/" + path });
export const pathOf = (uri: monaco.Uri) => uri.path.replace(/^\//, "");

// Palette matches styles.css: slate/concrete grounds, steel comments, ink text.
monaco.editor.defineTheme("kuli-dark", {
  base: "vs-dark", inherit: true,
  rules: [
    { token: "comment", foreground: "8b8b94", fontStyle: "italic" },
    { token: "string", foreground: "a7d3a0" },
    { token: "keyword", foreground: "fdba74" },
    { token: "number", foreground: "fbbf24" },
    { token: "type", foreground: "93c5fd" },
  ],
  colors: {
    "editor.background": "#111315", "editor.foreground": "#f5f5f4",
    "editorLineNumber.foreground": "#71717a", "editorLineNumber.activeForeground": "#f5f5f4",
    "editor.lineHighlightBackground": "#181a1d", "editor.selectionBackground": "#7c2d12",
    "editorCursor.foreground": "#f97316", "editorWidget.background": "#292d32", "editorWidget.border": "#34383e",
    "editorHoverWidget.background": "#292d32", "editorHoverWidget.border": "#34383e",
    "editorIndentGuide.background1": "#24272b", "focusBorder": "#f97316",
  },
});
monaco.editor.defineTheme("kuli-light", {
  base: "vs", inherit: true,
  rules: [
    { token: "comment", foreground: "78716c", fontStyle: "italic" },
    { token: "string", foreground: "15803d" },
    { token: "keyword", foreground: "c2410c" },
    { token: "number", foreground: "a16207" },
    { token: "type", foreground: "1d4ed8" },
  ],
  colors: {
    "editor.background": "#fafaf9", "editor.foreground": "#1c1917",
    "editorLineNumber.foreground": "#a8a29e", "editorLineNumber.activeForeground": "#1c1917",
    "editor.lineHighlightBackground": "#f0efed", "editor.selectionBackground": "#fed7aa",
    "editorCursor.foreground": "#ea580c", "editorWidget.background": "#ffffff", "editorHoverWidget.background": "#ffffff",
    "focusBorder": "#ea580c",
  },
});

export const themeName = () => (isDark() ? "kuli-dark" : "kuli-light");
addEventListener("kuli-theme", () => monaco.editor.setTheme(themeName()));

// ---- knowledge providers -----------------------------------------------------

import { definitions } from "./defs";

// Same-language definitions first: from Python, a Python class beats a TypeScript
// interface with the same name. Others are kept only when nothing matches.
function ranked(hits: Hit[], fromPath: string): Hit[] {
  const lang = languageOf(fromPath);
  const same = hits.filter((h) => languageOf(h.Path) === lang);
  return same.length ? same : hits;
}

// Peek needs the target text loaded as a Monaco model.
async function ensureModels(hits: Hit[]) {
  await Promise.all(
    hits.slice(0, 5).map(async (h) => {
      const uri = uriOf(h.Path);
      if (monaco.editor.getModel(uri)) return;
      try {
        const f = await api.file(h.Path);
        if (!monaco.editor.getModel(uri)) monaco.editor.createModel(f.lines.join("\n"), languageOf(h.Path), uri);
      } catch { /* unreadable target: peek shows the location only */ }
    }),
  );
}

let installed = false;

// installKnowledge registers hover + go-to-definition for every language, backed by
// the knowledge graph, and routes cross-file jumps to the workbench.
export function installKnowledge(openFile: (path: string, from?: number, to?: number) => void) {
  if (installed) return;
  installed = true;
  const langs = monaco.languages.getLanguages().map((l) => l.id);

  monaco.languages.registerHoverProvider(langs, {
    async provideHover(model, position) {
      const w = model.getWordAtPosition(position);
      if (!w) return null;
      const here = pathOf(model.uri);
      const hits = ranked(await definitions(w.word), here);
      if (!hits.length) return null;
      const lines = hits.slice(0, 3).map((h) => {
        const where = h.Path === here ? `line ${h.Line}` : `${h.Path}:${h.Line}`;
        return `**${h.Kind}** \`${h.Name}\` · ${where}${h.Detail ? `\n\n${h.Detail}` : ""}`;
      });
      if (hits.length > 3) lines.push(`_and ${hits.length - 3} more definitions with this name_`);
      return {
        range: new monaco.Range(position.lineNumber, w.startColumn, position.lineNumber, w.endColumn),
        contents: lines.map((value) => ({ value })),
      };
    },
  });

  monaco.languages.registerDefinitionProvider(langs, {
    async provideDefinition(model, position) {
      const w = model.getWordAtPosition(position);
      if (!w) return null;
      const hits = ranked(await definitions(w.word), pathOf(model.uri));
      if (hits.length > 1) await ensureModels(hits);
      return hits.map((h) => ({
        uri: uriOf(h.Path),
        range: new monaco.Range(h.Line, 1, Math.max(h.Line, h.EndLine), 1),
      }));
    },
  });

  // Ctrl/Cmd+click into another file: open it as a workbench tab instead of
  // asking Monaco to load a model it doesn't have.
  monaco.editor.registerEditorOpener({
    openCodeEditor(_source, resource, sel) {
      const line = sel && "startLineNumber" in sel ? sel.startLineNumber : sel && "lineNumber" in sel ? sel.lineNumber : undefined;
      const end = sel && "endLineNumber" in sel ? sel.endLineNumber : line;
      openFile(pathOf(resource), line, end);
      return true;
    },
  });
}
