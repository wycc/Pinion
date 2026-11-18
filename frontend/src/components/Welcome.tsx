import { useState } from "react";
import { Plus, Terminal, Download, Zap, FileSpreadsheet, RefreshCw } from "lucide-react";
import { useStore, toast } from "../lib/store";
import { openLocal, openSession } from "../lib/actions";
import { isMac, errMsg } from "../lib/api";
import { ImportService } from "../lib/services";
import { typeIcon } from "./Sidebar";
import { useT } from "../lib/i18n";

export default function Welcome() {
  const t = useT();
  const { sessions, set } = useStore();
  const recent = [...sessions].sort((a, b) => (b.updatedAt > a.updatedAt ? 1 : -1)).slice(0, 8);
  const mod = isMac ? "⌘" : "Ctrl+Shift+";
  const sheetUrl = useStore((s) => s.settings?.sheetUrl);
  const [refreshing, setRefreshing] = useState(false);
  const [summary, setSummary] = useState("");

  // Re-read the imported Google Sheet; only rows that changed are analysed again.
  const refresh = async () => {
    if (!sheetUrl) return set("importOpen", "sheet");
    setRefreshing(true);
    try {
      const r = await ImportService.RefreshSheet();
      if (!r) return;
      let msg = t("welcome.refresh.done", {
        rows: r.diff.rows,
        changed: r.diff.changed,
        method: t(r.method === "ai" ? "welcome.refresh.ai" : "welcome.refresh.rules"),
        added: r.added,
        updated: r.updated,
      });
      if (r.diff.removed) msg += t("welcome.refresh.removed", { n: r.diff.removed });
      if (r.missing) msg += t("welcome.refresh.missing", { n: r.missing });
      if (r.skipped) msg += t("welcome.refresh.skipped", { n: r.skipped });
      setSummary(msg);
      toast("success", msg);
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setRefreshing(false);
    }
  };

  return (
    <div className="flex flex-1 items-center justify-center overflow-auto">
      <div className="w-[520px] max-w-full p-6">
        <div className="mb-1 flex items-center gap-3">
          <img src="/icon.png" className="h-12 w-12" alt="" />
          <div>
            <div className="text-2xl font-semibold">Pinion</div>
            <div className="text-mute">{t("welcome.tagline")}</div>
          </div>
        </div>
        <div className="mt-6 grid grid-cols-2 gap-2">
          <button className="btn justify-start !py-2" onClick={() => set("sessionEditor", {})}>
            <Plus size={16} /> {t("welcome.newSession")} <span className="ml-auto text-mute">{mod}N</span>
          </button>
          <button className="btn justify-start !py-2" onClick={openLocal}>
            <Terminal size={16} /> {t("welcome.localTerminal")} <span className="ml-auto text-mute">{mod}T</span>
          </button>
          <button className="btn justify-start !py-2" onClick={() => set("quickOpen", true)}>
            <Zap size={16} /> {t("welcome.quickConnect")} <span className="ml-auto text-mute">{mod}K</span>
          </button>
          <button className="btn justify-start !py-2" onClick={() => set("importOpen", true)}>
            <Download size={16} /> {t("welcome.import")}
          </button>
          <button className="btn justify-start !py-2" onClick={() => set("importOpen", "sheet")}>
            <FileSpreadsheet size={16} /> {t("welcome.sheet")}
          </button>
          <button
            className="btn justify-start !py-2"
            disabled={refreshing}
            title={sheetUrl ? t("welcome.refresh.title", { url: sheetUrl }) : t("welcome.refresh.none")}
            onClick={refresh}
          >
            <RefreshCw size={16} className={refreshing ? "animate-spin" : ""} />
            {refreshing ? t("welcome.refresh.busy") : t("welcome.refresh")}
          </button>
        </div>
        {summary && <div className="mt-2 text-[13px] text-mute">{summary}</div>}
        {recent.length > 0 && (
          <>
            <div className="mb-1 mt-6 text-mute">{t("welcome.recent")}</div>
            <div className="rounded border border-line">
              {recent.map((s) => (
                <button
                  key={s.id}
                  className="flex w-full items-center gap-2 border-b border-line px-3 py-1.5 text-left last:border-0 hover:bg-hover"
                  onClick={() => openSession(s.id)}
                >
                  {typeIcon(s.type)}
                  <span className="truncate">{s.name}</span>
                  <span className="ml-auto truncate text-[13px] text-mute">{s.folder}</span>
                </button>
              ))}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
