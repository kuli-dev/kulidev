import { useEffect, useState } from "react";

export type Loadable<T> = { data?: T; error?: string; loading: boolean };

// useLoad runs fn whenever deps change and ignores results from stale runs.
export function useLoad<T>(fn: () => Promise<T>, deps: unknown[]): Loadable<T> & { reload: () => void } {
  const [state, setState] = useState<Loadable<T>>({ loading: true });
  const [tick, setTick] = useState(0);
  useEffect(() => {
    let live = true;
    setState((s) => ({ ...s, loading: true, error: undefined }));
    fn().then(
      (data) => live && setState({ data, loading: false }),
      (e: Error) => live && setState({ error: e.message, loading: false }),
    );
    return () => {
      live = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, tick]);
  return { ...state, reload: () => setTick((t) => t + 1) };
}

export const fmt = (n: number) => n.toLocaleString("en-US");
