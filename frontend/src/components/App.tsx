import { useEffect } from "react";
import { on, SystemService, errMsg, isMac, type prompt as P, type tabs as T } from "../lib/api";
import { useStore, toast } from "../lib/store";
import { refreshTree, loadSettings, applyTheme, openLocal, closeTab } from "../lib/actions";
import { terminals } from "../lib/terminals";
import { initTransfers } from "../lib/transfers";
import { initProbe } from "../lib/probe";
import Toolbar from "./Toolbar";
import Sidebar from "./Sidebar";
import Workspace from "./Workspace";
import StatusBar from "./StatusBar";
import PromptDialog from "./dialogs/PromptDialog";
import SessionEditor from "./dialogs/SessionEditor";
import SettingsDialog from "./dialogs/SettingsDialog";
import ImportDialog from "./dialogs/ImportDialog";
import QuickConnect from "./dialogs/QuickConnect";
import VaultDialog from "./dialogs/VaultDialog";
import { AskHost, ContextMenuHost } from "./ui";
import Toasts from "./Toasts";
import MultiExecBar from "./MultiExecBar";

export default function App() {
  const st = useStore();

  useEffect(() => {
    initTransfers();
    loadSettings().catch((e) => toast("error", errMsg(e)));
    refreshTree().catch((e) => toast("error", errMsg(e)));
    const offs = [
      on<T.Info>("tab:state", (info) => useStore.getState().updateFromInfo(info)),
      on<{ id: string; text: string }>("tab:notice", (n) => terminals.get(n.id)?.notice(n.text)),
      on<P.Request>("prompt", (p) => useStore.getState().pushPrompt(p)),
      on<string>("prompt:cancel", (id) => useStore.getState().dropPrompt(id)),
      on("sessions:changed", () => refreshTree()),
      initProbe(),
      on<any>("settings:changed", (s) => {
        useStore.getState().setSettings(s);
        applyTheme();
      }),
    ];
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const mqh = () => applyTheme();
    mq.addEventListener("change", mqh);
    return () => {
      offs.forEach((f) => f());
      mq.removeEventListener("change", mqh);
    };
  }, []);

  // global shortcuts: Cmd (mac) or Ctrl+Shift (win/linux)
  useEffect(() => {
    const h = (e: KeyboardEvent) => {
      const mod = isMac ? e.metaKey && !e.ctrlKey : e.ctrlKey && e.shiftKey;
      const s = useStore.getState();
      if (e.ctrlKey && e.key === "Tab") {
        e.preventDefault();
        const i = s.tabs.findIndex((t) => t.id === s.activeId);
        const n = s.tabs.length;
        if (n) s.activate(s.tabs[(i + (e.shiftKey ? n - 1 : 1)) % n].id);
        return;
      }
      if (!mod) return;
      const k = e.key.toLowerCase();
      if (k === "t") {
        e.preventDefault();
        openLocal();
      } else if (k === "w") {
        e.preventDefault();
        if (s.activeId) closeTab(s.activeId);
      } else if (k === "n") {
        e.preventDefault();
        s.set("sessionEditor", {});
      } else if (k === "k") {
        e.preventDefault();
        s.set("quickOpen", true);
      } else if (k === "e") {
        // jump to the SFTP file list (Esc returns to the terminal)
        e.preventDefault();
        s.set("sidebarOpen", true);
        s.set("sidebar", "sftp");
        setTimeout(() => document.getElementById("sftp-list")?.focus(), 50);
      } else if (k === "b") {
        e.preventDefault();
        s.set("sidebarOpen", !s.sidebarOpen);
      } else if (k === "," || e.code === "Comma") {
        e.preventDefault();
        s.set("settingsOpen", true);
      } else if (/^Digit[1-9]$/.test(e.code)) {
        // use the physical key: with Shift held, e.key is "!", "@", …
        const t = s.tabs[Number(e.code.slice(5)) - 1];
        if (t) {
          e.preventDefault();
          s.activate(t.id);
        }
      }
    };
    window.addEventListener("keydown", h);
    return () => window.removeEventListener("keydown", h);
  }, []);

  useEffect(() => {
    SystemService.Info().then((i) => {
      if (i.vaultLocked) useStore.getState().set("vaultOpen", true);
    });
  }, []);

  return (
    <div className="flex h-full flex-col">
      <Toolbar />
      <div className="flex min-h-0 flex-1">
        {st.sidebarOpen && <Sidebar />}
        <div className="flex min-w-0 flex-1 flex-col">
          <Workspace />
          {st.multi.on && <MultiExecBar />}
        </div>
      </div>
      <StatusBar />
      {st.prompts.length > 0 && <PromptDialog req={st.prompts[0]} />}
      {st.sessionEditor && <SessionEditor init={st.sessionEditor} />}
      {st.importOpen && <ImportDialog />}
      {/* after ImportDialog so "open settings" from the import dialog shows on top */}
      {st.settingsOpen && <SettingsDialog />}
      {st.quickOpen && <QuickConnect />}
      <VaultDialog />
      <ContextMenuHost />
      <AskHost />
      <Toasts />
    </div>
  );
}
