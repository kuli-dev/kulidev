import qrcode from "qrcode-generator";
import { useEffect, useState } from "react";
import { api, type Device } from "../api";
import { useLoad } from "../hooks";

function guessName(): string {
  const ua = navigator.userAgent;
  const os = /iPhone/.test(ua) ? "iPhone" : /iPad/.test(ua) ? "iPad" : /Android/.test(ua) ? "Android"
    : /Mac OS X/.test(ua) ? "Mac" : /Windows/.test(ua) ? "Windows" : /Linux/.test(ua) ? "Linux" : "Device";
  const browser = /Edg\//.test(ua) ? "Edge" : /Chrome\//.test(ua) ? "Chrome" : /Firefox\//.test(ua) ? "Firefox" : /Safari\//.test(ua) ? "Safari" : "browser";
  return `${os} ${browser}`;
}

function codeFromHash(): string {
  const m = location.hash.match(/[?&]code=([A-Za-z0-9-]+)/);
  return m ? m[1] : "";
}

export function PairScreen({ onPaired }: { onPaired: () => void }) {
  const [code, setCode] = useState(codeFromHash);
  const [name, setName] = useState(guessName);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.pair(code, name);
      onPaired();
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  }

  return (
    <main className="pair">
      <form className="pair-card" onSubmit={submit}>
        <p className="wordmark-big">Kuli<span>Dev</span></p>
        <h1>Pair this device</h1>
        <p className="muted">
          This workspace only opens on paired devices. Use the code printed by <code>kuli serve</code>, or one made in
          the Devices view on a device that is already paired.
        </p>
        <label>
          Pairing code
          <input value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} placeholder="ABCD-EFGH"
            autoComplete="one-time-code" autoCapitalize="characters" spellCheck={false} required autoFocus={!code} />
        </label>
        <label>
          Name this device
          <input value={name} onChange={(e) => setName(e.target.value)} maxLength={60} required />
        </label>
        {error && <p className="notice warn" role="alert">{error}</p>}
        <button className="btn primary" disabled={busy || !code.trim()}>{busy ? "Pairing…" : "Pair this device"}</button>
      </form>
    </main>
  );
}

// Devices view: who can open this workspace, and inviting one more.
export function DevicesPane() {
  const d = useLoad(api.devices, []);
  const [invite, setInvite] = useState<{ code: string; url: string; svg: string; until: number } | null>(null);
  const [now, setNow] = useState(Date.now());
  const [error, setError] = useState("");

  useEffect(() => {
    if (!invite) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [invite]);

  async function makeInvite() {
    setError("");
    try {
      const r = await api.newPairingCode();
      const url = `${location.origin}${location.pathname}#/pair?code=${r.code}`;
      const qr = qrcode(0, "M");
      qr.addData(url);
      qr.make();
      setInvite({ code: r.code, url, svg: qr.createSvgTag({ cellSize: 4, margin: 2, scalable: true }), until: Date.now() + r.expiresIn * 1000 });
    } catch (e) {
      setError((e as Error).message);
    }
  }

  async function revoke(dev: Device, isMe: boolean) {
    if (isMe) {
      await api.logout().catch(() => {});
      location.reload();
      return;
    }
    await api.revokeDevice(dev.id).catch((e: Error) => setError(e.message));
    d.reload();
  }

  if (d.data && !d.data.required) {
    return (
      <div className="pane">
        <h2 className="pane-title">Devices</h2>
        <p className="muted small pad-x">
          This workspace is local-only, so there is nothing to pair. To use it from your phone or another computer,
          run <code>kuli serve --addr 0.0.0.0:7878 --tls-self-signed</code>.
        </p>
      </div>
    );
  }
  const left = invite ? Math.max(0, Math.round((invite.until - now) / 1000)) : 0;
  return (
    <div className="pane">
      <h2 className="pane-title">Devices</h2>
      {error && <p className="notice warn pad">{error}</p>}
      <ul className="plain devices">
        {d.data?.devices.map((dev) => {
          const isMe = dev.id === d.data?.current;
          return (
            <li key={dev.id}>
              <div className="agent-row">
                <span className="agent-name">{dev.name}{isMe && <span className="muted small"> (this device)</span>}</span>
                <button className="btn" onClick={() => revoke(dev, isMe)}>{isMe ? "Sign out" : "Revoke"}</button>
              </div>
              <span className="small muted">Paired {new Date(dev.created).toLocaleDateString()}, last seen {new Date(dev.lastSeen).toLocaleString()}</span>
            </li>
          );
        })}
      </ul>
      <div className="pad-x invite">
        {!invite || left === 0 ? (
          <button className="btn" onClick={makeInvite}>Pair another device</button>
        ) : (
          <>
            <p className="small">Scan with the other device, or open the link there. Works once, for {Math.floor(left / 60)}:{String(left % 60).padStart(2, "0")} more.</p>
            <div className="qr" dangerouslySetInnerHTML={{ __html: invite.svg }} aria-label={`QR code for pairing code ${invite.code}`} role="img" />
            <p className="pair-code">{invite.code}</p>
            <p className="small muted break">{invite.url}</p>
          </>
        )}
      </div>
    </div>
  );
}
