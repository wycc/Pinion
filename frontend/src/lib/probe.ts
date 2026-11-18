// Reachability of saved sessions, probed by the backend (ProbeService):
// a TCP connect at startup and then every 5 minutes per session.
import { create } from "zustand";
import { ProbeService } from "./services";
import { on } from "./api";

export interface ProbeResult {
  id: string;
  ok: boolean;
  latencyMs: number;
  error?: string;
  addr: string;
  at: number; // unix ms
}

export const useProbe = create<{ r: Record<string, ProbeResult> }>(() => ({ r: {} }));

/** Loads current results and follows updates; returns an unsubscribe function. */
export function initProbe(): () => void {
  ProbeService.Results()
    .then((m) => useProbe.setState({ r: { ...((m ?? {}) as Record<string, ProbeResult>) } }))
    .catch(() => {});
  return on<ProbeResult[]>("probe:results", (batch) =>
    useProbe.setState((s) => {
      const r = { ...s.r };
      for (const x of batch ?? []) if (x) r[x.id] = x;
      return { r };
    }),
  );
}

/** Session types the backend probes (TCP only in client mode). */
export function probeable(s: { type: string; net?: { mode?: string } | null }): boolean {
  return s.type === "ssh" || s.type === "telnet" || (s.type === "tcp" && s.net?.mode !== "server");
}

export function probeNow(ids: string[]) {
  ProbeService.ProbeNow(ids).catch(() => {});
}
