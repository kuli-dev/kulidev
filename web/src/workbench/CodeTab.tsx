import { Suspense, lazy, useEffect, useRef, useState } from "react";
import { mermaidBlocks } from "../flowchart";
import { Markdown } from "../markdown";
import { api, ApiError } from "../api";
import { clearDefinitionCache } from "../defs";
import { installKnowledge, languageOf, monaco, themeName, uriOf } from "../monaco";
import { forgetFile, loadFile } from "./files";

// Per-file editing state lives outside React so it survives tab switches.
const viewStates = new Map<string, monaco.editor.ICodeEditorViewState | null>();
const baseHash = new Map<string, string>(); // hash of the version on disk the edits started from
const savedVersion = new Map<string, number>(); // model version id at last load/save

// Markdown and Mermaid files get a preview beside the code; the diagram editor loads
// on demand (it brings the canvas).
const MermaidEditor = lazy(() => import("./MermaidEditor").then((m) => ({ default: m.MermaidEditor })));
const isMarkdown = (p: string) => /\.(md|markdown|mdx)$/i.test(p);
const isMermaid = (p: string) => /\.(mmd|mermaid)$/i.test(p);
type ViewMode = "code" | "split" | "preview";
const NEW_DIAGRAM = "flowchart TD\n    A[Start] --> B{Decision?}\n    B -->|yes| C[Do it]\n    B -->|no| D[Skip]";

function savedMode(): ViewMode {
  try {
    const v = localStorage.getItem("kuli.mdview");
    if (v === "code" || v === "split" || v === "preview") return v;
  } catch { /* storage off */ }
  return matchMedia("(max-width: 900px)").matches ? "preview" : "split";
}

export type Cursor = { line: number; col: number; lang: string };
type SaveState = { kind: "idle" } | { kind: "saving" } | { kind: "saved" } | { kind: "conflict" } | { kind: "error"; msg: string };

export const isDirty = (path: string) => {
  const m = monaco.editor.getModel(uriOf(path));
  return !!m && savedVersion.has(path) && m.getAlternativeVersionId() !== savedVersion.get(path);
};

export function CodeTab({ path, from, to, onOpen, onCursor, onDirty }: {
  path: string; from?: number; to?: number;
  onOpen: (path: string, from?: number, to?: number) => void;
  onCursor: (c: Cursor | null) => void;
  onDirty: (path: string, dirty: boolean) => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const deco = useRef<monaco.editor.IEditorDecorationsCollection | null>(null);
  const gutter = useRef<monaco.editor.IEditorDecorationsCollection | null>(null);

  // Git change markers in the margin (vs HEAD), like VS Code's green/blue/red bars.
  function refreshGutter() {
    api.gitLines(path).then((r) => {
      const ed = editor.current;
      if (!ed) return;
      gutter.current?.clear();
      gutter.current = ed.createDecorationsCollection(r.changes.map((c) => {
        const start = Math.max(1, c.start);
        const end = c.kind === "deleted" ? start : start + Math.max(1, c.count) - 1;
        return {
          range: new monaco.Range(start, 1, end, 1),
          options: {
            isWholeLine: true, linesDecorationsClassName: `gutter-${c.kind}`,
            overviewRuler: { color: c.kind === "added" ? "#22c55e" : c.kind === "deleted" ? "#ef4444" : "#3b82f6", position: monaco.editor.OverviewRulerLane.Left },
          },
        };
      }));
    }, () => {});
  }
  const [error, setError] = useState("");
  const previewable = isMarkdown(path) || isMermaid(path);
  const [mode, setModeState] = useState<ViewMode>(savedMode);
  const setMode = (m: ViewMode) => { setModeState(m); try { localStorage.setItem("kuli.mdview", m); } catch { /* storage off */ } };
  const view: ViewMode = previewable ? mode : "code";
  const [source, setSource] = useState(""); // what the preview shows (follows the editor)
  const [canWrite, setCanWrite] = useState(false);
  const [diagram, setDiagram] = useState<{ index: number; code: string } | null>(null);
  const [note, setNote] = useState("");
  // Menu commands (File > Save, Edit > Undo, Find, …) for the open editor.
  useEffect(() => {
    const on = (e: Event) => {
      const id = (e as CustomEvent<string>).detail;
      const ed = editor.current;
      if (!ed) return;
      if (id === "save") saveRef.current(false);
      else { ed.focus(); ed.trigger("menu", id, null); }
    };
    addEventListener("kuli-editor-action", on);
    return () => removeEventListener("kuli-editor-action", on);
  }, []);
  const [save, setSave] = useState<SaveState>({ kind: "idle" });
  const saveRef = useRef<(force: boolean) => void>(() => {});

  useEffect(() => {
    installKnowledge(onOpen);
    let disposed = false;
    loadFile(path).then(
      (f) => {
        if (disposed || !host.current) return;
        if (f.tooLarge) setNote("This file is over 2 MB and is not shown.");
        const writable = !!f.writable && !f.tooLarge && f.hash !== undefined;
        const uri = uriOf(path);
        let model = monaco.editor.getModel(uri);
        if (!model) {
          model = monaco.editor.createModel(f.content ?? f.lines.join("\n"), languageOf(path), uri);
        }
        if (!savedVersion.has(path)) {
          savedVersion.set(path, model.getAlternativeVersionId());
          if (f.hash) baseHash.set(path, f.hash);
        }
        setCanWrite(writable);
        setSource(model.getValue());
        const ed = monaco.editor.create(host.current, {
          model, readOnly: !writable, theme: themeName(), automaticLayout: true,
          fontFamily: "'Martian Mono', ui-monospace, monospace", fontSize: 12.5, lineHeight: 21,
          scrollBeyondLastLine: false, renderLineHighlight: "line", minimap: { enabled: true, renderCharacters: false },
          readOnlyMessage: { value: "This workspace is read-only." }, stickyScroll: { enabled: true },
        });
        editor.current = ed;
        const vs = viewStates.get(path);
        if (vs && !from) ed.restoreViewState(vs);
        ed.onDidChangeCursorPosition((e) => onCursor({ line: e.position.lineNumber, col: e.position.column, lang: languageOf(path) }));
        let pending: ReturnType<typeof setTimeout> | undefined;
        ed.onDidChangeModelContent(() => {
          onDirty(path, isDirty(path));
          clearTimeout(pending);
          pending = setTimeout(() => setSource(ed.getModel()?.getValue() ?? ""), 200);
        });
        ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => saveRef.current(false));
        onCursor({ line: 1, col: 1, lang: languageOf(path) });
        highlight(from, to);
        refreshGutter();
      },
      (e: Error) => !disposed && setError(e.message),
    );
    return () => {
      disposed = true;
      if (editor.current) {
        viewStates.set(path, editor.current.saveViewState());
        editor.current.dispose();
        editor.current = null;
      }
      onCursor(null);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path]);

  useEffect(() => {
    highlight(from, to);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [from, to]);

  saveRef.current = async (force: boolean) => {
    const model = monaco.editor.getModel(uriOf(path));
    if (!model || (!force && !isDirty(path))) return;
    const version = model.getAlternativeVersionId();
    setSave({ kind: "saving" });
    try {
      const r = await api.save(path, model.getValue(), baseHash.get(path) ?? "", force);
      baseHash.set(path, r.hash);
      savedVersion.set(path, version);
      onDirty(path, isDirty(path));
      forgetFile(path);
      setSave({ kind: "saved" });
      refreshGutter();
      // The server rescans shortly after a save; drop cached definitions after it.
      setTimeout(clearDefinitionCache, 3000);
    } catch (e) {
      setSave(e instanceof ApiError && e.status === 409 ? { kind: "conflict" } : { kind: "error", msg: (e as Error).message });
    }
  };

  async function reloadFromDisk() {
    forgetFile(path);
    const f = await loadFile(path);
    const model = monaco.editor.getModel(uriOf(path));
    if (!model) return;
    model.setValue(f.content ?? f.lines.join("\n"));
    savedVersion.set(path, model.getAlternativeVersionId());
    if (f.hash) baseHash.set(path, f.hash);
    onDirty(path, false);
    setSave({ kind: "idle" });
  }

  function highlight(a?: number, b?: number) {
    const ed = editor.current;
    if (!ed) return;
    deco.current?.clear();
    if (!a) return;
    const end = Math.max(a, b ?? a);
    deco.current = ed.createDecorationsCollection([{
      range: new monaco.Range(a, 1, end, 1),
      options: {
        isWholeLine: true, className: "mono-cited", linesDecorationsClassName: "mono-cited-bar",
        overviewRuler: { color: "#facc15", position: monaco.editor.OverviewRulerLane.Full },
        minimap: { color: "#facc15", position: monaco.editor.MinimapPosition.Inline },
      },
    }]);
    ed.revealLinesInCenter(a, end);
    ed.setPosition({ lineNumber: a, column: 1 });
  }

  // Diagrams in the document: the i-th ```mermaid block, or the whole .mmd file.
  const blocks = () => {
    const model = editor.current?.getModel();
    if (!model) return [];
    const text = model.getValue();
    if (isMermaid(path)) return [{ start: -1, end: model.getLineCount(), code: text }];
    return mermaidBlocks(text);
  };
  function applyDiagram(index: number, code: string, andSave: boolean) {
    const ed = editor.current;
    const model = ed?.getModel();
    const b = blocks()[index];
    if (!ed || !model || !b) return;
    const range = b.start < 0
      ? model.getFullModelRange()
      : new monaco.Range(b.start + 2, 1, b.end + 1, 1); // the lines between the fences
    ed.pushUndoStop();
    ed.executeEdits("diagram-editor", [{ range, text: b.start < 0 ? code : `${code}\n` }]);
    ed.pushUndoStop();
    setDiagram(null);
    if (andSave) saveRef.current(false);
  }
  function deleteDiagram(index: number) {
    const ed = editor.current;
    const model = ed?.getModel();
    const b = blocks()[index];
    if (!ed || !model || !b || b.start < 0) return;
    const last = b.end + 1 >= model.getLineCount();
    const range = last
      ? new monaco.Range(Math.max(1, b.start), b.start > 0 ? model.getLineMaxColumn(b.start) : 1, b.end + 1, model.getLineMaxColumn(b.end + 1))
      : new monaco.Range(b.start + 1, 1, b.end + 2, 1);
    ed.pushUndoStop();
    ed.executeEdits("diagram-editor", [{ range, text: "" }]);
    ed.pushUndoStop();
  }
  function insertDiagram() {
    const ed = editor.current;
    const model = ed?.getModel();
    if (!ed || !model) return;
    const line = ed.getPosition()?.lineNumber ?? model.getLineCount();
    const at = model.getLineMaxColumn(line);
    ed.pushUndoStop();
    ed.executeEdits("diagram-editor", [{ range: new monaco.Range(line, at, line, at), text: `\n\n\`\`\`mermaid\n${NEW_DIAGRAM}\n\`\`\`\n` }]);
    ed.pushUndoStop();
    const index = blocks().findIndex((b) => b.start >= line);
    if (index >= 0) setDiagram({ index, code: NEW_DIAGRAM });
  }
  const editDiagram = (index: number) => {
    const b = blocks()[index];
    if (b) setDiagram({ index, code: b.code });
  };

  if (error) return <p className="notice warn pad">{error}</p>;
  return (
    <>
      {previewable && (
        <div className="md-bar">
          <div className="seg" role="radiogroup" aria-label="View">
            {(["code", "split", "preview"] as ViewMode[]).map((m) => (
              <button key={m} role="radio" aria-checked={view === m} className={view === m ? "on" : ""} onClick={() => setMode(m)}>
                {m === "code" ? "Code" : m === "split" ? "Split" : "Preview"}
              </button>
            ))}
          </div>
          <span className="st-gap" />
          {canWrite && isMarkdown(path) && <button className="btn small-btn" onClick={insertDiagram}>Insert diagram</button>}
          {isMermaid(path) && <button className="btn small-btn" onClick={() => editDiagram(0)}>{canWrite ? "Edit diagram" : "Open diagram"}</button>}
        </div>
      )}
      {note && <p className="notice pad">{note}</p>}
      {save.kind === "conflict" && (
        <div className="editor-banner warn" role="alert">
          <span>This file changed on disk after you opened it. Saving now would overwrite those changes.</span>
          <button className="btn" onClick={reloadFromDisk}>Reload from disk</button>
          <button className="btn" onClick={() => saveRef.current(true)}>Overwrite with mine</button>
        </div>
      )}
      {save.kind === "error" && (
        <div className="editor-banner warn" role="alert">
          <span>Not saved: {save.msg}</span>
          <button className="btn" onClick={() => saveRef.current(false)}>Try again</button>
        </div>
      )}
      <div className={`monaco-host ${save.kind === "conflict" || save.kind === "error" ? "with-banner" : ""} ${previewable ? `with-bar view-${view}` : ""}`} ref={host} />
      {previewable && view !== "code" && (
        <div className={`md-preview view-${view}`}>
          {isMermaid(path) ? (
            <Markdown source={"```mermaid\n" + source + "\n```"} onEditDiagram={() => editDiagram(0)} />
          ) : (
            <Markdown source={source} base={path.includes("/") ? path.slice(0, path.lastIndexOf("/")) : ""}
              onEditDiagram={canWrite ? editDiagram : undefined} onDeleteDiagram={canWrite ? deleteDiagram : undefined} />
          )}
        </div>
      )}
      {diagram && (
        <Suspense fallback={null}>
          <MermaidEditor code={diagram.code} title={path} readOnly={!canWrite}
            onApply={(code, andSave) => applyDiagram(diagram.index, code, andSave)} onClose={() => setDiagram(null)} />
        </Suspense>
      )}
      <span className="sr-only" role="status">{save.kind === "saved" ? "Saved" : save.kind === "saving" ? "Saving" : ""}</span>
    </>
  );
}
