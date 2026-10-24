"use client";

import * as React from "react";

import {
  ArrowUpRightIcon,
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

// 战况卡（P0-b）：任务总览顶部**唯一**一张卡。
//
// 一张卡要回答四个问题，用户不用在页面里找：
//   1) 我在哪（权限等级 + 值不值得打 + 下一步）
//   2) 我有什么（已拿到的后台/立足点/命令执行/数据/凭据）
//   3) 每一条漏洞能换到什么（规则版；P0-c 换成 AI 摘要）
//   4) 我现在能做哪几件事（业务动作库，state 恒为 idle —— 只展示，永不自动执行）
//
// 数据源：GET /api/tasks/{id}/brief（一次拿全，避免前端拼三张卡）。
// 利用链 / 越界授权不属于 brief（它们是"过程"和"人的决定"），仍然各拉一个接口，
// 但都收进这张卡里显示，页面上不再出现第二张同义卡。

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

function CardCopy({ text, label = "复制" }: { text: string; label?: string }) {
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
  const [showAllActions, setShowAllActions] = React.useState(false);
  // 「为什么这么判」默认收起：先给结论，要查证据再展开（不丢原来的理由/攻击面）。
  const [showWhy, setShowWhy] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  // P0-c：「能换到什么」让 AI 写（按证据 hash 缓存，失败回落规则版）
  const [aiBusy, setAiBusy] = React.useState(false);

  // 战况卡是纯规则计算，很便宜：进页面拉一次，之后 20s 刷。
  React.useEffect(() => {
    let alive = true;
    const load = () => {
      briefapi
        .get(taskId)
        .then((v) => alive && setBrief(v))
        .catch(() => void 0);
      verdictapi
        .get(taskId)
        .then((v) => alive && setVerdict(v))
        .catch(() => void 0);
      impactapi
        .get(taskId)
        .then((v) => alive && setImpact(v))
        .catch(() => void 0);
      chainapi
        .get(taskId)
        .then((v) => alive && setChains(v.chains ?? []))
        .catch(() => void 0);
    };
    load();
    const t = setInterval(load, 20000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, [taskId]);

  const refresh = React.useCallback(async () => {
    setBusy(true);
    try {
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
    } finally {
      setBusy(false);
    }
  }, [taskId]);

  const has: BriefItem[] = brief?.has ?? [];
  const can = React.useMemo(() => {
    const rank: Record<string, number> = { got: 0, one_step: 1, no_gain: 2 };
    return [...(brief?.can ?? [])].sort(
      (a, b) => (rank[a.standing ?? "one_step"] ?? 1) - (rank[b.standing ?? "one_step"] ?? 1),
    );
  }, [brief?.can]);
  const aiCount = can.filter((c) => c.source === "ai").length;
  const gotCount = can.filter((c) => c.standing === "got").length;
  const stepCount = can.filter((c) => c.standing === "one_step").length;
  const noGainCount = can.filter((c) => c.standing === "no_gain").length;
  const firstGap = can.find((c) => c.standing === "one_step")?.gap ?? "";

  const explainAI = React.useCallback(async () => {
    setAiBusy(true);
    try {
      const res = await briefapi.explainAI(taskId);
      setBrief((prev) => (prev ? { ...prev, can: res.can } : prev));
      if (res.filled > 0) {
        toast.success(`AI 写好了 ${res.filled} 条（已按证据缓存，下次不再花 token）`);
      } else if (res.asked === 0) {
        toast.success("这几条都已经有 AI 解释了（命中缓存）");
      } else {
        toast.error("模型这次没给出可用结果，先看规则版");
      }
    } catch {
      toast.error("AI 暂时不可用，先看规则版（不影响其它）");
    } finally {
      setAiBusy(false);
    }
  }, [taskId]);
  const actions: BriefAction[] = brief?.actions ?? [];
  const level = brief?.level ?? 0;
  const shownActions = showAllActions ? actions : actions.slice(0, 7);
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
      <CardHeader className="pb-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="flex size-5 items-center justify-center rounded bg-sky-600 text-[10px] font-bold text-white">
            战
          </span>
          <CardTitle className="text-base">
            战况
            {brief ? (
              <span className="ml-1.5 font-normal">
                · <b className="text-sky-700 dark:text-sky-300">{brief.level_label}</b>
                <span className="ml-1 text-xs text-muted-foreground">{brief.basis}</span>
              </span>
            ) : (
              <span className="ml-1.5 text-xs font-normal text-muted-foreground">读取中…</span>
            )}
          </CardTitle>
          {verdict && (
            <span className={cn("rounded border px-1.5 py-0.5 text-[11px] font-medium", verdictTone)}>
              {verdict.label}
              <span className="ml-1 font-normal opacity-80">{verdict.headline}</span>
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
          <button
            type="button"
            className="ml-auto inline-flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10.5px] text-muted-foreground hover:bg-accent"
            onClick={() => void refresh()}
            disabled={busy}
          >
            <RefreshCwIcon className={cn("size-3", busy && "animate-spin")} /> 刷新
          </button>
        </div>
        <p className="mt-1 text-[11.5px] text-muted-foreground">
          {LEVEL_HINT[level] ?? "还没有结论：先跑②或③模板把入口摸全"}
        </p>
      </CardHeader>

      <CardContent className="flex flex-col gap-3 text-[11.5px]">
        {/* 2) 我有什么 */}
        <section>
          <h5 className="mb-1 flex items-center gap-1 text-[10px] font-semibold tracking-wide text-muted-foreground">
            <TargetIcon className="size-3" /> 我有什么
          </h5>
          {has.length > 0 ? (
            <div className="flex flex-wrap gap-1">
              {has.map((h, i) => (
                <span
                  key={`${h.kind}-${i}`}
                  className="inline-flex items-center gap-1 rounded border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-emerald-700 dark:text-emerald-300"
                >
                  <b className="text-[9.5px] font-bold opacity-70">{ITEM_ICON[h.kind] ?? h.kind}</b>
                  {h.text}
                </span>
              ))}
            </div>
          ) : (
            <p className="text-muted-foreground">还没有可直接利用的权限 —— 下面的 7 件事现在一件也做不到。</p>
          )}
        </section>

        {/* 3) 能换到什么（每条漏洞）：规则版立刻给，AI 版按证据 hash 缓存（P0-c） */}
        <section>
          <h5 className="mb-1 flex flex-wrap items-center gap-1 text-[10px] font-semibold tracking-wide text-muted-foreground">
            <CrosshairIcon className="size-3" /> 每个漏洞能换到什么
            {can.length > 0 && (
              <span className="font-normal opacity-80">
                （{gotCount} 个已成立 · {stepCount} 个差一步
                {stepCount > 0 && firstGap ? `（缺：${firstGap}）` : ""}
                {noGainCount > 0 ? ` · ${noGainCount} 个换不到` : ""} · {aiCount}/{can.length} AI 版）
              </span>
            )}
            {can.length > 0 && (
              <button
                type="button"
                className="ml-auto inline-flex items-center gap-1 rounded border border-violet-500/40 bg-violet-500/5 px-1.5 py-0.5 text-[10.5px] font-semibold text-violet-600 hover:bg-violet-500/10 disabled:opacity-50"
                onClick={() => void explainAI()}
                disabled={aiBusy}
                title="让 AI 把每条漏洞翻成人话（按证据 hash 缓存，证据不变就不重复花 token）"
              >
                {aiBusy ? <RefreshCwIcon className="size-3 animate-spin" /> : <SparklesIcon className="size-3" />}
                {aiCount > 0 && aiCount === can.length ? "✦ 重新解释" : "✦ 用 AI 说清楚"}
              </button>
            )}
          </h5>
          {can.length > 0 ? (
            <ul className="flex flex-col gap-1">
              {can.map((c) => (
                <li
                  key={`${c.finding_id}-${c.title}`}
                  className="flex items-start gap-1.5 rounded border bg-card px-2 py-1"
                >
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
                          ? "换不到东西：这条洞没有通向任何能力行（不是误报，只是没到能用的程度）"
                          : `差一步：还缺「${c.gap || "下一个能力"}」`
                    }
                  >
                    {c.standing === "got" ? "已成立" : c.standing === "no_gain" ? "换不到" : "差一步"}
                  </span>
                  <b className="shrink-0 font-medium">{c.title}</b>
                  <span className="flex min-w-0 items-start gap-1 text-muted-foreground">
                    {c.source === "ai" && (
                      <span
                        className="mt-[1px] shrink-0 rounded bg-violet-500/15 px-1 text-[9px] font-bold text-violet-600"
                        title={c.cached ? "AI 解释（命中缓存，没花 token）" : "AI 解释"}
                      >
                        AI
                      </span>
                    )}
                    <span>→ {c.grants}</span>
                  </span>
                  <span className="ml-auto shrink-0 font-mono text-[10px] text-muted-foreground">#{c.finding_id}</span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="text-muted-foreground">还没确认任何漏洞，所以暂时换不到东西。</p>
          )}
        </section>

        {/* 4) 能做哪几件事（业务动作库：只展示，永不自动执行） */}
        <section>
          <h5 className="mb-1 flex items-center gap-1 text-[10px] font-semibold tracking-wide text-muted-foreground">
            <SparklesIcon className="size-3" /> 我现在能做到的现实后果（{actions.length} 件，我不会自己动手）
          </h5>
          <div className="grid grid-cols-1 gap-1 md:grid-cols-2">
            {shownActions.map((a) => {
              return (
                <div
                  key={a.class}
                  className={cn(
                    "rounded border px-2 py-1",
                    a.doable
                      ? "border-emerald-500/50 bg-emerald-500/[0.07]"
                      : "border-border bg-card opacity-55 grayscale-[0.3]",
                  )}
                  title={a.doable ? "前置已满足：现在说一句就能做" : `还做不到，前置：${a.need}`}
                >
                  <div className="flex items-center gap-1">
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
                  <div className="mt-0.5 flex items-start gap-1 text-muted-foreground">
                    <span className="shrink-0 opacity-70">前置：{a.need}</span>
                    <CardCopy text={a.say_it} label="复制说法" />
                  </div>
                  {a.run_state === "requested" && (
                    <div className="mt-0.5 rounded bg-sky-500/10 px-1 text-[10.5px] text-sky-700 dark:text-sky-300">
                      已登记（等结果）{a.run_say_it ? `· 你说的是：「${a.run_say_it}」` : ""}
                    </div>
                  )}
                  {a.run_state === "done" && (
                    <div className="mt-0.5 rounded bg-emerald-500/10 px-1 text-[10.5px] text-emerald-700 dark:text-emerald-300">
                      已完成{a.run_evidence ? ` · ${a.run_evidence}` : ""}
                    </div>
                  )}
                  {(a.run_state === "failed" || a.run_state === "aborted") && (
                    <div className="mt-0.5 rounded bg-slate-500/10 px-1 text-[10.5px] text-slate-600">
                      {a.run_state === "failed" ? "失败了" : "已放弃"}
                      {a.run_evidence ? ` · ${a.run_evidence}` : ""}
                    </div>
                  )}
                  <div className="mt-0.5 text-[10.5px] text-foreground/70">想让我做就说：「{a.say_it}」</div>
                  {a.evidence_need && (
                    <div className="text-[10px] text-muted-foreground">做完要留证：{a.evidence_need}</div>
                  )}
                </div>
              );
            })}
          </div>
          {actions.length > 7 && (
            <button
              type="button"
              className="mt-1 rounded border px-1.5 py-0.5 text-[10.5px] text-muted-foreground hover:bg-accent"
              onClick={() => setShowAllActions((v) => !v)}
            >
              {showAllActions ? "只看前 7 件" : `还有 ${actions.length - 7} 件（展开）`}
            </button>
          )}
        </section>

        {/* 利用链：串联漏洞才是渗透本质（收进同一张卡，不再单独占一张） */}
        {openChains.length > 0 && (
          <section>
            <h5 className="mb-1 flex items-center gap-1 text-[10px] font-semibold tracking-wide text-muted-foreground">
              <ListChecksIcon className="size-3" /> 利用链（{openChains.length} 条在走）
            </h5>
            <div className="flex flex-col gap-1">
              {openChains.slice(0, 4).map((c) => (
                <div key={c.key} className="rounded border bg-card px-2 py-1">
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
                        {st.Title.length > 22 ? st.Title.slice(0, 22) + "…" : st.Title}
                      </span>
                    ))}
                  </div>
                  <div className="mt-0.5 text-muted-foreground">下一步：{c.next}</div>
                </div>
              ))}
            </div>
          </section>
        )}

        {/* 越界动作（P4）：不再放"允许/拒绝"按钮 —— 授权只剩一条路：在对话里说。
            这里只显示"它建议往哪走 + 你现在允许到哪一步"，按钮消失，说明保留一行。 */}
        {steps.length > 0 && (
          <section>
            <h5 className="mb-1 flex items-center gap-1 text-[10px] font-semibold tracking-wide text-muted-foreground">
              <ShieldAlertIcon className="size-3" /> 越界动作（默认不做；要做就在对话里说「帮我…」）
            </h5>
            <div className="flex flex-col gap-1">
              {steps.map((st) => {
                const g = (impact?.grants ?? []).find((x) => x.class === st.class);
                return (
                  <div
                    key={st.class}
                    className="flex flex-wrap items-center gap-1 rounded border bg-card px-2 py-1"
                    title={`为什么能做：${st.why}
我会做什么：${st.plan}
风险：${st.risk}`}
                  >
                    <span className="font-medium">{st.title}</span>
                    <span className="text-muted-foreground">→ 做成升到 E{st.next}</span>
                    {g?.granted ? (
                      <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10.5px] font-medium text-emerald-700 dark:text-emerald-300">
                        已在对话里授权（{g.decided_at}）
                      </span>
                    ) : g ? (
                      <span className="rounded bg-slate-500/15 px-1.5 py-0.5 text-[10.5px] font-medium text-slate-600">
                        已明确拒绝（{g.decided_at}）
                      </span>
                    ) : (
                      <span className="rounded bg-muted px-1.5 py-0.5 text-[10.5px] text-muted-foreground">未授权</span>
                    )}
                  </div>
                );
              })}
            </div>
            <p className="mt-1 text-[10.5px] text-muted-foreground">
              这里不放"允许/拒绝"按钮：说一句就是授权（「帮我挂个 webshell」），否定句（「不要…」）自动不算。
            </p>
          </section>
        )}

        {/* 一句话：想做哪一步就去主会话说（确定性模板，不自动发） */}
        <section className="flex flex-wrap items-center gap-1.5 rounded border border-sky-500/30 bg-sky-500/[0.06] px-2 py-1.5">
          <ArrowUpRightIcon className="size-3.5 shrink-0 text-sky-600" />
          <span className="min-w-0 flex-1">
            {brief?.say_this || "想做上面哪一步，直接在下面主对话里说；我不会自己动手。"}
          </span>
          {actions.length > 0 && <CardCopy text={actions[0].say_it} label="复制第一句" />}
        </section>
        {verdict && (
          <div className="text-[10.5px] text-muted-foreground">
            <div className="flex flex-wrap items-center gap-1.5">
              <span>为什么这么判：{verdict.advice}</span>
              <span className="opacity-60">（规则判定，不调模型）</span>
              {((verdict.reasons ?? []).length > 0 || (verdict.surface ?? []).length > 0) && (
                <button
                  type="button"
                  className="rounded border px-1.5 py-0.5 hover:bg-accent"
                  onClick={() => setShowWhy((v) => !v)}
                >
                  {showWhy ? "收起依据" : `看依据（${(verdict.reasons ?? []).length} 条理由）`}
                </button>
              )}
            </div>
            {showWhy && (
              <div className="mt-1 flex flex-col gap-1">
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
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
