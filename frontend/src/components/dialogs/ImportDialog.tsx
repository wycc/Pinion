import { useEffect, useState } from "react";
import { Download, FolderOpen, Sparkles } from "lucide-react";
import { ImportService } from "../../lib/services";
import { SystemService, errMsg } from "../../lib/api";
import { useStore, toast } from "../../lib/store";
import { Modal, Field, Check } from "../ui";
import { typeIcon } from "../Sidebar";
import { useT } from "../../lib/i18n";

type Source = "file" | "sheet";

export default function ImportDialog() {
  const t = useT();
  const set = useStore((s) => s.set);
  const settings = useStore((s) => s.settings);
  const openedFor = useStore((s) => s.importOpen);
  const [source, setSource] = useState<Source>(() => (openedFor === "sheet" || settings?.sheetUrl ? "sheet" : "file"));
  const [res, setRes] = useState<any>(null);
  const [busy, setBusy] = useState(false);

  // file source
  const [paths, setPaths] = useState<string[]>([]);
  const [path, setPath] = useState("");
  const [fileFolder, setFileFolder] = useState(() => t("importDialog.defaultFolder"));

  // Google Sheets source (remembered in settings)
  const [url, setUrl] = useState(settings?.sheetUrl ?? "");
  const [defHost, setDefHost] = useState(settings?.sheetDefaultHost ?? "");
  const [defUser, setDefUser] = useState(settings?.sheetDefaultUser ?? "");
  const [sheetFolder, setSheetFolder] = useState(() => settings?.sheetFolder || t("importDialog.sheet.defaultFolder"));
  const [hasKey, setHasKey] = useState(false);
  const [vaultLocked, setVaultLocked] = useState(false);
  const provider = settings?.aiProvider === "openrouter" ? "openrouter" : "anthropic";
  const settingsOpen = useStore((s) => s.settingsOpen);
  const vaultOpen = useStore((s) => s.vaultOpen);
  const [useAI, setUseAI] = useState(!!settings?.sheetUseAI);
  const [method, setMethod] = useState("");
  const [diff, setDiff] = useState<{ rows: number; changed: number } | null>(null);

  const folder = source === "file" ? fileFolder : sheetFolder;
  const setFolder = source === "file" ? setFileFolder : setSheetFolder;

  useEffect(() => {
    ImportService.DetectPaths().then((p) => {
      const list = (p ?? []) as string[];
      setPaths(list);
      if (list[0] && source === "file") previewFile(list[0]);
    });
  }, []);

  // Re-check the key whenever the AI service changes or the settings / vault
  // dialogs close (the user may have just entered a key or unlocked the vault).
  useEffect(() => {
    if (settingsOpen || vaultOpen) return;
    Promise.all([ImportService.HasAIKey(provider), SystemService.Info()]).then(([k, info]) => {
      setHasKey(k);
      setVaultLocked(!k && !!info?.vaultLocked);
    });
  }, [provider, settingsOpen, vaultOpen]);

  const switchSource = (s: Source) => {
    setSource(s);
    setRes(null);
    if (s === "file" && path) previewFile(path);
  };

  const previewFile = async (p: string) => {
    setPath(p);
    setRes(null);
    try {
      setRes(await ImportService.Preview(p));
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  const pick = async () => {
    const p = await ImportService.PickFile();
    if (p) previewFile(p);
  };

  const loadSheet = async () => {
    setBusy(true);
    setRes(null);
    try {
      const p = await ImportService.PreviewSheet({
        url, defaultHost: defHost, defaultUser: defUser, folder: sheetFolder, useAI,
      });
      setRes(p?.result ?? null);
      setMethod(p?.method ?? "");
      setDiff(p?.diff ?? null);
      // the backend remembered this request; keep the settings snapshot in sync
      SystemService.Settings().then((s) => useStore.getState().setSettings(s));
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setBusy(false);
    }
  };

  const commit = async () => {
    setBusy(true);
    try {
      if (source === "file") {
        const n = await ImportService.Commit(path, folder);
        toast("success", t("importDialog.imported", { n }));
      } else {
        const c = await ImportService.CommitSessions(sessions, res?.folders ?? [], folder);
        toast("success", t("importDialog.sheet.done", { added: c.added, updated: c.updated }));
      }
      set("importOpen", false);
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setBusy(false);
    }
  };

  const sessions = (res?.sessions ?? []).filter(Boolean);
  const skipped = (res?.skipped ?? []).filter(Boolean);
  return (
    <Modal
      title={t("importDialog.title")}
      width={720}
      onClose={() => set("importOpen", false)}
      footer={
        <>
          <button className="btn" onClick={() => set("importOpen", false)}>{t("common.cancel")}</button>
          <button className="btn-primary" disabled={!sessions.length || busy} onClick={commit}>
            {t("importDialog.importN", { n: sessions.length })}
          </button>
        </>
      }
    >
      <div className="mb-3 flex gap-1">
        {(["file", "sheet"] as const).map((s) => (
          <button key={s} className={"rounded px-2 py-1 " + (source === s ? "bg-sel" : "hover:bg-hover")} onClick={() => switchSource(s)}>
            {t(s === "file" ? "importDialog.tab.file" : "importDialog.tab.sheet")}
          </button>
        ))}
      </div>

      {source === "file" ? (
        <>
          <p className="mb-3 text-mute">{t("importDialog.intro")}</p>
          <Field label={t("importDialog.source")}>
            <div className="flex gap-1">
              <select className="input" value={path} onChange={(e) => previewFile(e.target.value)}>
                {!path && <option value="">{t("importDialog.notSelected")}</option>}
                {[...new Set([path, ...paths].filter(Boolean))].map((p) => (
                  <option key={p} value={p}>{p}</option>
                ))}
              </select>
              <button className="btn" onClick={pick}><FolderOpen size={14} /> {t("importDialog.choose")}</button>
            </div>
          </Field>
        </>
      ) : (
        <>
          <p className="mb-3 text-mute">{t("importDialog.sheet.intro")}</p>
          <Field label={t("importDialog.sheet.url")}>
            <div className="flex gap-1">
              <input
                className="input"
                value={url}
                placeholder="https://docs.google.com/spreadsheets/d/…"
                onChange={(e) => setUrl(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && url.trim() && !busy && loadSheet()}
              />
              <button className="btn" disabled={!url.trim() || busy} onClick={loadSheet}>
                <Download size={14} /> {busy ? t("importDialog.sheet.loading") : t("importDialog.sheet.load")}
              </button>
            </div>
          </Field>
          <div className="mt-3 grid grid-cols-2 gap-3">
            <Field label={t("importDialog.sheet.defaultHost")} hint={t("importDialog.sheet.defaultHostHint")}>
              <input className="input font-mono" value={defHost} placeholder="relay.example.com" onChange={(e) => setDefHost(e.target.value)} />
            </Field>
            <Field label={t("importDialog.sheet.defaultUser")}>
              <input className="input font-mono" value={defUser} onChange={(e) => setDefUser(e.target.value)} />
            </Field>
          </div>
          <div className="mt-2">
            <Check checked={useAI} onChange={setUseAI}>
              <Sparkles size={13} className="mr-1 inline" />
              {t("importDialog.sheet.useAI")}
            </Check>
            {useAI && (
              <div className="ml-6 text-[12px]">
                {hasKey ? (
                  <span className="text-mute">{t("importDialog.sheet.aiVia", { provider: t(`importDialog.sheet.provider.${provider}`) })}</span>
                ) : vaultLocked ? (
                  <span className="text-warn">
                    {t("importDialog.sheet.vaultLocked")}{" "}
                    <button className="text-accent underline" onClick={() => set("vaultOpen", true)}>{t("importDialog.sheet.unlock")}</button>
                  </span>
                ) : (
                  <span className="text-warn">
                    {t("importDialog.sheet.noKeyFor", { provider: t(`importDialog.sheet.provider.${provider}`) })}{" "}
                    <button className="text-accent underline" onClick={() => set("settingsOpen", true)}>{t("importDialog.sheet.openSettings")}</button>
                  </span>
                )}
              </div>
            )}
          </div>
        </>
      )}

      <Field label={t("importDialog.targetFolder")} className="mt-3">
        <input className="input" value={folder} onChange={(e) => setFolder(e.target.value)} />
      </Field>
      {res && (
        <div className="mt-3">
          <div className="mb-1 text-[13px] text-mute">
            {source === "file"
              ? t("importDialog.summary", { encoding: res.encoding, n: sessions.length, skipped: skipped.length })
              : t("importDialog.sheet.summary", {
                  method: t(method === "ai" ? "importDialog.sheet.methodAI" : "importDialog.sheet.methodRules"),
                  n: sessions.length,
                  skipped: skipped.length,
                }) + (diff ? t("importDialog.sheet.diff", { rows: diff.rows, changed: diff.changed }) : "")}
          </div>
          {(res.warnings ?? []).map((w: string, i: number) => (
            <div key={i} className="mb-1 text-[13px] text-warn">⚠ {w}</div>
          ))}
          <div className="max-h-64 overflow-auto rounded border border-line text-[13px]">
            {sessions.map((s: any, i: number) => (
              <div key={i} className="flex items-center gap-2 border-b border-line px-2 py-1 last:border-0">
                {typeIcon(s.type)}
                <span>{s.name}</span>
                {source === "sheet" && s.ssh && (
                  <span className="font-mono text-mute">
                    {s.ssh.user ? s.ssh.user + "@" : ""}{s.ssh.host}:{s.ssh.port}
                  </span>
                )}
                <span className="ml-auto text-mute">{[folder, s.folder].filter(Boolean).join("/")}</span>
              </div>
            ))}
            {skipped.map((s: any, i: number) => (
              <div key={"k" + i} className="flex items-center gap-2 border-b border-line px-2 py-1 text-mute last:border-0">
                <span className="line-through">{s.name}</span>
                <span className="ml-auto">{s.reason}</span>
              </div>
            ))}
          </div>
          {source === "sheet" && sessions.length > 0 && (
            <div className="mt-1 text-[12px] text-mute">{t("importDialog.sheet.updateNote")}</div>
          )}
        </div>
      )}
    </Modal>
  );
}
