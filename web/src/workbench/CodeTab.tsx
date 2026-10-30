import { useEffect, useRef, useState } from "react";
import { installKnowledge, languageOf, monaco, themeName, uriOf } from "../monaco";
import { loadFile } from "./files";

const viewStates = new Map<string, monaco.editor.ICodeEditorViewState | null>();

export type Cursor = { line: number; col: number; lang: string };

export function CodeTab({ path, from, to, onOpen, onCursor }: {
  path: string; from?: number; to?: number;
  onOpen: (path: string, from?: number, to?: number) => void;
  onCursor: (c: Cursor | null) => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor | null>(null);
  const deco = useRef<monaco.editor.IEditorDecorationsCollection | null>(null);
  const [error, setError] = useState("");
  const [note, setNote] = useState("");

  useEffect(() => {
    installKnowledge(onOpen);
    let disposed = false;
    loadFile(path).then(
      (f) => {
        if (disposed || !host.current) return;
        if (f.tooLarge) setNote("This file is over 2 MB and is not shown.");
        const uri = uriOf(path);
        const model = monaco.editor.getModel(uri) ?? monaco.editor.createModel(f.lines.join("\n"), languageOf(path), uri);
        const ed = monaco.editor.create(host.current, {
          model, readOnly: true, theme: themeName(), automaticLayout: true,
          fontFamily: "'Martian Mono', ui-monospace, monospace", fontSize: 12.5, lineHeight: 21,
          scrollBeyondLastLine: false, renderLineHighlight: "line", minimap: { enabled: true, renderCharacters: false },
          domReadOnly: false, readOnlyMessage: { value: "KuliDev is read-only for now — editing is coming." },
          stickyScroll: { enabled: true }, smoothScrolling: true,
        });
        editor.current = ed;
        const vs = viewStates.get(path);
        if (vs && !from) ed.restoreViewState(vs);
        ed.onDidChangeCursorPosition((e) => onCursor({ line: e.position.lineNumber, col: e.position.column, lang: languageOf(path) }));
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
      <div className="monaco-host" ref={host} />
    </>
  );
}
