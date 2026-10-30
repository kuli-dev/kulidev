import { useCallback, useEffect, useState } from "react";
import { api } from "./api";
import { Launcher } from "./workbench/Launcher";
import { PairScreen } from "./workbench/Pair";
import { Workbench } from "./workbench/Workbench";

// Local app with no folder open → the launcher. Remote workspaces require pairing.
export function App() {
  const [state, setState] = useState<"checking" | "launcher" | "pair" | "ok">("checking");
  useEffect(() => {
    const check = async () => {
      try {
        const ws = await api.workspace();
        if (ws.canOpen && !ws.open) {
          setState("launcher");
          return;
        }
      } catch {
        /* a fixed (remote) workspace has no launcher API; carry on */
      }
      try {
        const s = await api.authState();
        setState(s.required && !s.paired ? "pair" : "ok");
      } catch {
        setState("ok");
      }
    };
    check();
    const onAuth = () => setState("pair");
    const onNoWorkspace = () => setState("launcher");
    addEventListener("kuli-auth-required", onAuth);
    addEventListener("kuli-no-workspace", onNoWorkspace);
    return () => {
      removeEventListener("kuli-auth-required", onAuth);
      removeEventListener("kuli-no-workspace", onNoWorkspace);
    };
  }, []);
  const enter = () => { location.hash = "#/"; location.reload(); };
  const opened = useCallback(() => { location.hash = "#/"; setState("ok"); }, []);
  if (state === "checking") return null;
  if (state === "launcher") return <Launcher onOpened={opened} />;
  if (state === "pair") return <PairScreen onPaired={enter} />;
  return <Workbench />;
}
