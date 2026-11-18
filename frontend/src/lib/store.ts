import { create } from "zustand";
import type { store as S, tabs as T, prompt as P } from "./api";

export type ViewOpts = { hex: boolean; timestamp: boolean; json: boolean };

export interface TermTab {
  kind: "terminal";
  id: string; // backend tab id
  sessionId: string;
  title: string;
  type: string;
  state: string;
  message: string;
  describe: string;
  logPath: string;
  sftpOpen: boolean;
  view: ViewOpts;
  color?: string;
}

export interface EditorTab {
  kind: "editor";
  id: string; // "e:" + docId
  docId: string;
  title: string;
  path: string;
  source: "local" | "remote";
  termTabId?: string;
  dirty: boolean;
}

export type Tab = TermTab | EditorTab;

export interface Toast {
  id: number;
  kind: "info" | "error" | "success";
  text: string;
}

export type SidebarView = "sessions" | "sftp" | "tunnels" | "servers";

export interface SessionEditorState {
  session?: S.Session;
  folder?: string;
  type?: string;
}

interface State {
  folders: string[];
  sessions: S.Session[];
  tabs: Tab[];
  activeId: string | null;
  split: { on: boolean; rightId: string | null; focus: "left" | "right" };
  multi: { on: boolean; selected: string[]; mirror: boolean };
  settings: S.Settings | null;
  prompts: P.Request[];
  toasts: Toast[];
  sidebar: SidebarView;
  sidebarOpen: boolean;
  sessionEditor: SessionEditorState | null;
  settingsOpen: boolean;
  importOpen: boolean | "sheet"; // "sheet" opens on the Google Sheets source
  quickOpen: boolean;
  vaultOpen: boolean;

  setTree(folders: string[], sessions: S.Session[]): void;
  addTab(t: Tab, activate?: boolean): void;
  updateTab(id: string, patch: Partial<TermTab> | Partial<EditorTab>): void;
  updateFromInfo(info: T.Info): void;
  removeTab(id: string): void;
  activate(id: string): void;
  setSplit(p: Partial<State["split"]>): void;
  setMulti(p: Partial<State["multi"]>): void;
  toggleSelected(id: string): void;
  setSettings(s: S.Settings): void;
  pushPrompt(p: P.Request): void;
  dropPrompt(id: string): void;
  toast(kind: Toast["kind"], text: string): void;
  dismissToast(id: number): void;
  set<K extends keyof State>(k: K, v: State[K]): void;
}

let toastSeq = 0;

/** Latest backend state per tab, kept even before the tab is added to the
 * store (a fast failure can emit "disconnected" before Open() returns). */
export const lastInfo = new Map<string, T.Info>();

export const useStore = create<State>((set, get) => ({
  folders: [],
  sessions: [],
  tabs: [],
  activeId: null,
  split: { on: false, rightId: null, focus: "left" },
  multi: { on: false, selected: [], mirror: false },
  settings: null,
  prompts: [],
  toasts: [],
  sidebar: "sessions",
  sidebarOpen: true,
  sessionEditor: null,
  settingsOpen: false,
  importOpen: false,
  quickOpen: false,
  vaultOpen: false,

  setTree: (folders, sessions) => set({ folders, sessions }),
  addTab: (t, activate = true) =>
    set((s) => ({ tabs: [...s.tabs, t], activeId: activate ? t.id : s.activeId ?? t.id })),
  updateTab: (id, patch) =>
    set((s) => ({ tabs: s.tabs.map((t) => (t.id === id ? ({ ...t, ...patch } as Tab) : t)) })),
  updateFromInfo: (info) => {
    lastInfo.set(info.id, info);
    set((s) => ({
      tabs: s.tabs.map((t) =>
        t.id === info.id && t.kind === "terminal"
          ? { ...t, state: info.state, message: info.message, describe: info.describe || t.describe, logPath: info.logPath }
          : t,
      ),
    }));
  },
  removeTab: (id) =>
    set((s) => {
      lastInfo.delete(id);
      const idx = s.tabs.findIndex((t) => t.id === id);
      const tabs = s.tabs.filter((t) => t.id !== id);
      let activeId = s.activeId;
      if (activeId === id) activeId = tabs[Math.min(idx, tabs.length - 1)]?.id ?? null;
      const split = { ...s.split };
      if (split.rightId === id) split.rightId = null;
      return {
        tabs,
        activeId,
        split,
        multi: { ...s.multi, selected: s.multi.selected.filter((x) => x !== id) },
      };
    }),
  activate: (id) =>
    set((s) => {
      if (s.split.on && s.split.focus === "right") return { split: { ...s.split, rightId: id } };
      return { activeId: id };
    }),
  setSplit: (p) => set((s) => ({ split: { ...s.split, ...p } })),
  setMulti: (p) => set((s) => ({ multi: { ...s.multi, ...p } })),
  toggleSelected: (id) =>
    set((s) => ({
      multi: {
        ...s.multi,
        selected: s.multi.selected.includes(id) ? s.multi.selected.filter((x) => x !== id) : [...s.multi.selected, id],
      },
    })),
  setSettings: (settings) => set({ settings }),
  pushPrompt: (p) => set((s) => ({ prompts: [...s.prompts, p] })),
  dropPrompt: (id) => set((s) => ({ prompts: s.prompts.filter((p) => p.id !== id) })),
  toast: (kind, text) => {
    const id = ++toastSeq;
    set((s) => ({ toasts: [...s.toasts, { id, kind, text }] }));
    setTimeout(() => get().dismissToast(id), kind === "error" ? 8000 : 4000);
  },
  dismissToast: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
  set: (k, v) => set({ [k]: v } as any),
}));

export const toast = (kind: Toast["kind"], text: string) => useStore.getState().toast(kind, text);
