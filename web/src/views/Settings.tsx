import { useEffect, useState } from "react";
import { api, type LLMInput, type SettingsInfo } from "../api";
import { setThemePref, themePref, type ThemePref } from "../theme";

// Settings: the model connection, appearance, folders and how KuliDev was started.
// Changes apply at once; they can only be made on the computer running KuliDev.
export function SettingsView({ onOpenFolder, onRescan, onSaved, root }: {
  onOpenFolder: () => void; onRescan: () => void; onSaved: () => void; root?: string;
}) {
  const [info, setInfo] = useState<SettingsInfo | null>(null);
  const [error, setError] = useState("");
  const [form, setForm] = useState<LLMInput>({ baseURL: "", model: "", reasoningEffort: "", maxTokens: 0 });
  const [key, setKey] = useState("");
  const [note, setNote] = useState<{ text: string; warn?: boolean } | null>(null);
  const [busy, setBusy] = useState<"" | "test" | "save">("");
  const [theme, setTheme] = useState<ThemePref>(themePref);

  const load = () =>
    api.settings().then((s) => {
      setInfo(s);
      setForm({ baseURL: s.llm.baseURL, model: s.llm.model, reasoningEffort: s.llm.reasoningEffort ?? "", maxTokens: s.llm.maxTokens ?? 0 });
    }, (e: Error) => setError(e.message));
  useEffect(() => { load(); }, []);

  const input = (clearKey = false): LLMInput => ({ ...form, apiKey: key || undefined, clearKey });
  async function test() {
    setBusy("test");
    setNote(null);
    try {
      const r = await api.testSettings(input());
      setNote(r.ok ? { text: `Connected: the model answered “${r.reply}” in ${(r.ms / 1000).toFixed(1)} s.` } : { text: r.error ?? "No answer.", warn: true });
    } catch (e) {
      setNote({ text: (e as Error).message, warn: true });
    } finally {
      setBusy("");
    }
  }
  async function save(clearKey = false) {
    setBusy("save");
    setNote(null);
    try {
      const r = await api.saveSettings(input(clearKey));
      setKey("");
      onSaved();
      setNote({ text: r.activeModel ? `Saved. Ask and DocsMana now use ${r.activeModel}.` : "Saved. No model is set, so Ask and DocsMana are off." });
      load();
    } catch (e) {
      setNote({ text: (e as Error).message, warn: true });
    } finally {
      setBusy("");
    }
  }
  const field = (k: keyof LLMInput) => ({
    value: k === "maxTokens" && !form.maxTokens ? "" : ((form[k] as string | number | undefined) ?? ""),
    onChange: (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
      setForm((f) => ({ ...f, [k]: k === "maxTokens" ? Number(e.target.value) || 0 : e.target.value })),
  });

  return (
    <div className="page settings-page">
      <h1 className="title">Settings</h1>

      <section className="set-section">
        <h2>AI model</h2>
        <p className="muted small">
          Ask and DocsMana use any OpenAI-compatible endpoint: Ollama, LM Studio, vLLM, llama.cpp, or a hosted API. Code excerpts
          from this folder are sent to it when you use them, so pick an endpoint you trust with your code.
        </p>
        {error ? (
          <p className="notice warn">{error.includes("no such endpoint") || error.includes("Not Found")
            ? "Settings can only be changed on the computer running KuliDev." : error}</p>
        ) : (
          <form className="set-form" onSubmit={(e) => { e.preventDefault(); save(); }}>
            <label>Base URL
              <input {...field("baseURL")} placeholder="http://localhost:11434/v1" spellCheck={false} autoComplete="off" />
            </label>
            <label>Model
              <input {...field("model")} placeholder="qwen3:8b" spellCheck={false} autoComplete="off" />
            </label>
            <label>API key
              <input type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="new-password"
                placeholder={info?.llm.hasKey ? "Saved; type to replace it" : "Only if the endpoint needs one"} />
            </label>
            <label>Reasoning effort
              <select {...field("reasoningEffort")}>
                <option value="">Model default</option>
                <option value="none">None (fastest; for Qwen3-style thinking models)</option>
                <option value="low">Low</option>
                <option value="medium">Medium</option>
                <option value="high">High</option>
              </select>
            </label>
            <label>Max tokens per reply
              <input type="number" min={0} max={200000} step={256} {...field("maxTokens")} placeholder="2048" />
            </label>
            <div className="set-actions">
              <button type="button" className="btn" disabled={!!busy || !form.baseURL} onClick={test}>{busy === "test" ? "Testing…" : "Test connection"}</button>
              <button className="btn primary" disabled={!!busy}>{busy === "save" ? "Saving…" : "Save"}</button>
              {info?.llm.hasKey && <button type="button" className="btn" disabled={!!busy} onClick={() => save(true)}>Remove saved key</button>}
            </div>
            {note && <p className={`notice ${note.warn ? "warn" : ""}`} role="status">{note.text}</p>}
            {info && (
              <p className="muted small">
                In use now: {info.activeModel || "none"}.
                {info.fromStartup && " KuliDev was started with a model on the command line or in KULI_LLM_* variables; saving here replaces it until the next start."}
              </p>
            )}
          </form>
        )}
      </section>

      <section className="set-section">
        <h2>Appearance</h2>
        <div className="seg" role="radiogroup" aria-label="Theme">
          {(["system", "light", "dark"] as ThemePref[]).map((t) => (
            <button key={t} role="radio" aria-checked={theme === t} className={theme === t ? "on" : ""}
              onClick={() => { setTheme(t); setThemePref(t); }}>
              {t === "system" ? "Match system" : t === "light" ? "Light" : "Dark"}
            </button>
          ))}
        </div>
      </section>

      <section className="set-section">
        <h2>Folder</h2>
        {root && <p className="small mono-path">{root}</p>}
        <div className="set-actions">
          <button className="btn" onClick={onOpenFolder}>Open another folder</button>
          <button className="btn" onClick={onRescan}>Rescan this folder</button>
        </div>
        <p className="muted small">Ctrl+O opens a folder, Ctrl+R switches to a recent one.</p>
      </section>

      {info && (
        <section className="set-section">
          <h2>How KuliDev is running</h2>
          <ul className="plain set-facts">
            <li><span>Version</span>{info.version || "dev"}</li>
            <li><span>Editing</span>{info.readOnly ? "Read-only (started with --read-only)" : "Files can be edited and saved"}</li>
            <li><span>Terminal and agents</span>{info.terminals ? "On" : "Off (Windows, --no-terminal or --read-only)"}</li>
            <li><span>Settings file</span><code>{info.settingsFile}</code></li>
            <li><span>Recent folders</span><code>{info.recentFile}</code></li>
          </ul>
          <p className="muted small">Access from other devices (phone, tunnel) is set when starting <code>kuli serve</code>; see Devices to pair or revoke them.</p>
        </section>
      )}
    </div>
  );
}
