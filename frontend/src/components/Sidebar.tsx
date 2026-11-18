import { useEffect, useMemo, useState } from "react";
import {
  ChevronRight, ChevronDown, Folder, FolderOpen, Server, Cpu, Network, TerminalSquare, Search,
  Plus, FolderPlus, Pencil, Copy, Trash2, Play, Upload, Download, Cable, HardDrive, FolderTree, Activity,
} from "lucide-react";
import { useStore, toast, type SidebarView } from "../lib/store";
import { SessionService, errMsg, type store as S } from "../lib/api";
import { openSession } from "../lib/actions";
import { openMenu, confirmDialog, inputDialog } from "./ui";
import TunnelsPanel from "./panels/TunnelsPanel";
import ServersPanel from "./panels/ServersPanel";
import SftpPanel from "./SftpPanel";
import { useT } from "../lib/i18n";
import { useProbe, probeable, probeNow, type ProbeResult } from "../lib/probe";

/** Text describing a session's last probe, for tooltips. */
function probeText(t: ReturnType<typeof useT>, r: ProbeResult | undefined): string {
  if (!r) return t("sidebar.probe.pending");
  const at = new Date(r.at).toLocaleTimeString();
  return r.ok
    ? t("sidebar.probe.up", { addr: r.addr, ms: r.latencyMs, at })
    : t("sidebar.probe.down", { addr: r.addr, error: r.error ?? "", at });
}

/** Reachability dot: green = reachable, red = not, grey = not probed yet. */
function ProbeDot({ id }: { id: string }) {
  const t = useT();
  const r = useProbe((s) => s.r[id]);
  const cls = !r ? "bg-zinc-400/50" : r.ok ? "bg-emerald-500" : "bg-red-500";
  return <span className={"h-1.5 w-1.5 shrink-0 rounded-full " + cls} title={probeText(t, r)} />;
}

export function typeIcon(type: string, size = 14) {
  switch (type) {
    case "ssh":
      return <Server size={size} className="text-sky-500" />;
    case "serial":
      return <Cpu size={size} className="text-amber-500" />;
    case "telnet":
      return <TerminalSquare size={size} className="text-violet-500" />;
    case "tcp":
    case "udp":
      return <Network size={size} className="text-emerald-500" />;
    default:
      return <TerminalSquare size={size} className="text-mute" />;
  }
}

function sessionSubtitle(s: S.Session): string {
  if (s.ssh) return `${s.ssh.user ? s.ssh.user + "@" : ""}${s.ssh.host}${s.ssh.port !== 22 ? ":" + s.ssh.port : ""}`;
  if (s.telnet) return `${s.telnet.host}:${s.telnet.port}`;
  if (s.serial) return `${s.serial.port} ${s.serial.baud}`;
  if (s.net) return `${s.type.toUpperCase()} ${s.net.mode} ${s.net.host}:${s.net.port}`;
  return s.type;
}

interface Node {
  name: string;
  path: string;
  children: Map<string, Node>;
  sessions: S.Session[];
}

function buildTree(folders: string[], sessions: S.Session[]): Node {
  const root: Node = { name: "", path: "", children: new Map(), sessions: [] };
  const ensure = (path: string) => {
    let n = root;
    if (!path) return n;
    let acc = "";
    for (const part of path.split("/")) {
      acc = acc ? acc + "/" + part : part;
      if (!n.children.has(part)) n.children.set(part, { name: part, path: acc, children: new Map(), sessions: [] });
      n = n.children.get(part)!;
    }
    return n;
  };
  folders.forEach(ensure);
  sessions.forEach((s) => ensure(s.folder).sessions.push(s));
  return root;
}

/** The terminal tab shown in the focused pane (split-aware). */
export function useFocusedTab() {
  return useStore((s) => {
    const id = s.split.on && s.split.focus === "right" && s.split.rightId ? s.split.rightId : s.activeId;
    return s.tabs.find((t) => t.id === id);
  });
}

export default function Sidebar() {
  const tr = useT();
  const view = useStore((s) => s.sidebar);
  const set = useStore((s) => s.set);
  const autoSwitch = useStore((s) => s.settings?.sftpAutoSwitch ?? true);
  const [width, setWidth] = useState(() => Number(localStorage.getItem("pinion.sidebarWidth")) || 300);
  const focused = useFocusedTab();
  // SFTP target: the focused SSH tab, or the SSH tab a remote editor came from
  const sshTab = useStore((s) => {
    if (focused?.kind === "terminal" && focused.type === "ssh") return focused;
    if (focused?.kind === "editor" && focused.termTabId) {
      const t = s.tabs.find((x) => x.id === focused.termTabId);
      if (t?.kind === "terminal") return t;
    }
    return undefined;
  });

  // One simple rule: switching to an SSH tab shows its SFTP files,
  // switching to anything else shows the session list. A manual choice
  // (tunnels, servers...) holds until the next tab switch.
  useEffect(() => {
    if (!autoSwitch) return;
    set("sidebar", sshTab ? "sftp" : "sessions");
  }, [focused?.id, autoSwitch]);

  const tabs: { id: SidebarView; label: string; icon: JSX.Element }[] = [
    { id: "sessions", label: "Sessions", icon: <Server size={14} /> },
    { id: "sftp", label: "SFTP", icon: <FolderTree size={14} /> },
    { id: "tunnels", label: tr("sidebar.tunnels"), icon: <Cable size={14} /> },
    { id: "servers", label: tr("sidebar.servers"), icon: <HardDrive size={14} /> },
  ];
  const shown = view;
  const choose = (v: SidebarView) => set("sidebar", v);
  const startDrag = (e: React.MouseEvent) => {
    const x0 = e.clientX;
    const w0 = width;
    let w = w0;
    const move = (ev: MouseEvent) => {
      w = Math.max(220, Math.min(700, w0 + ev.clientX - x0));
      setWidth(w);
    };
    const up = () => {
      localStorage.setItem("pinion.sidebarWidth", String(w));
      window.removeEventListener("mousemove", move);
      window.removeEventListener("mouseup", up);
    };
    window.addEventListener("mousemove", move);
    window.addEventListener("mouseup", up);
  };
  return (
    <div className="relative flex shrink-0 flex-col border-r border-line bg-side" style={{ width }}>
      <div className="sidebar-tabs flex border-b border-line">
        {tabs.map((t) => (
          <button
            key={t.id}
            className={
              "flex min-w-0 flex-1 items-center justify-center gap-1 whitespace-nowrap py-1.5 text-[13px] " +
              (shown === t.id ? "border-b-2 border-accent text-fg" : "text-mute hover:text-fg")
            }
            onClick={() => choose(t.id)}
          >
            <span className="sidebar-tab-icon flex">{t.icon}</span>
            {t.label}
          </button>
        ))}
      </div>
      <div className="min-h-0 flex-1">
        {shown === "sessions" && <SessionTree />}
        {shown === "sftp" &&
          (sshTab ? (
            <SftpPanel key={sshTab.id} tab={sshTab} />
          ) : (
            <div className="p-4 text-center text-mute">
              {tr("sidebar.sftpHint")}
              <br />
              <button className="mt-2 text-accent" onClick={() => set("sidebar", "sessions")}>
                {tr("sidebar.backToSessions")}
              </button>
            </div>
          ))}
        {shown === "tunnels" && <TunnelsPanel />}
        {shown === "servers" && <ServersPanel />}
      </div>
      <div className="absolute right-[-3px] top-0 z-10 h-full w-[6px] cursor-col-resize" onMouseDown={startDrag} />
    </div>
  );
}

function SessionTree() {
  const t = useT();
  const { folders, sessions, set } = useStore();
  const [q, setQ] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [selected, setSelected] = useState<string | null>(null);

  const filtered = useMemo(() => {
    const t = q.trim().toLowerCase();
    if (!t) return sessions;
    return sessions.filter((s) =>
      [s.name, s.folder, sessionSubtitle(s), s.notes ?? ""].some((x) => x.toLowerCase().includes(t)),
    );
  }, [q, sessions]);
  const tree = useMemo(() => buildTree(q ? [] : folders, filtered), [folders, filtered, q]);

  const newFolder = async (parent: string) => {
    const name = await inputDialog(t("sidebar.newFolder"), "", parent ? t("sidebar.newFolderUnder", { parent }) : t("sidebar.newFolderHint"));
    if (!name) return;
    SessionService.AddFolder(parent ? parent + "/" + name : name).catch((e) => toast("error", errMsg(e)));
  };

  const probeOn = useStore((st) => st.settings?.sessionProbe ?? true);
  const probeResults = useProbe((st) => st.r);

  const folderMenu = (e: React.MouseEvent, n: Node) =>
    openMenu(e, [
      { label: t("sidebar.newSessionHere"), icon: <Plus size={14} />, onClick: () => set("sessionEditor", { folder: n.path }) },
      { label: t("sidebar.newSubfolder"), icon: <FolderPlus size={14} />, onClick: () => newFolder(n.path) },
      {
        label: t("sidebar.connectAll"),
        icon: <Play size={14} />,
        onClick: () => collect(n).forEach((s) => openSession(s.id)),
      },
      ...(probeOn
        ? [{
            label: t("sidebar.probe.checkFolder"),
            icon: <Activity size={14} />,
            onClick: () => probeNow(collect(n).filter(probeable).map((s) => s.id)),
          }]
        : []),
      { separator: true },
      {
        label: t("common.rename"),
        icon: <Pencil size={14} />,
        onClick: async () => {
          const v = await inputDialog(t("sidebar.renameFolder"), n.path, t("sidebar.renameFolderHint"));
          if (v && v !== n.path) SessionService.RenameFolder(n.path, v).catch((er) => toast("error", errMsg(er)));
        },
      },
      {
        label: t("sidebar.exportFolder"),
        icon: <Upload size={14} />,
        onClick: () =>
          SessionService.Export(n.path)
            .then((p) => p && toast("success", t("sidebar.exportedTo", { path: p })))
            .catch((er) => toast("error", errMsg(er))),
      },
      {
        label: t("sidebar.importToFolder"),
        icon: <Download size={14} />,
        onClick: () =>
          SessionService.ImportPinion(n.path)
            .then((c) => c && toast("success", t("sidebar.imported", { count: c })))
            .catch((er) => toast("error", errMsg(er))),
      },
      { separator: true },
      {
        label: t("sidebar.deleteFolder"),
        icon: <Trash2 size={14} />,
        danger: true,
        onClick: async () => {
          const count = collect(n).length;
          if (
            await confirmDialog(
              t("sidebar.deleteFolder"),
              count ? t("sidebar.deleteFolderWithSessions", { path: n.path, count }) : t("sidebar.deleteFolderConfirm", { path: n.path }),
              { danger: true, ok: t("common.delete") },
            )
          )
            SessionService.DeleteFolder(n.path, true).catch((er) => toast("error", errMsg(er)));
        },
      },
    ]);

  const sessionMenu = (e: React.MouseEvent, s: S.Session) => {
    setSelected(s.id);
    openMenu(e, [
      { label: t("sidebar.connect"), icon: <Play size={14} />, onClick: () => openSession(s.id) },
      { label: t("common.edit"), icon: <Pencil size={14} />, onClick: () => set("sessionEditor", { session: s }) },
      {
        label: t("sidebar.duplicate"),
        icon: <Copy size={14} />,
        onClick: () => SessionService.Duplicate(s.id).catch((er) => toast("error", errMsg(er))),
      },
      ...(probeOn && probeable(s)
        ? [{ label: t("sidebar.probe.checkNow"), icon: <Activity size={14} />, onClick: () => probeNow([s.id]) }]
        : []),
      { separator: true },
      {
        label: t("common.delete"),
        icon: <Trash2 size={14} />,
        danger: true,
        onClick: async () => {
          if (await confirmDialog(t("sidebar.deleteSession"), t("sidebar.deleteSessionConfirm", { name: s.name }), { danger: true, ok: t("common.delete") }))
            SessionService.Delete(s.id).catch((er) => toast("error", errMsg(er)));
        },
      },
    ]);
  };

  const onDrop = (e: React.DragEvent, folder: string) => {
    const id = e.dataTransfer.getData("pinion/session");
    if (!id) return;
    e.preventDefault();
    e.stopPropagation();
    SessionService.Move(id, folder).catch((er) => toast("error", errMsg(er)));
  };

  const renderNode = (n: Node, depth: number): JSX.Element[] => {
    const out: JSX.Element[] = [];
    const kids = [...n.children.values()].sort((a, b) => a.name.localeCompare(b.name));
    for (const c of kids) {
      const isCol = !q && collapsed[c.path];
      out.push(
        <div
          key={"f:" + c.path}
          className="flex cursor-default items-center gap-1 py-[3px] pr-2 hover:bg-hover"
          style={{ paddingLeft: 6 + depth * 14 }}
          onClick={() => setCollapsed((x) => ({ ...x, [c.path]: !x[c.path] }))}
          onContextMenu={(e) => folderMenu(e, c)}
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => onDrop(e, c.path)}
        >
          {isCol ? <ChevronRight size={14} className="text-mute" /> : <ChevronDown size={14} className="text-mute" />}
          {isCol ? <Folder size={14} className="text-amber-500" /> : <FolderOpen size={14} className="text-amber-500" />}
          <span className="truncate">{c.name}</span>
          <span className="ml-auto text-[12px] text-mute">{collect(c).length || ""}</span>
        </div>,
      );
      if (!isCol) out.push(...renderNode(c, depth + 1));
    }
    for (const s of n.sessions) {
      out.push(
        <div
          key={"s:" + s.id}
          draggable
          onDragStart={(e) => e.dataTransfer.setData("pinion/session", s.id)}
          className={
            "group flex cursor-default items-center gap-1.5 py-[3px] pr-2 " +
            (selected === s.id ? "bg-sel" : "hover:bg-hover")
          }
          style={{ paddingLeft: 6 + depth * 14 + 16 }}
          onClick={() => setSelected(s.id)}
          onDoubleClick={() => openSession(s.id)}
          onContextMenu={(e) => sessionMenu(e, s)}
          title={`${sessionSubtitle(s)}${s.notes ? "\n" + s.notes : ""}${
            probeOn && probeable(s) ? "\n" + probeText(t, probeResults[s.id]) : ""
          }`}
        >
          {probeOn && probeable(s) ? <ProbeDot id={s.id} /> : <span className="w-1.5 shrink-0" />}
          {s.color && <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: s.color }} />}
          {typeIcon(s.type)}
          <span className="truncate">{s.name}</span>
          <span className="ml-auto hidden truncate pl-2 text-[12px] text-mute group-hover:block">
            {sessionSubtitle(s)}
          </span>
        </div>,
      );
    }
    return out;
  };

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-1 p-2">
        <div className="relative flex-1">
          <Search size={13} className="absolute left-2 top-1.5 text-mute" />
          <input className="input pl-7" placeholder={t("sidebar.searchPlaceholder")} value={q} onChange={(e) => setQ(e.target.value)} />
        </div>
        <button className="icon-btn" title={t("sidebar.newFolder")} onClick={() => newFolder("")}>
          <FolderPlus size={16} />
        </button>
        <button className="icon-btn" title={t("sidebar.newSession")} onClick={() => set("sessionEditor", {})}>
          <Plus size={16} />
        </button>
      </div>
      <div
        className="min-h-0 flex-1 overflow-auto pb-4"
        onDragOver={(e) => e.preventDefault()}
        onDrop={(e) => onDrop(e, "")}
        onContextMenu={(e) =>
          e.target === e.currentTarget &&
          openMenu(e, [
            { label: t("sidebar.newSession"), icon: <Plus size={14} />, onClick: () => set("sessionEditor", {}) },
            { label: t("sidebar.newFolder"), icon: <FolderPlus size={14} />, onClick: () => newFolder("") },
            {
              label: t("sidebar.exportAll"),
              icon: <Upload size={14} />,
              onClick: () => SessionService.Export("").then((p) => p && toast("success", t("sidebar.exportedTo", { path: p }))),
            },
            {
              label: t("sidebar.importJson"),
              icon: <Download size={14} />,
              onClick: () => SessionService.ImportPinion("").then((c) => c && toast("success", t("sidebar.imported", { count: c }))),
            },
          ])
        }
      >
        {renderNode(tree, 0)}
        {sessions.length === 0 && (
          <div className="p-4 text-center text-mute">
            {t("sidebar.empty")}
            <br />
            <button className="mt-2 text-accent" onClick={() => set("sessionEditor", {})}>
              {t("sidebar.addOne")}
            </button>
            {t("sidebar.or")}
            <button className="text-accent" onClick={() => set("importOpen", true)}>
              {t("sidebar.importSettings")}
            </button>
          </div>
        )}
      </div>
    </div>
  );
}

function collect(n: Node): S.Session[] {
  return [...n.sessions, ...[...n.children.values()].flatMap(collect)];
}
