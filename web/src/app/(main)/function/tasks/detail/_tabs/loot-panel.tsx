"use client";

import * as React from "react";

import { CheckIcon, ClipboardCopyIcon, Loader2Icon, RefreshCwIcon, XIcon } from "lucide-react";

import { candoSend, lootapi, type LootResponse, type LootRow } from "@/lib/api";
import { cn } from "@/lib/utils";

// 战果清单（CARD-B01）
//
// 形态：会话页右侧同一个方框里的表格（与旁路提问/复盘官互斥切换）。
// 内容是**通用能力模板**，只列这个站相关的行；状态由后端实时算，面板只负责显示。
const ST: Record<string, { label: string; cls: string }> = {
  done: { label: "已拿到", cls: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  run: { label: "正在试", cls: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  tried: { label: "试过未成", cls: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  blank: { label: "还没试", cls: "bg-slate-500/10 text-slate-500" },
};

export function LootCapsule({
  taskId,
  open,
  onToggle,
}: {
  taskId: string;
  open: boolean;
  onToggle: () => void;
}) {
  const [done, setDone] = React.useState<number | null>(null);
  React.useEffect(() => {
    let alive = true;
    const load = () =>
      lootapi
        .get(taskId)
        .then((d) => alive && setDone(d.done))
        .catch(() => void 0);
    load();
    const t = setInterval(load, 20000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [taskId]);

  return (
    <button
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-[12px] font-medium transition-colors",
        open ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700" : "hover:bg-accent",
      )}
      onClick={onToggle}
      title="战果清单：这个站现在拿到了哪些权限/数据"
    >
      <CheckIcon className="size-3.5" />
      战果
      {done !== null && done > 0 && (
        <span className="rounded bg-emerald-500/15 px-1 text-[10px] font-semibold text-emerald-600">{done}</span>
      )}
    </button>
  );
}

export function LootPanel({ taskId, onClose }: { taskId: string; onClose: () => void }) {
  const [data, setData] = React.useState<LootResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [openRow, setOpenRow] = React.useState("");
  const [auto, setAuto] = React.useState(true);
  const [busy, setBusy] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [stamp, setStamp] = React.useState("");

  const load = React.useCallback(() => {
    setBusy(true);
    lootapi
      .get(taskId)
      .then((d) => {
        setData(d);
        setErr("");
        const n = new Date();
        setStamp(
          `更新于 ${String(n.getHours()).padStart(2, "0")}:${String(n.getMinutes()).padStart(2, "0")}:${String(n.getSeconds()).padStart(2, "0")}`,
        );
      })
      .catch(() => setErr("战果清单取不到"))
      .finally(() => setBusy(false));
  }, [taskId]);

  React.useEffect(() => {
    load();
  }, [load]);

  React.useEffect(() => {
    if (!auto) return;
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, [auto, load]);

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setMsg("已复制");
    } catch {
      setMsg("复制失败，请手动选中");
    }
  }

  async function send(text: string) {
    try {
      await candoSend(taskId, text, "");
      setMsg("已作为提示发给主 Agent，规划者下一轮会读到");
    } catch {
      setMsg("发送失败（可能任务已终止）");
    }
  }

  const rows = data?.rows ?? [];

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <span className="flex size-5 items-center justify-center rounded bg-emerald-500 text-[10px] font-bold text-white">得</span>
        <div className="min-w-0 flex-1">
          <div className="text-[12.5px] font-semibold">战果清单</div>
          <div className="text-[10px] text-muted-foreground">实时统计 · 只列与这个站相关的项</div>
        </div>
        <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10.5px] font-semibold text-emerald-600">
          {data ? `${data.done}/${data.rows.length}` : "…"}
        </span>
        <button className="text-muted-foreground hover:text-foreground" onClick={onClose} aria-label="关闭">
          <XIcon className="size-4" />
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-2 border-b px-3 py-1.5">
        <button className="inline-flex items-center gap-1 rounded-md border px-2 py-0.5 text-[11px]" onClick={load}>
          {busy ? <Loader2Icon className="size-3 animate-spin" /> : <RefreshCwIcon className="size-3" />}
          刷新
        </button>
        <button
          className={cn(
            "inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px]",
            auto ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-600" : "text-muted-foreground",
          )}
          onClick={() => setAuto((v) => !v)}
        >
          <span className={cn("size-1.5 rounded-full", auto ? "bg-emerald-500" : "bg-slate-300")} />
          自动刷新 {auto ? "开" : "关"}
        </button>
        <span className="ml-auto text-[10px] text-muted-foreground">{stamp}</span>
      </div>

      <div className="min-h-0 flex-1 overflow-auto">
        {err && <div className="px-3 py-2 text-[11px] text-rose-600">{err}</div>}
        {data && rows.length === 0 && (
          <div className="px-4 py-10 text-center text-[11.5px] leading-relaxed text-muted-foreground">
            这个站还没确认任何能力
            <br />
            （等第一个漏洞上报后，这里会自动冒出对应的行）
          </div>
        )}
        {rows.length > 0 && (
          <table className="w-full text-[11.5px]">
            <thead className="sticky top-0 z-10 bg-card">
              <tr className="border-b text-left text-[10px] text-muted-foreground">
                <th className="px-3 py-1.5">能做什么</th>
                <th className="w-[76px] px-1 py-1.5">状态</th>
                <th className="w-[70px] px-2 py-1.5">依据</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r: LootRow) => {
                const isOpen = openRow === r.key;
                const st = ST[r.status] ?? ST.blank;
                return (
                  <React.Fragment key={r.key}>
                    <tr
                      className="cursor-pointer border-b align-top hover:bg-accent/40"
                      onClick={() => setOpenRow(isOpen ? "" : r.key)}
                    >
                      <td className="px-3 py-1.5">{r.label}</td>
                      <td className="px-1 py-1.5">
                        <span className={cn("inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] font-semibold", st.cls)}>
                          {r.status === "done" && <CheckIcon className="size-3" />}
                          {st.label}
                        </span>
                      </td>
                      <td className="px-2 py-1.5 text-muted-foreground">
                        {(r.evidence ?? []).length > 0 ? (r.evidence ?? []).map((e) => `#${e.id}`).join(" ") : "—"}
                      </td>
                    </tr>
                    {isOpen && (
                      <tr className="border-b bg-muted/30">
                        <td colSpan={3} className="px-3 py-2">
                          {(r.evidence ?? []).map((e) => (
                            <div key={e.id} className="text-[11px] leading-snug">
                              <span className="text-muted-foreground">finding #{e.id}</span> {e.title}
                            </div>
                          ))}
                          {!r.evidence?.length && <div className="text-[11px] text-muted-foreground">{r.why}</div>}
                          {r.hint && <div className="mt-1 text-[11px] text-muted-foreground">下一步：{r.hint}</div>}
                          <div className="mt-1.5 flex flex-wrap gap-1.5">
                            {r.hint && (
                              <>
                                <button className="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10.5px]" onClick={() => copy(r.hint!)}>
                                  <ClipboardCopyIcon className="size-3" /> 复制下一步
                                </button>
                                <button
                                  className="rounded border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-[10.5px] font-medium text-emerald-700"
                                  onClick={() => send(r.hint!)}
                                >
                                  让它去试
                                </button>
                              </>
                            )}
                            {r.status === "done" && r.evidence?.length ? (
                              <button
                                className="rounded border px-1.5 py-0.5 text-[10.5px]"
                                onClick={() => copy(r.evidence!.map((e) => `#${e.id} ${e.title}`).join("\n"))}
                              >
                                复制证据
                              </button>
                            ) : null}
                          </div>
                        </td>
                      </tr>
                    )}
                  </React.Fragment>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {msg && <div className="border-t bg-muted/40 px-3 py-1.5 text-[11px] text-muted-foreground">{msg}</div>}
      <div className="border-t px-3 py-1.5 text-[10.5px] text-muted-foreground">
        通用模板；没有证据的行不会出现。数据由后端实时计算，面板关着也照常更新。
      </div>
    </div>
  );
}
