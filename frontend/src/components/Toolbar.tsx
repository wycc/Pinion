import { useState } from "react";
import {
  Plus, Terminal, FileText, Columns2, Radio, Download, FileSpreadsheet, Settings, PanelLeft, Zap,
} from "lucide-react";
import { useStore } from "../lib/store";
import { openLocal, openAdhoc, parseQuick } from "../lib/actions";
import { openLocalFile } from "../lib/editor";
import { isMac } from "../lib/api";
import { useT } from "../lib/i18n";

export default function Toolbar() {
  const t = useT();
  const st = useStore();
  const [quick, setQuick] = useState("");
  const go = () => {
    const s = parseQuick(quick);
    if (s) {
      openAdhoc(s);
      setQuick("");
    }
  };
  const mod = isMac ? "⌘" : "Ctrl+Shift+";
  return (
    <div
      className={"toolbar flex shrink-0 items-center gap-1 border-b border-line bg-side px-2 " + (isMac ? "h-[52px]" : "h-10")}
      style={{ paddingLeft: isMac ? 88 : 8, ["--wails-draggable" as any]: "drag" }}
    >
      <button className="icon-btn" title={t("toolbar.sidebar", { key: mod + "B" })} onClick={() => st.set("sidebarOpen", !st.sidebarOpen)} style={{ ["--wails-draggable" as any]: "no-drag" }}>
        <PanelLeft size={17} />
      </button>
      <div className="mx-1 h-5 border-l border-line" />
      <div className="flex items-center gap-1" style={{ ["--wails-draggable" as any]: "no-drag" }}>
        <button className="btn" title={t("toolbar.newSession", { key: mod + "N" })} onClick={() => st.set("sessionEditor", {})}>
          <Plus size={15} /> Session
        </button>
        <button className="btn" title={t("toolbar.localTitle", { key: mod + "T" })} onClick={openLocal}>
          <Terminal size={15} /> {t("toolbar.local")}
        </button>
        <button className="btn" title={t("toolbar.openFileTitle")} onClick={() => openLocalFile()}>
          <FileText size={15} /> {t("toolbar.openFile")}
        </button>
        <div className="mx-1 h-5 border-l border-line" />
        <button
          className={"btn " + (st.split.on ? "!border-accent text-accent" : "")}
          title={t("toolbar.splitTitle")}
          onClick={() => st.setSplit({ on: !st.split.on, focus: "left" })}
        >
          <Columns2 size={15} /> {t("toolbar.split")}
        </button>
        <button
          className={"btn " + (st.multi.on ? "!border-accent text-accent" : "")}
          title={t("toolbar.multiExecTitle")}
          onClick={() => st.setMulti({ on: !st.multi.on })}
        >
          <Radio size={15} /> MultiExec
        </button>
        <button className="btn" title={t("toolbar.importTitle")} onClick={() => st.set("importOpen", true)}>
          <Download size={15} /> {t("toolbar.import")}
        </button>
        <button className="btn" title={t("toolbar.sheetTitle")} onClick={() => st.set("importOpen", "sheet")}>
          <FileSpreadsheet size={15} /> <span className="max-[1100px]:hidden">{t("toolbar.sheet")}</span>
        </button>
      </div>
      <div className="flex-1" />
      <div className="flex items-center gap-1" style={{ ["--wails-draggable" as any]: "no-drag" }}>
        <div className="relative">
          <Zap size={14} className="absolute left-2 top-1.5 text-mute" />
          <input
            className="input w-64 pl-7"
            placeholder={t("toolbar.quickPlaceholder")}
            value={quick}
            onChange={(e) => setQuick(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && go()}
            title={t("toolbar.quickTitle")}
          />
        </div>
        <button className="icon-btn" title={t("toolbar.settings", { key: mod + "," })} onClick={() => st.set("settingsOpen", true)}>
          <Settings size={17} />
        </button>
      </div>
    </div>
  );
}
