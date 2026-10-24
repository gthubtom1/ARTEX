"use client";

import * as React from "react";

import {
  ChevronRightIcon,
  ClipboardCopyIcon,
  CrosshairIcon,
  ListChecksIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
  SparklesIcon,
  TargetIcon,
} from "lucide-react";
import { toast } from "sonner";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { briefapi, chainapi, impactapi, verdictapi } from "@/lib/api";
import type {
  BriefAction,
  BriefItem,
  ChainView,
  ImpactResponse,
  ImpactStep,
  TaskBrief,
  TaskVerdict,
} from "@/lib/types";
import { cn } from "@/lib/utils";

// 战况卡（docs/25 §五，一屏一问）
//
// 主视线只回答一个问题：**我现在能做什么？**
//   ① 我在哪（等级 + 已确认几个洞）        —— 一行
//   ② 我有什么（最多 3 个胶囊）            —— 一行
//   ③ 现在能做到（最多 3 条 + [就做这个]） —— 这里是主视线
//   ④ 差一步（一行：几个洞、缺什么）
//   ⑤ 换不到（一行：几个洞）
//   底：固定说明 + `✦ 分析`（主视线唯一的 AI 按钮）
//
// 其余（全部洞 / 全部后果 / 利用链 / 越界动作 / 判定依据）全部折叠，标题带数量。
//
// 一条铁律：AI 只写措辞，不改判定。能不能做（doable）成没成立（standing）都由后端规则算。
// 「一盏灯」（docs/24 §九）：`✦ 分析` 是开关（花钱、有缓存），`⟳` 是免费重排；
// 点 `✦` 会通知会话页的短语一起刷新（window 事件），做不到的地方就明确说"还没分析"。

type FoldProps = {
  title: React.ReactNode;
  defaultOpen?: boolean;
  children: React.ReactNode;
};

// Fold 是这张卡里唯一的"藏东西"方式：默认收起，标题上写数量。
function Fold({ title, defaultOpen = false, children }: FoldProps) {
  const [open, setOpen] = React.useState(defaultOpen);
  return (
    <div className="rounded border bg-card/60">
      <button
        type="button"
        className="flex w-full items-center gap-1 px-2 py-1 text-left text-[11px] text-muted-foreground hover:bg-accent/40"
        onClick={() => setOpen((v) => !v)}
      >
        <ChevronRightIcon className={cn("size-3 transition-transform", open && "rotate-90")} />
        {title}
      </button>
      {open && <div className="border-t px-2 py-1.5">{children}</div>}
    </div>
  );
}

const LEVEL_HINT: Record<number, string> = {
  0: "只有信息（指纹/端口/入口清单），还没拿到任何权限",
  1: "有有效凭据（账号/哈希/连接串），但还没验证可用",
  2: "应用控制：能进后台、越权读写数据",
  3: "命令执行：能在服务器上跑命令（webshell / 反弹 shell）",
  4: "数据访问：已经读到业务数据（样本级）",
  5: "服务器控制：能控制整台机器，可内网横移",
};

const ITEM_ICON: Record<string, string> = {
  app: "后台",
  os: "立足点",
  shell: "命令",
  data: "数据",
  cred: "凭据",
};

function CopyBtn({ text, label = "复制" }: { text: string; label?: string }) {
  return (
    <button
      type="button"
      className="inline-flex shrink-0 items-center gap-1 rounded border px-1.5 py-0.5 text-[10.5px] text-muted-foreground hover:bg-accent hover:text-foreground"
      title="只复制，不会自动发给 AI"
      onClick={async () => {
        try {
          await navigator.clipboard.writeText(text);
          toast.success("已复制，粘到主会话里发送即可");
        } catch {
          toast.error("复制失败，请手动选中");
        }
      }}
    >
      <ClipboardCopyIcon className="size-3" />
      {label}
    </button>
  );
}

export function BattleCard({ taskId, onOpenLoot }: { taskId: string; onOpenLoot?: () => void }) {
  const [brief, setBrief] = React.useState<TaskBrief | null>(null);
  const [verdict, setVerdict] = React.useState<TaskVerdict | null>(null);
  const [impact, setImpact] = React.useState<ImpactResponse | null>(null);
  const [chains, setChains] = React.useState<ChainView[]>([]);
  const [aiBusy, setAiBusy] = React.useState(false);
  const [busy, setBusy] = React.useState(false);

  const load = React.useCallback(async () => {
    const [b, v, im, ch] = await Promise.all([
      briefapi.get(taskId).catch(() => null),
      verdictapi.get(taskId).catch(() => null),
      impactapi.get(taskId).catch(() => null),
      chainapi.get(taskId).catch(() => null),
    ]);
    if (b) setBrief(b);
    if (v) setVerdict(v);
    if (im) setImpact(im);
    if (ch) setChains(ch.chains ?? []);
  }, [taskId]);

  React.useEffect(() => {
    void load();
    const t = setInterval(() => void load(), 20000);
    return () => clearInterval(t);
  }, [load]);

  const refresh = React.useCallback(async () => {
    setBusy(true);
    try {
      await load();
    } finally {
      setBusy(false);
    }
  }, [load]);

  // ✦ 分析 = 点亮这盏灯（花钱、有缓存）：洞解释 + 站点特有后果 + 通知短语一起刷新
  const analyze = React.useCallback(async () => {
    setAiBusy(true);
    try {
      const res = await briefapi.explainAI(taskId);
      setBrief((prev) => (prev ? { ...prev, can: res.can } : prev));
      // 短语组件监听这个事件做一次免费重排（同一盏灯，多个开关）
      if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent("artex:analyze"));
      if (res.filled > 0) {
        toast.success(`AI 分析完成：${res.filled} 条（已按证据缓存，下次不再花 token）`);
      } else if (res.asked === 0) {
        toast.success("这几条都已经分析过了（命中缓存，没花钱）");
      } else {
        toast.error("模型这次没给出可用结果，先看规则版");
      }
    } catch {
      toast.error("AI 暂时不可用，先看规则版（不影响其它）");
    } finally {
      setAiBusy(false);
    }
  }, [taskId]);

  const has: BriefItem[] = brief?.has ?? [];
  const can = React.useMemo(() => {
    const rank: Record<string, number> = { got: 0, one_step: 1, no_gain: 2 };
    return [...(brief?.can ?? [])].sort(
      (a, b) => (rank[a.standing ?? "one_step"] ?? 1) - (rank[b.standing ?? "one_step"] ?? 1),
    );
  }, [brief?.can]);
  const actions: BriefAction[] = brief?.actions ?? [];
  const aiCount = can.filter((c) => c.source === "ai").length;
  const gotCount = can.filter((c) => c.standing === "got").length;
  const stepCan = can.filter((c) => c.standing === "one_step");
  const noGainCan = can.filter((c) => c.standing === "no_gain");
  const doable = actions.filter((a) => a.doable);
  const shownDoable = doable.slice(0, 3);
  const level = brief?.level ?? 0;
  const verdictTone =
    verdict?.verdict === "worth"
      ? "text-emerald-700 dark:text-emerald-300"
      : verdict?.verdict === "not_worth"
        ? "text-rose-700 dark:text-rose-300"
        : "text-amber-700 dark:text-amber-300";
  const openChains = (chains ?? []).filter((c) => c.status !== "dead");
  const steps: ImpactStep[] = impact?.steps ?? [];

  return (
    <Card className="border-sky-500/40 bg-sky-500/[0.04]">
      {/* ── 抬头：等级 + 值不值得打 + 战果红点 + ✦分析 + 刷新 ── */}
      <CardHeader className="pb-1.5">
        <div className="flex flex-wrap items-center gap-2">
          <span className="flex size-5 items-center justify-center rounded bg-sky-600 text-[10px] font-bold text-white">
            战
          </span>
          <CardTitle className="text-base">战况</CardTitle>
          {verdict && (
            <span className={cn("rounded border px-1.5 py-0.5 text-[11px] font-medium", verdictTone)}>
              {verdict.label}
            </span>
          )}
          {brief && brief.new_since > 0 && (
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded-full border border-rose-500/40 bg-rose-500/10 px-1.5 py-0.5 text-[10.5px] font-semibold text-rose-600 hover:bg-rose-500/20"
              onClick={() => onOpenLoot?.()}
              title="上次看战果之后新拿到的条目：点开战果面板就算看过"
            >
              <span className="size-1.5 rounded-full bg-rose-500" />
              战果 +{brief.new_since}
            </button>
          )}
          {/* 主视线唯一的 AI 按钮 */}
          <button
            type="button"
            className="ml-auto inline-flex items-center gap-1 rounded border border-violet-500/40 bg-violet-500/10 px-2 py-0.5 text-[11px] font-semibold text-violet-700 hover:bg-violet-500/20 disabled:opacity-50 dark:text-violet-300"
            onClick={() => void analyze()}
            disabled={aiBusy || can.length === 0}
            title="让 AI 分析：这个站现在真能做什么（按证据缓存，重复点不花钱；不会替你执行任何动作）"
          >
            {aiBusy ? <RefreshCwIcon className="size-3 animate-spin" /> : <SparklesIcon className="size-3" />}
            {aiCount > 0 ? "✦ 重新分析" : "✦ 分析"}
          </button>
          <button
            type="button"
            className="inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10.5px] text-muted-foreground hover:bg-accent"
            onClick={() => void refresh()}
            disabled={busy}
            title="免费：重新读一遍当前状态（不调模型）"
          >
            <RefreshCwIcon className={cn("size-3", busy && "animate-spin")} /> 刷新
          </button>
        </div>
      </CardHeader>

      <CardContent className="flex flex-col gap-2.5 text-[11.5px]">
        {/* 空态（docs/26 P2）：什么都还没有时，不要显示一堆 0 和空分区，只给一句话 + 下一步 */}
        {brief && can.length === 0 && has.length === 0 && doable.length === 0 && (
          <section className="rounded border border-dashed px-3 py-3 text-center">
            <p className="text-[12px] font-medium">还没确认任何漏洞，所以现在什么都做不到</p>
            <p className="mt-1 text-muted-foreground">
              先跑①「判断值不值得打」（20 分钟只读）或③「找漏洞并取证」（40 分钟），把入口摸出来再说；
              拿到第一个洞后，这里会自动变成"我在哪 / 我有什么 / 现在能做到"。
            </p>
          </section>
        )}
        {/* ① 我在哪 */}
        <section className="flex flex-wrap items-center gap-1.5">
          <span className="text-[10px] text-muted-foreground">我在哪</span>
          <b className="text-[13px]">{brief?.level_label ?? "读取中…"}</b>
          <span className="flex items-center gap-0.5" title="E0 仅有信息 → E5 服务器控制">
            {[0, 1, 2, 3, 4, 5].map((i) => (
              <span
                key={i}
                className={cn("size-1.5 rounded-full", i <= level ? "bg-sky-600" : "bg-muted-foreground/25")}
              />
            ))}
          </span>
          <span className="text-muted-foreground">
            {brief?.basis ?? ""}
            {can.length > 0 ? ` · 已确认 ${can.length} 个洞` : ""}
          </span>
        </section>
        <p className="-mt-1 text-[10.5px] text-muted-foreground">{LEVEL_HINT[level] ?? ""}</p>

        {/* 继承 ≠ 本任务已自证（真机跑 #18 时用户的疑问）：显式写清"我从谁那儿继承了啥" */}
        {(brief?.inherited ?? []).length > 0 && (
          <section className="rounded border border-sky-500/30 bg-sky-500/[0.05] px-2 py-1 text-[10.5px]">
            <span className="text-muted-foreground">继承自：</span>
            {(brief?.inherited ?? []).map((it) => (
              <span key={it.task_id} className="mr-2 inline-flex flex-wrap items-center gap-1">
                <b>#{it.task_id}</b>
                {it.name ? <span className="opacity-70">{it.name}</span> : null}
                <span className="rounded bg-muted px-1">{it.level_label}</span>
                {it.findings > 0 ? <span className="opacity-70">{it.findings} 个洞</span> : null}
                {(it.has ?? []).length > 0 ? <span className="opacity-70">已拿到 {it.has?.join("/")}</span> : null}
              </span>
            ))}
            <span className="text-amber-700 dark:text-amber-300">
              · 这些是只读参考：本任务的等级只算它自己验证过的证据
            </span>
          </section>
        )}

        {/* 闸门2：没进展时直说（warn 黄 / stop 红），并给"该说什么" */}
        {brief?.stall && brief.stall.level !== "ok" && (
          <section
            className={cn(
              "flex flex-wrap items-center gap-1.5 rounded border px-2 py-1.5",
              brief.stall.level === "stop"
                ? "border-rose-500/40 bg-rose-500/10 text-rose-700 dark:text-rose-300"
                : "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-300",
            )}
          >
            <span className="rounded bg-current/15 px-1 text-[9.5px] font-bold">
              {brief.stall.level === "stop" ? "建议收工" : "没进展"}
            </span>
            <span className="min-w-0 flex-1">{brief.stall.advice}</span>
            <CopyBtn
              text={
                brief.stall.level === "stop"
                  ? "用当前已有的证据出报告并停，别再试了"
                  : "现在进展在哪？给我一句：继续、换路线、还是收工"
              }
              label="复制一句"
            />
          </section>
        )}

        {/* ② 我有什么（最多 3 个 + 还有 N） */}
        <section className="flex flex-wrap items-center gap-1">
          <span className="text-[10px] text-muted-foreground">我有什么</span>
          {has.length === 0 && <span className="text-muted-foreground">还没有可直接利用的权限</span>}
          {has.slice(0, 3).map((h, i) => (
            <span
              key={`${h.kind}-${i}`}
              className="inline-flex items-center gap-1 rounded border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-emerald-700 dark:text-emerald-300"
            >
              <b className="text-[9.5px] font-bold opacity-70">{ITEM_ICON[h.kind] ?? h.kind}</b>
              {h.text}
            </span>
          ))}
          {has.length > 3 && <span className="text-[10px] text-muted-foreground">还有 {has.length - 3} 项</span>}
        </section>

        {/* ③ 现在能做到（主视线，最多 3 条） */}
        <section className="rounded border border-emerald-500/30 bg-emerald-500/[0.05] px-2 py-1.5">
          <div className="mb-1 flex items-center gap-1">
            <TargetIcon className="size-3 text-emerald-600" />
            <b className="text-[11.5px]">现在能做到</b>
            <span className="text-[10px] text-muted-foreground">
              {doable.length > 0
                ? `（${doable.length} 件有前置、我能去做；每条都能挂着证据查）`
                : "（现在一件都做不了 —— 前置还没满足）"}
            </span>
          </div>
          {shownDoable.length === 0 && (
            <p className="text-muted-foreground">
              还没有一件能做的 —— 先把它打成能读数据/能进后台，下面的「差一步」会告诉你缺什么。
            </p>
          )}
          <ul className="flex flex-col gap-1">
            {shownDoable.map((a) => (
              <li key={a.class} className="flex flex-wrap items-center gap-1.5">
                <span className="font-medium">{a.title}</span>
                {a.run_state === "requested" && (
                  <span className="rounded bg-sky-500/15 px-1 text-[9.5px] text-sky-700 dark:text-sky-300">
                    已登记·等结果
                  </span>
                )}
                {a.run_state === "done" && (
                  <span
                    className="rounded bg-emerald-500/20 px-1 text-[9.5px] text-emerald-700 dark:text-emerald-300"
                    title={a.run_evidence || ""}
                  >
                    已完成
                  </span>
                )}
                <CopyBtn text={a.say_it} label="就做这个" />
              </li>
            ))}
          </ul>
          {doable.length > 3 && (
            <p className="mt-0.5 text-[10px] text-muted-foreground">还有 {doable.length - 3} 件，见下面折叠区</p>
          )}
        </section>

        {/* ④ 差一步 / ⑤ 换不到：各一行，不铺开 */}
        {stepCan.length > 0 && (
          <section className="flex flex-wrap items-center gap-1 text-amber-700 dark:text-amber-300">
            <span className="rounded bg-amber-500/15 px-1 text-[9.5px] font-bold">差一步</span>
            <span>
              {stepCan.length} 个洞差一步就能换到东西
              {stepCan[0]?.gap ? ` —— 缺：${stepCan[0].gap}` : ""}
            </span>
          </section>
        )}
        {noGainCan.length > 0 && (
          <section className="flex flex-wrap items-center gap-1 text-muted-foreground">
            <span className="rounded bg-muted px-1 text-[9.5px] font-bold">换不到</span>
            <span>{noGainCan.length} 个洞目前换不到任何现实后果（不是误报，只是没到能用的程度）</span>
          </section>
        )}

        {/* 一句话：想做就在会话里说 */}
        <section className="flex flex-wrap items-center gap-1.5 rounded border border-sky-500/30 bg-sky-500/[0.06] px-2 py-1.5">
          <span className="min-w-0 flex-1">
            {brief?.say_this || "想做上面哪一步，直接在下面主对话里说；我不会自己动手。"}
          </span>
          {actions.length > 0 && <CopyBtn text={actions[0].say_it} label="复制第一句" />}
        </section>
        <p className="text-[10px] text-muted-foreground">
          灰的是"可能清单"，只有拿到证据才会点亮；AI 只做解释，不替你决定，也不会自己动手。
        </p>

        {/* ── 折叠区（点开才看，标题带数量）── */}
        <div className="flex flex-col gap-1">
          <Fold title={`${can.length} 个洞 · 每个能换到什么${aiCount > 0 ? `（${aiCount} 条 AI 版）` : ""}`}>
            <ul className="flex flex-col gap-1.5">
              {can.map((c) => (
                <li key={`${c.finding_id}-${c.title}`} className="flex flex-wrap items-start gap-1.5">
                  <span
                    className={cn(
                      "mt-[1px] shrink-0 rounded px-1 text-[9.5px] font-bold",
                      c.standing === "got"
                        ? "bg-emerald-600 text-white"
                        : c.standing === "no_gain"
                          ? "bg-slate-500/25 text-slate-600 dark:text-slate-300"
                          : "bg-amber-500/20 text-amber-700 dark:text-amber-300",
                    )}
                    title={
                      c.standing === "got"
                        ? "已成立：对应能力行已拿到，有证据"
                        : c.standing === "no_gain"
                          ? "换不到东西：这条洞没有通向任何能力行"
                          : `差一步：还缺「${c.gap || "下一个能力"}」`
                    }
                  >
                    {c.standing === "got" ? "已成立" : c.standing === "no_gain" ? "换不到" : "差一步"}
                  </span>
                  <b className="shrink-0 font-medium">{c.title}</b>
                  {c.source === "ai" && (
                    <span
                      className="mt-[1px] shrink-0 rounded bg-violet-500/15 px-1 text-[9px] font-bold text-violet-600"
                      title={c.cached ? "AI 解释（命中缓存，没花 token）" : "AI 解释"}
                    >
                      AI
                    </span>
                  )}
                  <span className="min-w-0 text-muted-foreground">
                    → {c.grants}
                    {c.standing === "one_step" && c.gap ? `（缺：${c.gap}）` : ""}
                  </span>
                  <span className="ml-auto shrink-0 font-mono text-[10px] text-muted-foreground">#{c.finding_id}</span>
                </li>
              ))}
              {can.length === 0 && <p className="text-muted-foreground">还没确认任何漏洞。</p>}
            </ul>
          </Fold>

          <Fold title={`${actions.length} 件能做到的事（含前置与留证要求）`}>
            <div className="grid grid-cols-1 gap-1.5 md:grid-cols-2">
              {actions.map((a) => (
                <div
                  key={a.class}
                  className={cn(
                    "rounded-md border px-2.5 py-2",
                    a.doable
                      ? "border-emerald-500/50 bg-emerald-500/[0.07] shadow-sm"
                      : "border-border bg-muted/30 opacity-70",
                  )}
                >
                  <div className="flex flex-wrap items-center gap-1">
                    <span className="font-medium">{a.title}</span>
                    <span
                      className={cn(
                        "rounded px-1 text-[9.5px] font-semibold",
                        a.doable ? "bg-emerald-600 text-white" : "bg-slate-500/20 text-slate-600 dark:text-slate-300",
                      )}
                    >
                      {a.doable ? "现在能做到" : "还做不到"}
                    </span>
                    <span className="rounded bg-amber-500/15 px-1 text-[9.5px] font-semibold text-amber-700 dark:text-amber-300">
                      不自动执行
                    </span>
                  </div>
                  <div className="mt-0.5 text-muted-foreground">前置：{a.need}</div>
                  <div className="text-foreground/70">想让我做就说：「{a.say_it}」</div>
                  {a.evidence_need && (
                    <div className="text-[10px] text-muted-foreground">做完要留证：{a.evidence_need}</div>
                  )}
                  {a.run_state === "requested" && (
                    <div className="text-[10.5px] text-sky-700 dark:text-sky-300">
                      已登记（等结果）{a.run_say_it ? `· 你说的是：「${a.run_say_it}」` : ""}
                    </div>
                  )}
                  {a.run_state === "done" && (
                    <div className="text-[10.5px] text-emerald-700 dark:text-emerald-300">
                      已完成{a.run_evidence ? ` · ${a.run_evidence}` : ""}
                    </div>
                  )}
                </div>
              ))}
            </div>
          </Fold>

          {openChains.length > 0 && (
            <Fold title={`${openChains.length} 条利用链在走`}>
              <div className="flex flex-col gap-1">
                {openChains.slice(0, 6).map((c) => (
                  <div key={c.key} className="rounded border px-2 py-1">
                    <div className="flex items-center gap-1.5">
                      <b className="font-medium">{c.title}</b>
                      <span
                        className={cn(
                          "rounded px-1.5 py-0.5 text-[10px] font-semibold",
                          c.status === "done"
                            ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300"
                            : "bg-amber-500/15 text-amber-700 dark:text-amber-300",
                        )}
                      >
                        {c.status === "done" ? "已拿到收益" : "进行中"}
                      </span>
                    </div>
                    <div className="mt-0.5 flex flex-wrap items-center gap-1">
                      {(c.steps ?? []).map((st) => (
                        <span
                          key={st.Key}
                          className={cn(
                            "rounded border px-1 py-0.5 text-[10px]",
                            st.Status === "done"
                              ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
                              : st.Status === "failed"
                                ? "border-slate-400/50 text-slate-500 line-through"
                                : "border-amber-500/40 text-amber-700 dark:text-amber-300",
                          )}
                          title={st.Title}
                        >
                          {st.Title.length > 22 ? `${st.Title.slice(0, 22)}…` : st.Title}
                        </span>
                      ))}
                    </div>
                    <div className="mt-0.5 text-muted-foreground">下一步：{c.next}</div>
                  </div>
                ))}
              </div>
            </Fold>
          )}

          {steps.length > 0 && (
            <Fold title={`${steps.length} 个越界动作（默认不做，要做就在对话里说）`}>
              <div className="grid grid-cols-1 gap-1.5 md:grid-cols-2">
                {steps.map((st) => {
                  const g = (impact?.grants ?? []).find((x) => x.class === st.class);
                  return (
                    <div
                      key={st.class}
                      className="rounded-md border bg-muted/20 px-2.5 py-2"
                      title={`为什么能做：${st.why}\n我会做什么：${st.plan}\n风险：${st.risk}`}
                    >
                      <div className="flex flex-wrap items-center gap-1">
                        <span className="font-medium">{st.title}</span>
                        <span className="text-muted-foreground">→ 做成升到 E{st.next}</span>
                        {st.ready === false && st.precondition && (
                          <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10.5px] text-amber-700 dark:text-amber-300">
                            还差前置：{st.precondition}
                          </span>
                        )}
                        {g?.granted ? (
                          <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10.5px] text-emerald-700 dark:text-emerald-300">
                            已在对话里授权（{g.decided_at}）
                          </span>
                        ) : g ? (
                          <span className="rounded bg-slate-500/15 px-1.5 py-0.5 text-[10.5px] text-slate-600">
                            已明确拒绝（{g.decided_at}）
                          </span>
                        ) : st.ready !== false ? (
                          <span className="rounded bg-muted px-1.5 py-0.5 text-[10.5px] text-muted-foreground">
                            未授权
                          </span>
                        ) : null}
                      </div>
                    </div>
                  );
                })}
              </div>
            </Fold>
          )}

          {verdict && (verdict.reasons?.length > 0 || verdict.surface?.length > 0) && (
            <Fold title={`为什么这么判（${verdict.reasons?.length ?? 0} 条理由）`}>
              <div className="flex flex-col gap-1">
                <p>{verdict.advice}</p>
                <ul className="flex flex-col gap-0.5">
                  {(verdict.reasons ?? []).map((r) => (
                    <li key={`${r.kind}-${r.id ?? 0}`} className="flex items-start gap-1.5">
                      <span className="mt-[3px] size-1.5 shrink-0 rounded-full bg-current opacity-40" />
                      <span>
                        {r.text}
                        {r.id ? <span className="ml-1 font-mono text-[10px] opacity-70">#{r.id}</span> : null}
                      </span>
                    </li>
                  ))}
                </ul>
                {(verdict.surface ?? []).length > 0 && (
                  <div className="flex flex-wrap gap-1">
                    {(verdict.surface ?? []).slice(0, 6).map((v) => (
                      <span
                        key={v.url}
                        className={cn(
                          "rounded border px-1 font-mono text-[10px]",
                          v.kind === "confirmed"
                            ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300"
                            : "border-border bg-muted",
                        )}
                        title={v.note || "观测到的入口"}
                      >
                        {v.url.length > 42 ? `${v.url.slice(0, 42)}…` : v.url}
                      </span>
                    ))}
                  </div>
                )}
              </div>
            </Fold>
          )}

          {/* 注：原来这里还有一行"下一步最值得做"，与会话页顶部的「它下一步想做」重复，
              按 docs/26 审查结论去掉 —— 一句话只说一次。 */}
        </div>
      </CardContent>
    </Card>
  );
}
