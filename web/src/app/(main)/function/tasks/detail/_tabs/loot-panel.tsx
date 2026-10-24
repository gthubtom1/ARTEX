"use client";

import * as React from "react";

import { CheckIcon, ClipboardCopyIcon, Loader2Icon, RefreshCwIcon, SparklesIcon, XIcon } from "lucide-react";
import { useRouter } from "next/navigation";

import { briefapi, lootapi, type LootResponse, type LootRow } from "@/lib/api";
import { cn } from "@/lib/utils";

// 战果清单（CARD-B01）
//
// 形态：会话页右侧同一个方框里的表格（与旁路提问/复盘官互斥切换）。
// 内容是**通用能力模板**，只列这个站相关的行；状态由后端实时算，面板只负责显示。
// 上下两区：上=清单表（可滚），下=详情（默认不渲染，点行升起，分隔条可拖）。
const ST: Record<LootRow["status"], { label: string; cls: string }> = {
  done: { label: "✓ 已拿到", cls: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  run: { label: "正在试", cls: "bg-blue-500/15 text-blue-600 dark:text-blue-400" },
  tried: { label: "试过未成", cls: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  // todo = 你标的「还没试」；na = 你标的「不适用」（不计入分母）
  todo: { label: "还没试", cls: "bg-slate-500/10 text-slate-600" },
  na: { label: "不适用", cls: "bg-slate-500/10 text-slate-400 line-through" },
  blank: { label: "待确认", cls: "bg-slate-500/10 text-slate-500" },
};

// 详情区高度夹取的上下限（下限固定 120px，上限为容器 70%）
const MIN_H = 120;

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
  // 未读战果（P0-b）：后端 /brief 带的 new_since，不看就一直是数字。
  const [fresh, setFresh] = React.useState(0);
  React.useEffect(() => {
    let alive = true;
    const load = () => {
      lootapi
        .get(taskId)
        .then((d) => alive && setDone(d.done))
        .catch(() => void 0);
      briefapi
        .get(taskId)
        .then((b) => alive && setFresh(b.new_since ?? 0))
        .catch(() => void 0);
    };
    load();
    const t = setInterval(load, 20000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [taskId]);

  // 胶囊底色：打开中 > 有新战果 > 默认（写成变量，避免嵌套三元）
  let tone = "hover:bg-accent";
  if (fresh > 0) tone = "border-rose-500/50 hover:bg-rose-500/5";
  if (open) tone = "border-emerald-500/40 bg-emerald-500/10 text-emerald-700";

  return (
    <button
      className={cn(
        "inline-flex items-center gap-1.5 rounded-md border px-2.5 py-1 text-[12px] font-medium transition-colors",
        tone,
      )}
      onClick={onToggle}
      title={
        fresh > 0
          ? `战果清单：有 ${fresh} 条新战果还没看（打开即清零）`
          : "战果清单：这个站现在拿到了哪些权限/数据"
      }
    >
      <CheckIcon className="size-3.5" />
      战果
      {done !== null && done > 0 && (
        <span className="rounded bg-emerald-500/15 px-1 text-[10px] font-semibold text-emerald-600">{done}</span>
      )}
      {fresh > 0 && (
        <span className="inline-flex items-center gap-0.5 rounded-full bg-rose-500 px-1 text-[10px] font-semibold text-white">
          <span className="size-1 rounded-full bg-white/90" />
          {fresh > 9 ? "+9" : `+${fresh}`}
        </span>
      )}
    </button>
  );
}

export function LootPanel({ taskId, onClose }: { taskId: string; onClose: () => void }) {
  const [data, setData] = React.useState<LootResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [auto, setAuto] = React.useState(true);
  const [busy, setBusy] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [stamp, setStamp] = React.useState("");
  const router = useRouter();

  // 详情区：selected===null 时完全不渲染（不占空间）
  const [selected, setSelected] = React.useState<string | null>(null);
  const [detailH, setDetailH] = React.useState<number | null>(null);
  const [showEvidence, setShowEvidence] = React.useState(false);
  const [aiText, setAiText] = React.useState<Record<string, string>>({});
  const [aiBusy, setAiBusy] = React.useState<string | null>(null);
  const [aiErr, setAiErr] = React.useState<Record<string, string>>({});

  // AI 补这个站特有的项：只追加到列表末尾，status=blank，不计入 done/total
  const [extra, setExtra] = React.useState<LootRow[]>([]);
  const [suggestBusy, setSuggestBusy] = React.useState(false);
  // 人工标记（还没试 / 不适用）进行中的 key+state，用于禁用按钮
  const [stateBusy, setStateBusy] = React.useState<string | null>(null);

  const rootRef = React.useRef<HTMLDivElement>(null);
  const detailRef = React.useRef<HTMLDivElement>(null);
  const dragCleanup = React.useRef<(() => void) | null>(null);

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

  // 打开战果面板 = 已读：把后端未读游标推到现在（新战果红点回零）。
  // 失败不影响展示：游标只是显示层的东西，不阻塞任何判定。
  React.useEffect(() => {
    briefapi.markSeen(taskId).catch(() => void 0);
  }, [taskId]);

  React.useEffect(() => {
    if (!auto) return;
    const t = setInterval(load, 10000);
    return () => clearInterval(t);
  }, [auto, load]);

  // 卸载时收掉可能残留的拖拽监听
  React.useEffect(() => () => dragCleanup.current?.(), []);

  const rows = React.useMemo<LootRow[]>(() => [...(data?.rows ?? []), ...extra], [data, extra]);
  const aiKeys = React.useMemo(() => new Set(extra.map((r) => r.key)), [extra]);
  const row = selected ? rows.find((r) => r.key === selected) ?? null : null;

  // 数据刷新后选中的行没了 → 收起详情
  React.useEffect(() => {
    if (selected && !rows.some((r) => r.key === selected)) setSelected(null);
  }, [rows, selected]);

  // 首次升起时给一个默认高度：容器 45%
  React.useEffect(() => {
    if (!selected || detailH !== null) return;
    const h = rootRef.current?.getBoundingClientRect().height || 520;
    setDetailH(Math.max(MIN_H, Math.round(h * 0.45)));
  }, [selected, detailH]);

  // 拖拽分隔条：pointer events 自己实现，不引依赖
  function onGripPointerDown(e: React.PointerEvent<HTMLDivElement>) {
    e.preventDefault();
    const root = rootRef.current;
    if (!root) return;
    const startY = e.clientY;
    const startH = detailRef.current?.getBoundingClientRect().height ?? MIN_H;
    const move = (ev: PointerEvent) => {
      const maxH = Math.max(MIN_H, root.getBoundingClientRect().height * 0.7);
      const h = Math.max(MIN_H, Math.min(maxH, startH - (ev.clientY - startY)));
      setDetailH(Math.round(h));
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      window.removeEventListener("pointercancel", up);
      document.body.style.userSelect = "";
      dragCleanup.current = null;
    };
    document.body.style.userSelect = "none";
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
    window.addEventListener("pointercancel", up);
    dragCleanup.current = up;
  }

  function pick(r: LootRow) {
    if (selected === r.key) {
      // 再点同一行 → 收起
      setSelected(null);
      setShowEvidence(false);
      return;
    }
    setSelected(r.key);
    setShowEvidence(false);
  }

  async function copy(text: string, okMsg = "已复制") {
    try {
      await navigator.clipboard.writeText(text);
      setMsg(okMsg);
    } catch {
      setMsg("复制失败，请手动选中");
    }
  }

  // L1：AI 说清楚（单行，按证据 hash 缓存）
  async function explain(r: LootRow) {
    setAiBusy(r.key);
    setAiErr((prev) => {
      const n = { ...prev };
      delete n[r.key];
      return n;
    });
    try {
      const res = await lootapi.explain(taskId, r.key);
      setAiText((prev) => ({ ...prev, [r.key]: res.text }));
    } catch {
      setAiErr((prev) => ({ ...prev, [r.key]: "AI 暂时不可用，用上面的原文即可" }));
    } finally {
      setAiBusy((cur) => (cur === r.key ? null : cur));
    }
  }

  // L2：AI 补这个站特有的项
  async function suggest() {
    setSuggestBusy(true);
    try {
      const res = await lootapi.suggest(taskId);
      setExtra((prev) => {
        const seen = new Set([...(data?.rows ?? []).map((x) => x.key), ...prev.map((x) => x.key)]);
        const add: LootRow[] = [];
        for (const r of res.rows ?? []) {
          if (seen.has(r.key)) continue;
          seen.add(r.key);
          add.push({ ...r, status: "blank" });
        }
        return [...prev, ...add];
      });
      setMsg("已补上 AI 推断的项（待确认，不计入已拿到）");
    } catch {
      setMsg("AI 暂时不可用，稍后再试");
    } finally {
      setSuggestBusy(false);
    }
  }

  // 人工标记：写库后重新拉一次，状态永远由后端算（不在前端改 status）
  async function markState(r: LootRow, next: "todo" | "na" | "") {
    setStateBusy(`${r.key}:${next || "clear"}`);
    try {
      await lootapi.setState(taskId, r.key, next);
      load();
      setMsg(
        next === "na"
          ? `已把「${r.label}」标为不适用（不计入分母，刷新后仍在）`
          : next === "todo"
            ? `已把「${r.label}」标为还没试（刷新后仍在）`
            : `已取消「${r.label}」的标记`,
      );
    } catch {
      setMsg("标记没写成，稍后再试");
    } finally {
      setStateBusy(null);
    }
  }

  const evidence = row?.evidence ?? [];
  const plainText = row?.plain || (row ? ST[row.status].label : "");
  // 下一步：优先用后端确定性模板生成的 next_prompt（可直接粘贴到主会话）
  const nextStep = row?.next_prompt ?? row?.hint ?? "";
  const nextActionText = row ? (row.next_prompt ?? row.hint ?? row.plain ?? row.label) : "";

  return (
    <div ref={rootRef} className="flex h-full min-h-0 flex-col">
      {/* 头部：战果清单 + done/total + 关闭 */}
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <span className="flex size-5 items-center justify-center rounded bg-emerald-500 text-[10px] font-bold text-white">得</span>
        <div className="min-w-0 flex-1">
          <div className="text-[12.5px] font-semibold">战果清单</div>
          <div className="truncate text-[10px] text-muted-foreground">
            {data?.goal ? `目标：${data.goal}` : "实时统计 · 只列与这个站相关的项"}
          </div>
        </div>
        <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10.5px] font-semibold text-emerald-600">
          {data ? `${data.done}/${data.total || data.rows.length}` : "…"}
        </span>
        <button
          className="shrink-0 rounded border border-sky-500/40 bg-sky-500/5 px-1.5 py-0.5 text-[10.5px] font-medium text-sky-700 hover:bg-sky-500/10 dark:text-sky-300"
          onClick={() => router.push(`/function/tasks/detail?id=${taskId}&tab=overview`)}
          title="打开战况卡：我在哪 / 我有什么 / 能换到什么 / 能做哪几件事"
        >
          详情看战况 →
        </button>
        <button className="text-muted-foreground hover:text-foreground" onClick={onClose} aria-label="关闭">
          <XIcon className="size-4" />
        </button>
      </div>

      {/* 工具行：⟳ 刷新 / 自动刷新开关 / ✦ 补站内特有项 / 更新时间 */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-1.5">
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
        <button
          className="inline-flex items-center gap-1 rounded-md border border-violet-500/40 bg-violet-500/5 px-2 py-0.5 text-[10.5px] font-medium text-violet-600 disabled:opacity-50"
          onClick={suggest}
          disabled={suggestBusy}
          title="让 AI 猜这个站特有的能力项（待确认，不计入已拿到）"
        >
          {suggestBusy ? <Loader2Icon className="size-3 animate-spin" /> : <SparklesIcon className="size-3" />}
          ✦ 补这个站特有的项
        </button>
        <span className="ml-auto text-[10px] text-muted-foreground">{stamp}</span>
      </div>

      {msg && <div className="shrink-0 border-b bg-muted/40 px-3 py-1 text-[10.5px] text-muted-foreground">{msg}</div>}

      {/* 清单区：flex-1 min-h-0 overflow-auto */}
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
                <th className="w-[86px] px-1 py-1.5">状态</th>
                <th className="w-[74px] px-2 py-1.5">依据</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const on = selected === r.key;
                const st = ST[r.status] ?? ST.blank;
                const isAi = aiKeys.has(r.key);
                return (
                  <tr
                    key={r.key}
                    className={cn(
                      "cursor-pointer border-b align-top transition-colors",
                      on ? "bg-blue-500/[0.06] shadow-[inset_3px_0_0_#3b82f6]" : "hover:bg-accent/40",
                    )}
                    onClick={() => pick(r)}
                  >
                    <td className="px-3 py-1.5">
                      {/* 主行说白话（plain），原始归类名降为次要行：零基础看得懂“拿到了什么”而不是“哪一类能力”。 */}
                      <div className="font-medium leading-snug">{r.plain || r.label}</div>
                      {r.plain && <div className="mt-0.5 text-[10px] text-muted-foreground">{r.label}</div>}
                    </td>
                    <td className="px-1 py-1.5">
                      <span className={cn("inline-flex items-center gap-0.5 rounded px-1 py-0.5 text-[10px] font-semibold", st.cls)}>
                        {r.status === "done" && !isAi && <CheckIcon className="size-3" />}
                        {isAi ? "AI 推断·待确认" : st.label}
                      </span>
                    </td>
                    <td className="px-2 py-1.5 text-muted-foreground">
                      {(r.evidence ?? []).length > 0 ? (r.evidence ?? []).map((e) => `#${e.id}`).join(" ") : "—"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>

      {/* 详情区：默认不渲染；点行升起，再点同一行收起 */}
      {row && (
        <div
          ref={detailRef}
          className="flex max-h-[70%] min-h-[120px] shrink-0 flex-col overflow-hidden border-t bg-muted/20"
          style={{ height: detailH !== null ? `${detailH}px` : "45%" }}
        >
          {/* 可拖拽分隔条 */}
          <div
            className="h-1.5 shrink-0 cursor-ns-resize border-y bg-muted-foreground/20 transition-colors hover:bg-blue-500/40 active:bg-blue-500/60"
            onPointerDown={onGripPointerDown}
            role="separator"
            aria-orientation="horizontal"
            aria-label="拖动调整详情区高度"
            title="拖动调整高度（120px ~ 70%）"
          />
          <div className="flex shrink-0 items-center gap-2 border-b bg-card px-3 py-1.5">
            <span className="min-w-0 truncate text-[12px] font-semibold">{row.label}</span>
            <span className={cn("rounded px-1 py-0.5 text-[10px] font-semibold", ST[row.status].cls)}>
              {aiKeys.has(row.key) ? "AI 推断·待确认" : ST[row.status].label}
            </span>
            <span className="ml-auto text-[10px] text-muted-foreground">
              {row.status === "done"
                ? "这一行已确认"
                : row.status === "run"
                  ? "worker 正在试"
                  : row.status === "tried"
                    ? "试过，没成"
                    : row.status === "na"
                      ? "你标的：不适用"
                      : row.status === "todo"
                        ? "你标的：还没试"
                        : "AI 推断，待确认"}
            </span>
          </div>

          <div className="min-h-0 flex-1 overflow-auto px-3 py-2 text-[11.5px] leading-relaxed">
            {/* 一句话 */}
            <section className="mb-2">
              <h5 className="mb-1 text-[10px] font-semibold tracking-wide text-muted-foreground">一句话</h5>
              {aiBusy === row.key ? (
                <span className="text-violet-600">✦ AI 正在把证据翻成人话…</span>
              ) : (
                <>
                  {aiErr[row.key] && <div className="text-[11px] text-rose-600">{aiErr[row.key]}</div>}
                  <div className="text-[11.5px]">
                    {aiText[row.key] && (
                      <span className="mr-1 rounded bg-violet-500/15 px-1 text-[9.5px] font-bold text-violet-600">AI</span>
                    )}
                    {aiText[row.key] ?? plainText}
                  </div>
                </>
              )}
            </section>

            {/* 具体能拿什么 */}
            <section className="mb-2">
              <h5 className="mb-1 text-[10px] font-semibold tracking-wide text-muted-foreground">具体能拿什么</h5>
              {(row.objects ?? []).length > 0 ? (
                <div className="flex flex-col gap-1">
                  {(row.objects ?? []).map((o, i) => (
                    <div key={`${o.name}-${i}`} className="flex items-baseline gap-1.5 rounded border bg-card px-2 py-1">
                      <b className="text-[11px] font-semibold">{o.name}</b>
                      <span className="text-[10.5px] text-muted-foreground">{o.fields}</span>
                    </div>
                  ))}
                </div>
              ) : (
                <div className="text-[11px] text-muted-foreground">{row.why || "证据里还没抽出具体表名/字段"}</div>
              )}
            </section>

            {/* 依据 */}
            <section className="mb-2">
              <h5 className="mb-1 text-[10px] font-semibold tracking-wide text-muted-foreground">依据</h5>
              <div className="flex flex-wrap items-center gap-1.5">
                {evidence.length > 0 ? (
                  evidence.map((e) => (
                    <span key={e.id} className="rounded bg-muted px-1.5 py-0.5 text-[10.5px] text-muted-foreground">
                      #{e.id}
                    </span>
                  ))
                ) : (
                  <span className="text-[11px] text-muted-foreground">暂无证据编号</span>
                )}
                <button
                  className="ml-auto rounded border px-1.5 py-0.5 text-[10.5px] hover:bg-accent"
                  onClick={() => setShowEvidence((v) => !v)}
                  disabled={evidence.length === 0}
                >
                  {showEvidence ? "收起证据原文" : "看证据原文"}
                </button>
              </div>
              {showEvidence && evidence.length > 0 && (
                <div className="mt-1 flex flex-col gap-0.5 rounded border bg-card px-2 py-1">
                  {evidence.map((e) => (
                    <div key={e.id} className="text-[11px] leading-snug">
                      <span className="text-muted-foreground">#{e.id} · {e.kind}</span> {e.title}
                    </div>
                  ))}
                </div>
              )}
            </section>

            {/* 下一步：只读展示可直接粘贴的指令 */}
            <section className="mb-1">
              <h5 className="mb-1 text-[10px] font-semibold tracking-wide text-muted-foreground">下一步</h5>
              {nextStep ? (
                <>
                  <pre className="max-h-[4.5rem] overflow-auto whitespace-pre-wrap break-words rounded border bg-card px-2 py-1 text-[11px] leading-snug text-foreground/80">
                    {nextStep}
                  </pre>
                  <div className="mt-0.5 text-[10px] text-muted-foreground">点下面的按钮复制，粘到主 Agent 会话里发送即可。</div>
                </>
              ) : (
                <div className="text-[11.5px] text-muted-foreground">暂无建议</div>
              )}
            </section>
            {/* 人工标记：把这一行标成「还没试」或「不适用」，写库，刷新后仍在 */}
            {!aiKeys.has(row.key) && (
              <section className="mb-1 flex flex-wrap items-center gap-1.5">
                <span className="text-[10px] text-muted-foreground">这一项：</span>
                <button
                  className={cn(
                    "rounded border px-1.5 py-0.5 text-[10.5px] hover:bg-accent disabled:opacity-45",
                    row.status === "todo" && "border-slate-400 bg-slate-500/10",
                  )}
                  onClick={() => markState(row, row.status === "todo" ? "" : "todo")}
                  disabled={stateBusy !== null}
                  title="标成「还没试」：这一行会一直显示，提醒你还有这一项没动"
                >
                  {stateBusy === `${row.key}:todo` ? "…" : row.status === "todo" ? "取消「还没试」" : "标为还没试"}
                </button>
                <button
                  className={cn(
                    "rounded border px-1.5 py-0.5 text-[10.5px] hover:bg-accent disabled:opacity-45",
                    row.status === "na" && "border-slate-400 bg-slate-500/10",
                  )}
                  onClick={() => markState(row, row.status === "na" ? "" : "na")}
                  disabled={stateBusy !== null}
                  title="标成「不适用」：这个站没有这一类东西，不再计入分母"
                >
                  {stateBusy === `${row.key}:na` ? "…" : row.status === "na" ? "取消「不适用」" : "标为不适用"}
                </button>
              </section>
            )}
          </div>

          {/* 三个动作：复制指令 / 看证据 / AI 说清楚 */}
          <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-t bg-card px-3 py-2">
            <button
              className="inline-flex items-center gap-1 rounded-md border border-emerald-600/40 bg-emerald-600 px-2 py-1 text-[11px] font-medium text-white hover:bg-emerald-700 disabled:opacity-45"
              onClick={() => copy(nextActionText, "已复制，粘到上面「主 Agent 会话」里发送即可")}
              disabled={!nextActionText}
              title="只复制，不会自动发给 AI 或任务"
            >
              <ClipboardCopyIcon className="size-3" />
              {row.status === "tried" ? '复制"换个手法再试"的指令' : "复制下一步指令"}
            </button>
            <button
              className="rounded-md border px-2 py-1 text-[11px] hover:bg-accent disabled:opacity-45"
              onClick={() => setShowEvidence((v) => !v)}
              disabled={evidence.length === 0}
            >
              看证据原文
            </button>
            <button
              className="ml-auto inline-flex items-center gap-1 rounded-md border border-violet-500/40 bg-violet-500/5 px-2 py-1 text-[11px] font-semibold text-violet-600 hover:bg-violet-500/10 disabled:opacity-50"
              onClick={() => explain(row)}
              disabled={aiBusy === row.key}
            >
              {aiBusy === row.key ? <Loader2Icon className="size-3 animate-spin" /> : <SparklesIcon className="size-3" />}
              ✦ 用 AI 说清楚
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
