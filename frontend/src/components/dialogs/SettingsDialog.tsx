import { useEffect, useMemo, useState } from "react";
import { FolderOpen, Trash2, Upload } from "lucide-react";
import { SystemService, errMsg, type store as S } from "../../lib/api";
import { EditorService, ImportService } from "../../lib/services";
import { useStore, toast } from "../../lib/store";
import { applyTheme } from "../../lib/actions";
import { Modal, Field, Check, confirmDialog } from "../ui";
import { useT } from "../../lib/i18n";
import {
  installedMonoFonts, fontChain, pickedFont, resolveFont, fontAvailable, parseFamilies,
  importFont, removeFont, useUserFonts, userFamilies, type FontKind,
} from "../../lib/fonts";

interface KH {
  line: number;
  hosts: string;
  keyType: string;
  fingerprint: string;
}

export default function SettingsDialog() {
  const t = useT();
  const set = useStore((s) => s.set);
  const cur = useStore((s) => s.settings);
  const [s, setS] = useState<S.Settings | null>(cur);
  const [tab, setTab] = useState<"general" | "terminal" | "editor" | "files" | "hosts" | "ai">("general");
  const [kh, setKh] = useState<KH[]>([]);
  const [detected, setDetected] = useState("");
  const [info, setInfo] = useState<any>(null);

  useEffect(() => {
    SystemService.Info().then(setInfo);
    EditorService.DetectExternalEditor().then(setDetected).catch(() => {});
  }, []);
  useEffect(() => {
    if (tab === "hosts") SystemService.KnownHosts().then((k) => setKh(((k ?? []) as KH[]).filter(Boolean)));
  }, [tab]);
  if (!s) return null;
  const up = (p: Partial<S.Settings>) => setS({ ...s, ...p });
  const save = async () => {
    try {
      await SystemService.SaveSettings(s);
      useStore.getState().setSettings(s);
      applyTheme();
      set("settingsOpen", false);
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  const browse = async (k: "logDir" | "serverRoot") => {
    const p = await SystemService.PickFolder(t("common.pickFolder"));
    if (p) up({ [k]: p } as any);
  };

  const hint1 = t("settings.ignoreCaseHint1").split("{code}");
  const hint2 = t("settings.ignoreCaseHint2").split("{code}");

  const tabs = [
    ["general", t("settings.tab.general")],
    ["terminal", t("settings.tab.terminal")],
    ["editor", t("settings.tab.editor")],
    ["files", t("settings.tab.files")],
    ["hosts", t("settings.tab.hosts")],
    ["ai", t("settings.tab.ai")],
  ] as const;

  return (
    <Modal
      title={t("settings.title")}
      width={680}
      onClose={() => set("settingsOpen", false)}
      footer={
        <>
          <button className="btn" onClick={() => set("settingsOpen", false)}>{t("common.cancel")}</button>
          <button className="btn-primary" onClick={save}>{t("common.save")}</button>
        </>
      }
    >
      <div className="flex gap-4">
        <div className="flex w-32 shrink-0 flex-col gap-0.5">
          {tabs.map(([id, label]) => (
            <button key={id} className={"rounded px-2 py-1 text-left " + (tab === id ? "bg-sel" : "hover:bg-hover")} onClick={() => setTab(id)}>
              {label}
            </button>
          ))}
        </div>
        <div className="min-h-[340px] flex-1 space-y-3">
          {tab === "general" && (
            <>
              <Check checked={s.sftpAutoSwitch} onChange={(v) => up({ sftpAutoSwitch: v })}>
                {t("settings.sftpAutoSwitch")}
              </Check>
              <br />
              <Check checked={s.sessionProbe} onChange={(v) => up({ sessionProbe: v })}>
                {t("settings.sessionProbe")}
              </Check>
              <Field label={t("settings.language")}>
                <select className="input" value={s.language || "system"} onChange={(e) => up({ language: e.target.value })}>
                  <option value="system">{t("settings.language.system")}</option>
                  <option value="zh-TW">繁體中文</option>
                  <option value="en">English</option>
                </select>
              </Field>
              <Field label={t("settings.theme")}>
                <select className="input" value={s.theme} onChange={(e) => up({ theme: e.target.value })}>
                  <option value="system">{t("settings.theme.system")}</option>
                  <option value="dark">{t("settings.theme.dark")}</option>
                  <option value="dark-modern">{t("settings.theme.darkModern")}</option>
                  <option value="light">{t("settings.theme.light")}</option>
                </select>
              </Field>
              {info && (
                <div className="rounded border border-line p-2 text-[13px] text-mute select-text">
                  <div>{t("settings.info.version", { version: info.version, os: info.os, arch: info.arch })}</div>
                  <div>{t("settings.info.configDir", { dir: info.configDir })}</div>
                  <div>{t("settings.info.passwordStore", { store: info.osKeychain ? t("settings.info.keychain") : t("settings.info.encryptedFile") })}</div>
                  <div>{t("settings.info.agent", { state: info.agent ? t("settings.info.agentAvailable") : t("settings.info.agentMissing") })}</div>
                  <button className="mt-1 text-accent" onClick={() => SystemService.OpenPath(info.configDir)}>{t("settings.info.openConfigDir")}</button>
                </div>
              )}
            </>
          )}
          {tab === "terminal" && (
            <>
              <FontPicker kind="terminal" value={s.terminalFont} size={s.terminalFontSize} onChange={(v) => up({ terminalFont: v })} />
              <div className="grid grid-cols-2 gap-3">
                <Field label={t("settings.fontSize")}>
                  <input className="input" type="number" value={s.terminalFontSize} onChange={(e) => up({ terminalFontSize: Number(e.target.value) })} />
                </Field>
                <Field label={t("settings.scrollback")}>
                  <input className="input" type="number" value={s.scrollback} onChange={(e) => up({ scrollback: Number(e.target.value) })} />
                </Field>
              </div>
              <Check checked={s.copyOnSelect} onChange={(v) => up({ copyOnSelect: v })}>{t("settings.copyOnSelect")}</Check>
              <br />
              <Check checked={s.sshCompletionIgnoreCase} onChange={(v) => up({ sshCompletionIgnoreCase: v })}>
                {t("settings.ignoreCase")}
              </Check>
              <div className="ml-6 text-[12px] text-mute">
                {hint1[0]}<code>bind 'set completion-ignore-case on'</code>{hint1[1]}
                {hint2[0]}<code>echo 'set completion-ignore-case on' &gt;&gt; ~/.inputrc</code>{hint2[1]}
              </div>
              <br />
              <Check checked={s.cursorBlink} onChange={(v) => up({ cursorBlink: v })}>{t("settings.cursorBlink")}</Check>
              <br />
              <Check checked={s.smartCopyPaste} onChange={(v) => up({ smartCopyPaste: v })}>
                {t("settings.smartCopyPaste")}
              </Check>
              <Field label={t("settings.rightClick")} className="mt-2">
                <select className="input" value={s.termRightClick || "menu"} onChange={(e) => up({ termRightClick: e.target.value })}>
                  <option value="menu">{t("settings.rightClick.menu")}</option>
                  <option value="paste">{t("settings.rightClick.paste")}</option>
                </select>
              </Field>
              <div className="text-[13px] text-mute">
                {t("settings.copyKeys", { copy: t("settings.copyKeysCopy"), paste: t("settings.copyKeysPaste") })}
              </div>
            </>
          )}
          {tab === "editor" && (
            <>
              <FontPicker kind="editor" value={s.editorFont} size={s.editorFontSize} onChange={(v) => up({ editorFont: v })} />
              <div className="grid grid-cols-3 gap-3">
                <Field label={t("settings.fontSize")}>
                  <input className="input" type="number" value={s.editorFontSize} onChange={(e) => up({ editorFontSize: Number(e.target.value) })} />
                </Field>
                <Field label={t("settings.tabSize")}>
                  <input className="input" type="number" value={s.editorTabSize} onChange={(e) => up({ editorTabSize: Number(e.target.value) })} />
                </Field>
                <Field label={t("settings.bigFileLimit")}>
                  <input className="input" type="number" value={s.bigFileLimitMB} onChange={(e) => up({ bigFileLimitMB: Number(e.target.value) })} />
                </Field>
              </div>
              <div className="flex gap-4">
                <Check checked={s.editorMinimap} onChange={(v) => up({ editorMinimap: v })}>{t("settings.minimap")}</Check>
                <Check checked={s.editorWordWrap} onChange={(v) => up({ editorWordWrap: v })}>{t("settings.wordWrap")}</Check>
              </div>
              <div className="border-t border-line pt-3">
                <Check checked={s.useExternalEditor} onChange={(v) => up({ useExternalEditor: v })}>
                  {t("settings.useExternalEditor")}
                </Check>
                <Field label={t("settings.externalEditorCmd")} className="mt-2" hint={t("settings.externalEditorHint", { detected: detected || t("settings.externalEditorNotFound") })}>
                  <input className="input font-mono" value={s.externalEditorCmd} placeholder="code --wait {file}" onChange={(e) => up({ externalEditorCmd: e.target.value })} />
                </Field>
              </div>
            </>
          )}
          {tab === "files" && (
            <>
              <Field label={t("settings.logDir")}>
                <div className="flex gap-1">
                  <input className="input font-mono" value={s.logDir} onChange={(e) => up({ logDir: e.target.value })} />
                  <button className="btn" onClick={() => browse("logDir")}><FolderOpen size={14} /></button>
                </div>
              </Field>
              <div className="flex gap-4">
                <Check checked={s.logStripAnsi} onChange={(v) => up({ logStripAnsi: v })}>{t("settings.logStripAnsi")}</Check>
                <Check checked={s.logTimestamps} onChange={(v) => up({ logTimestamps: v })}>{t("settings.logTimestamps")}</Check>
              </div>
              <Field label={t("settings.serverRoot")} className="pt-2">
                <div className="flex gap-1">
                  <input className="input font-mono" value={s.serverRoot} onChange={(e) => up({ serverRoot: e.target.value })} />
                  <button className="btn" onClick={() => browse("serverRoot")}><FolderOpen size={14} /></button>
                </div>
              </Field>
              <Field label={t("settings.serverIdle")}>
                <input className="input w-32" type="number" value={s.serverIdleMinutes} onChange={(e) => up({ serverIdleMinutes: Number(e.target.value) })} />
              </Field>
            </>
          )}
          {tab === "ai" && <AISettings s={s} up={up} />}
          {tab === "hosts" && (
            <div>
              <div className="mb-2 text-[13px] text-mute">{t("settings.hostsHint")}</div>
              <div className="max-h-[320px] overflow-auto rounded border border-line">
                {kh.map((k) => (
                  <div key={k.line} className="flex items-center gap-2 border-b border-line px-2 py-1 last:border-0">
                    <div className="min-w-0 flex-1">
                      <div className="truncate">{k.hosts}</div>
                      <div className="truncate font-mono text-[12px] text-mute">{k.keyType} {k.fingerprint}</div>
                    </div>
                    <button
                      className="icon-btn"
                      onClick={() =>
                        SystemService.RemoveKnownHost(k.line)
                          .then(() => SystemService.KnownHosts())
                          .then((x) => setKh(((x ?? []) as KH[]).filter(Boolean)))
                      }
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
                {!kh.length && <div className="p-3 text-mute">{t("settings.hostsEmpty")}</div>}
              </div>
            </div>
          )}
        </div>
      </div>
    </Modal>
  );
}

// Font dropdown: platform default, fonts imported into Pinion, detected installed
// monospace fonts, or a custom CSS font-family (with a found / not-found check).
function FontPicker(props: { kind: FontKind; value: string; size: number; onChange: (v: string) => void }) {
  const t = useT();
  const userFonts = useUserFonts((x) => x.fonts);
  const imported = useMemo(() => userFamilies(userFonts), [userFonts]);
  const installed = useMemo(() => installedMonoFonts().filter((f) => !imported.includes(f)), [imported]);
  const all = useMemo(() => [...imported, ...installed], [imported, installed]);
  const { kind, value } = props;
  const picked = pickedFont(kind, value, all);
  const [custom, setCustom] = useState(!!value.trim() && !picked);
  const [busy, setBusy] = useState(false);
  const sel = custom ? "__custom" : !value.trim() ? "" : picked ?? "__custom";
  const first = parseFamilies(value)[0];
  // Re-check after imports/removals change document.fonts.
  const found = useMemo(() => (first ? fontAvailable(first) : true), [first, userFonts]);

  const doImport = async () => {
    setBusy(true);
    try {
      const fam = await importFont(t("settings.font.importTitle"));
      if (fam) {
        setCustom(false);
        props.onChange(fontChain(kind, fam));
        toast("success", t("settings.font.imported", { family: fam }));
      }
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setBusy(false);
    }
  };
  const doRemove = async (file: string) => {
    if (!(await confirmDialog(t("settings.font.removeTitle"), t("settings.font.removeConfirm", { file }), { danger: true }))) return;
    removeFont(file).catch((e) => toast("error", errMsg(e)));
  };

  return (
    <div>
      <Field label={t("settings.font")}>
        <select
          className="input"
          value={sel}
          onChange={(e) => {
            const v = e.target.value;
            setCustom(v === "__custom");
            if (v === "__custom") return props.onChange(resolveFont(kind, value));
            props.onChange(v ? fontChain(kind, v) : "");
          }}
        >
          <option value="">{t("settings.font.default")}</option>
          {imported.length > 0 && (
            <optgroup label={t("settings.font.groupImported")}>
              {imported.map((f) => (
                <option key={f} value={f}>{f}</option>
              ))}
            </optgroup>
          )}
          <optgroup label={t("settings.font.groupInstalled")}>
            {installed.map((f) => (
              <option key={f} value={f}>{f}</option>
            ))}
          </optgroup>
          <option value="__custom">{t("settings.font.custom")}</option>
        </select>
      </Field>
      {sel === "__custom" && (
        <>
          <input
            className="input mt-1 font-mono"
            value={value}
            placeholder={resolveFont(kind, "")}
            onChange={(e) => props.onChange(e.target.value)}
          />
          {first && (
            <div className={"mt-0.5 text-[12px] " + (found ? "text-mute" : "text-danger")}>
              {found ? t("settings.font.found", { name: first }) : t("settings.font.notFound", { name: first })}
            </div>
          )}
        </>
      )}
      <div
        className="mt-1 truncate rounded border border-line px-2 py-1"
        style={{ fontFamily: resolveFont(kind, value), fontSize: props.size || 14 }}
      >
        {t("settings.font.preview")}
      </div>
      <div className="mt-1.5 flex items-center gap-2">
        <button className="btn" disabled={busy} onClick={doImport}>
          <Upload size={14} className="mr-1 inline" />
          {t("settings.font.import")}
        </button>
        <span className="text-[12px] text-mute">{t("settings.font.importHint")}</span>
      </div>
      {userFonts.length > 0 && (
        <div className="mt-1.5 max-h-[96px] overflow-auto rounded border border-line">
          {userFonts.map((f) => (
            <div key={f.file} className="flex items-center gap-2 border-b border-line px-2 py-0.5 text-[12px] last:border-0">
              <span className="min-w-0 flex-1 truncate">
                {f.family}
                <span className="ml-2 text-mute">{f.file}</span>
              </span>
              <button className="icon-btn" title={t("settings.font.removeTitle")} onClick={() => doRemove(f.file)}>
                <Trash2 size={13} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

const AI_PROVIDERS = {
  anthropic: {
    defaultModel: "claude-haiku-5-5",
    keyPlaceholder: "sk-ant-…",
    keyURL: "https://console.anthropic.com/settings/keys",
    modelField: "aiModel",
  },
  openrouter: {
    defaultModel: "anthropic/claude-haiku-5.5",
    keyPlaceholder: "sk-or-…",
    keyURL: "https://openrouter.ai/settings/keys",
    modelField: "openRouterModel",
  },
} as const;
type AIProvider = keyof typeof AI_PROVIDERS;

function AISettings({ s, up }: { s: S.Settings; up: (p: Partial<S.Settings>) => void }) {
  const t = useT();
  const provider: AIProvider = s.aiProvider === "openrouter" ? "openrouter" : "anthropic";
  const cfg = AI_PROVIDERS[provider];
  const [hasKey, setHasKey] = useState(false);
  const [key, setKey] = useState("");
  useEffect(() => {
    setKey("");
    ImportService.HasAIKey(provider).then(setHasKey);
  }, [provider]);
  const saveKey = async (v: string) => {
    try {
      await ImportService.SetAIKey(provider, v);
      setHasKey(!!v.trim());
      setKey("");
      if (v.trim()) toast("success", t("settings.ai.keySavedToast"));
    } catch (e) {
      toast("error", errMsg(e));
    }
  };
  return (
    <>
      <p className="text-mute">{t("settings.ai.intro")}</p>
      <Field label={t("settings.ai.provider")}>
        <select
          className="input"
          value={provider}
          onChange={async (e) => {
            // Saved right away, like the key, so the import dialog uses it even
            // if this dialog is closed without pressing Save.
            const v = e.target.value;
            up({ aiProvider: v });
            const cur = useStore.getState().settings;
            if (cur) {
              const next = { ...cur, aiProvider: v };
              try {
                await SystemService.SaveSettings(next);
                useStore.getState().setSettings(next);
              } catch (err) {
                toast("error", errMsg(err));
              }
            }
          }}
        >
          <option value="anthropic">{t("settings.ai.provider.anthropic")}</option>
          <option value="openrouter">{t("settings.ai.provider.openrouter")}</option>
        </select>
      </Field>
      <Field
        label={t("settings.ai.key")}
        hint={
          <a className="cursor-pointer underline" onClick={() => SystemService.OpenURL(cfg.keyURL)}>
            {t(provider === "openrouter" ? "settings.ai.getKeyOpenRouter" : "settings.ai.getKey")}
          </a>
        }
      >
        <div className="flex gap-1">
          <input
            className="input font-mono"
            type="password"
            autoComplete="off"
            value={key}
            placeholder={hasKey ? "••••••••" : cfg.keyPlaceholder}
            onChange={(e) => setKey(e.target.value)}
          />
          <button className="btn" disabled={!key.trim()} onClick={() => saveKey(key)}>{t("settings.ai.saveKey")}</button>
          {hasKey && (
            <button className="btn" onClick={() => saveKey("")}><Trash2 size={14} /> {t("settings.ai.removeKey")}</button>
          )}
        </div>
      </Field>
      <div className="text-[13px] text-mute">{hasKey ? t("settings.ai.keySaved") : t("settings.ai.keyNone")}</div>
      <Field label={t("settings.ai.model")} hint={t("settings.ai.modelHint", { model: cfg.defaultModel })}>
        <input
          className="input font-mono"
          value={s[cfg.modelField] ?? ""}
          placeholder={cfg.defaultModel}
          onChange={(e) => up({ [cfg.modelField]: e.target.value } as Partial<S.Settings>)}
        />
      </Field>
    </>
  );
}
