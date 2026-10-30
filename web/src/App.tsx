import { useEffect, useState } from "react";
import { api } from "./api";
import { PairScreen } from "./workbench/Pair";
import { Workbench } from "./workbench/Workbench";

// Remote workspaces require every device to be paired; local ones go straight in.
export function App() {
  const [state, setState] = useState<"checking" | "pair" | "ok">("checking");
  useEffect(() => {
    const check = () =>
      api.authState().then(
        (s) => setState(s.required && !s.paired ? "pair" : "ok"),
        () => setState("ok"),
      );
    check();
    const onAuth = () => setState("pair");
    addEventListener("kuli-auth-required", onAuth);
    return () => removeEventListener("kuli-auth-required", onAuth);
  }, []);
  if (state === "checking") return null;
  if (state === "pair") return <PairScreen onPaired={() => { location.hash = "#/"; location.reload(); }} />;
  return <Workbench />;
}
