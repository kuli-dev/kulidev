import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { languageOf, monaco, themeName } from "../monaco";

// Monaco's diff editor: index vs working tree, or HEAD vs index for staged changes.
export function DiffTab({ path, staged, narrow }: { path: string; staged: boolean; narrow: boolean }) {
  const host = useRef<HTMLDivElement>(null);
  const [note, setNote] = useState("");

  useEffect(() => {
    let disposed = false;
    let ed: monaco.editor.IStandaloneDiffEditor | null = null;
    const models: monaco.editor.ITextModel[] = [];
    api.gitDiff(path, staged).then(
      (d) => {
        if (disposed || !host.current) return;
        if (d.binary) {
          setNote("Binary file: no text diff to show.");
          return;
        }
        const lang = languageOf(path);
        const orig = monaco.editor.createModel(d.original, lang);
        const mod = monaco.editor.createModel(d.modified, lang);
        models.push(orig, mod);
        ed = monaco.editor.createDiffEditor(host.current, {
          theme: themeName(), automaticLayout: true, readOnly: true, originalEditable: false,
          renderSideBySide: !narrow, useInlineViewWhenSpaceIsLimited: true,
          fontFamily: "'Martian Mono', ui-monospace, monospace", fontSize: 12.5, lineHeight: 21,
          scrollBeyondLastLine: false, minimap: { enabled: false }, renderOverviewRuler: true,
          hideUnchangedRegions: { enabled: true, contextLineCount: 3, minimumLineCount: 5 },
        });
        ed.setModel({ original: orig, modified: mod });
        if (d.original === d.modified) setNote("No differences.");
      },
      (e: Error) => !disposed && setNote(e.message),
    );
    return () => {
      disposed = true;
      ed?.dispose();
      models.forEach((m) => m.dispose());
    };
  }, [path, staged, narrow]);

  return (
    <>
      {note && <p className="notice pad">{note}</p>}
      <div className="monaco-host" ref={host} />
    </>
  );
}
