import type { TermInfo } from "../api";

// An agent counts as "waiting" once it has printed nothing for this long while still
// running: coding agents stop and wait for an answer, an approval, or the next task.
export const IDLE_MS = 15_000;

export type AgentState = "working" | "waiting" | "exited";

export function stateOf(t: TermInfo, now = Date.now()): AgentState {
  if (t.exited) return "exited";
  return now - new Date(t.lastOutput).getTime() >= IDLE_MS ? "waiting" : "working";
}

export function ago(iso: string, now = Date.now()): string {
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${Math.floor(s / 3600)}h ${Math.floor((s % 3600) / 60)}m`;
}

const PREF = "kuli.notify";

export function notificationsWanted(): boolean {
  try {
    return localStorage.getItem(PREF) === "1" && "Notification" in window && Notification.permission === "granted";
  } catch {
    return false;
  }
}

// enableNotifications asks for permission (needs a tap) and registers the worker that
// Android needs to show them. Notifications need https or localhost.
export async function enableNotifications(): Promise<string> {
  if (!("Notification" in window) || !("serviceWorker" in navigator)) return "This browser can't show notifications.";
  if (!isSecureContext) return "Notifications need https (for example through --public-url or --tls-self-signed).";
  const p = await Notification.requestPermission();
  if (p !== "granted") return "Notifications were not allowed.";
  await navigator.serviceWorker.register("sw.js");
  try { localStorage.setItem(PREF, "1"); } catch { /* storage blocked */ }
  return "";
}

export function disableNotifications() {
  try { localStorage.removeItem(PREF); } catch { /* storage blocked */ }
}

export async function notify(title: string, body: string, tag: string) {
  try {
    const reg = await navigator.serviceWorker.getRegistration();
    if (reg) await reg.showNotification(title, { body, tag, icon: undefined });
    else new Notification(title, { body, tag });
  } catch { /* best effort */ }
}
