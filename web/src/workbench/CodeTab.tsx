import { useEffect, useRef, useState } from "react";
import { api, ApiError } from "../api";
import { clearDefinitionCache, installKnowledge, languageOf, monaco, themeName, uriOf } from "../monaco";
import { forgetFile, loadFile } from "./files";

// Per-file editing state lives outside React so it survives tab switches.
const viewStates = new Map<string, monaco.editor.ICodeEditorViewState | null>();
const baseHash = new Map<string, string>(); // hash of the version on disk the edits started from
const savedVersion = new Map<string, number>(); // model version id at last load/save

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
  const [error, setError] = useState("");
  const [note, setNote] = useState("");
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
        ed.onDidChangeModelContent(() => onDirty(path, isDirty(path)));
        ed.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => saveRef.current(false));
        onCursor({ line: 1, col: 1, lang: languageOf(path) });
        highlight(from, to);
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
        overviewRuler: { color: "#f2c230", position: monaco.editor.OverviewRulerLane.Full },
        minimap: { color: "#f2c230", position: monaco.editor.MinimapPosition.Inline },
      },
    }]);
    ed.revealLinesInCenter(a, end);
    ed.setPosition({ lineNumber: a, column: 1 });
  }

  if (error) return <p className="notice warn pad">{error}</p>;
  return (
    <>
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
      <div className={`monaco-host ${save.kind === "conflict" || save.kind === "error" ? "with-banner" : ""}`} ref={host} />
      <span className="sr-only" role="status">{save.kind === "saved" ? "Saved" : save.kind === "saving" ? "Saving" : ""}</span>
    </>
  );
}
