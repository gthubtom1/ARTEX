"use client";

import * as React from "react";

import { ChevronDownIcon, Loader2Icon, RefreshCwIcon, SparklesIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { reviewapi, type ReviewAction, type ReviewRun, type ReviewState } from "@/lib/api";
import { cn } from "@/lib/utils";

// 审查官（CARD-C01，原「审查官」）
//
// 交互对齐「旁路提问」：**同一个右侧方框**里的面板（不是悬浮层），
// 另配一个真正的开/关（持久化），开关 ≠ 是否打开面板。
const VERDICT: Record<string, { label: string; chip: string }> = {
  continue: { label: "继续", chip: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  pivot: { label: "转向", chip: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  wrapup: { label: "该收口了", chip: "bg-rose-500/15 text-rose-600 dark:text-rose-400" },
};

const LS_KEY = (taskId: string) => `artex.review.enabled.${taskId}`;

export function useReviewEnabled(taskId: string) {
  const [enabled, setEnabled] = React.useState(false);
  React.useEffect(() => {
    try {
      setEnabled(localStorage.getItem(LS_KEY(taskId)) === "1");
    } catch {
      /* ignore */
    }
  }, [taskId]);
  const set = React.useCallback(
    (v: boolean) => {
      setEnabled(v);
      try {
        localStorage.setItem(LS_KEY(taskId), v ? "1" : "0");
      } catch {
        /* ignore */
      }
    },
    [taskId],
  );
  return { enabled, setEnabled: set };
}

// useReviewState 轮询审查官状态（未读红点 / 生效档位 / 介入与自动次数 / 熔断）。
// 轻量接口（GET review/state），不拉结论正文，所以可以 15s 一刷。
export function useReviewState(taskId: string) {
  const [state, setState] = React.useState<ReviewState | null>(null);
  const reload = React.useCallback(() => {
    reviewapi
      .state(taskId)
      .then(setState)
      .catch(() => void 0);
  }, [taskId]);
  React.useEffect(() => {
    reload();
    const t = setInterval(reload, 15_000);
    return () => clearInterval(t);
  }, [reload]);
  return { state, reload };
}

const TIER_LABEL: Record<string, string> = {
  observe: "只看",
  lowrisk: "低风险自动",
  auto: "全自动",
};

export function ReviewCapsule({
  taskId,
  open,
  onToggle,
  enabled,
}: {
  taskId: string;
  open: boolean;
  onToggle: () => void;
  enabled: boolean;
}) {
  const { state } = useReviewState(taskId);
  const unseen = state?.unseen ?? 0;
  // 它自己看过但没人看结论 → 红点一直亮（docs/13：「你不理它，红点不消」）。
  const tripped = state?.tripped;
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={onToggle}
      title={
        tripped
          ? `审查官已熔断：${state?.trip_reason || "自动动作没带来看得见的改善"}`
          : `审查官：整条方向该不该继续（${TIER_LABEL[state?.tier ?? ""] ?? "低风险自动"}）`
      }
      className={cn(unseen > 0 && "border-violet-500/50")}
    >
      <SparklesIcon className="size-3.5" />
      审查官
      {enabled && <span className="ml-1 rounded bg-emerald-500/15 px-1 text-[9px] font-semibold text-emerald-600">开</span>}
      {tripped && <span className="ml-1 rounded bg-rose-500/15 px-1 text-[9px] font-semibold text-rose-600">熔断</span>}
      {unseen > 0 && (
        <span className="ml-1 inline-flex min-w-[14px] items-center justify-center rounded-full bg-violet-500 px-1 text-[9px] font-bold text-white">
          {unseen > 9 ? "9+" : unseen}
        </span>
      )}
      {open && <span className="ml-1 inline-flex size-1.5 rounded-full bg-emerald-500" />}
    </Button>
  );
}

export function ReviewPanel({
  taskId,
  onClose,
  enabled,
  setEnabled,
}: {
  taskId: string;
  onClose: () => void;
  enabled: boolean;
  setEnabled: (v: boolean) => void;
}) {
  const [busy, setBusy] = React.useState(false);
  const [latest, setLatest] = React.useState<ReviewRun | null>(null);
  const [notesVersion, setNotesVersion] = React.useState(0);
  const [notes, setNotes] = React.useState("");
  const [showNotes, setShowNotes] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [rebut, setRebut] = React.useState("");
  const [applied, setApplied] = React.useState<number[]>([]);

  const { state, reload } = useReviewState(taskId);

  const load = React.useCallback(() => {
    reviewapi
      .latest(taskId)
      .then((r) => {
        setLatest(r.run);
        setNotesVersion(r.notes_version ?? 0);
      })
      .catch(() => void 0);
  }, [taskId]);

  React.useEffect(() => {
    load();
  }, [load]);

  // 面板打开 = 人看到了：把未读结论标记为已读（红点消），并刷新状态。
  React.useEffect(() => {
    reviewapi
      .seen(taskId)
      .then(() => reload())
      .catch(() => void 0);
  }, [taskId, latest?.id, reload]);

  const runOnce = React.useCallback(async () => {
    setBusy(true);
    setMsg("");
    setApplied([]);
    try {
      const r = await reviewapi.run(taskId);
      setLatest(r.run ?? null);
      setMsg(r.run?.model === "rule" ? "这次是规则版判断（模型暂时不可用）" : "看完了");
    } catch {
      setMsg("它现在看不了（前面可能还有一个在评审，或任务已终止）");
    } finally {
      setBusy(false);
    }
  }, [taskId]);

  // 开关打开时：本页面停留期间每 10 分钟自动看一次
  React.useEffect(() => {
    if (!enabled) return;
    const t = setInterval(() => void runOnce(), 10 * 60 * 1000);
    return () => clearInterval(t);
  }, [enabled, runOnce]);

  async function apply(action: ReviewAction, idx: number) {
    if (!latest) return;
    try {
      const r = await reviewapi.apply(taskId, latest.id, [action]);
      if (r.applied > 0) {
        setApplied((a) => [...a, idx]);
        setMsg("已经照它说的做了");
      } else {
        setMsg("没能执行（这条方向的状态已经变了）");
      }
    } catch {
      setMsg("执行失败");
    }
  }

  async function sendRebut() {
    if (!latest || !rebut.trim()) return;
    setBusy(true);
    try {
      const r = await reviewapi.rebut(taskId, latest.id, rebut.trim());
      setLatest(r.run ?? null);
      setApplied([]);
      setMsg("它重新判了一遍");
      setRebut("");
    } catch {
      setMsg("反驳没送到（前面可能还有一个在评审）");
    } finally {
      setBusy(false);
    }
  }

  async function toggleNotes() {
    if (!showNotes && !notes) {
      try {
        const n = await reviewapi.notes(taskId);
        setNotes(n.body || "");
      } catch {
        /* ignore */
      }
    }
    setShowNotes((v) => !v);
  }

  const v = latest?.verdict ?? "continue";
  const tone = VERDICT[v] ?? VERDICT.continue;

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* 头：开关 + 关闭（与旁路提问面板同一套） */}
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <span className="flex size-5 items-center justify-center rounded bg-violet-500 text-[10px] font-bold text-white">复</span>
        <div className="min-w-0 flex-1">
          <div className="text-[12.5px] font-semibold">审查官</div>
          <div className="text-[10px] text-muted-foreground">只读快照 + 自己的笔记 · 不跑命令</div>
        </div>
        <button
          className={cn(
            "flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10.5px]",
            enabled ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-600" : "text-muted-foreground",
          )}
          onClick={() => setEnabled(!enabled)}
          title={enabled ? "已开启：每 10 分钟自动看一次" : "已关闭：只在你点「现在看一次」时评审"}
        >
          <span className={cn("size-2 rounded-full", enabled ? "bg-emerald-500" : "bg-slate-300")} />
          {enabled ? "已开启" : "已关闭"}
        </button>
        <button className="text-[11px] text-muted-foreground hover:text-foreground" onClick={onClose}>
          关闭
        </button>
      </div>

      <div className="flex items-center gap-2 border-b px-3 py-1.5">
        <span className="text-[10px] text-muted-foreground">权限</span>
        {(["observe", "lowrisk", "auto"] as const).map((t) => (
          <button
            key={t}
            className={cn(
              "rounded-full border px-2 py-0.5 text-[10.5px]",
              (state?.tier ?? "lowrisk") === t
                ? "border-violet-500/50 bg-violet-500/10 text-violet-600"
                : "text-muted-foreground hover:text-foreground",
            )}
            title={
              t === "observe"
                ? "只看：只留结论卡，不动任何方向"
                : t === "lowrisk"
                  ? "低风险自动：只自动做「重开被自己接口打断的」与「标死目标明确拒绝≥3次的」"
                  : "全自动：同上，配额更高（仍不碰目标/证据）"
            }
            onClick={() => {
              reviewapi
                .setTier(taskId, t)
                .then(() => reload())
                .catch(() => void 0);
            }}
          >
            {TIER_LABEL[t]}
          </button>
        ))}
        <span className="ml-auto text-[10px] text-muted-foreground">
          自动看过 {state?.interventions ?? 0} 次 · 自动处理 {state?.auto_actions ?? 0} 次
        </span>
      </div>
      {state?.tripped && (
        <div className="border-b bg-rose-500/10 px-3 py-1.5 text-[10.5px] text-rose-700">
          已熔断转「只看」：{state.trip_reason || "自动动作没带来看得见的改善"}（点上面的档位可重新授权）
        </div>
      )}
      <div className="flex items-center gap-2 border-b px-3 py-2">
        <Button size="sm" variant="outline" onClick={runOnce} disabled={busy} className="flex-1">
          {busy ? <Loader2Icon className="size-3.5 animate-spin" /> : <RefreshCwIcon className="size-3.5" />}
          现在看一次
        </Button>
        <Button size="sm" variant="ghost" onClick={toggleNotes} className="shrink-0 text-[11px]">
          笔记 {notesVersion > 0 ? `v${notesVersion}` : ""}
          <ChevronDownIcon className={cn("size-3 transition-transform", showNotes && "rotate-180")} />
        </Button>
      </div>

      {showNotes && (
        <pre className="max-h-28 overflow-auto whitespace-pre-wrap border-b bg-muted/40 px-3 py-2 text-[11px] leading-relaxed">
          {notes || "它还没有笔记（每评审一次会更新）"}
        </pre>
      )}

      <div className="min-h-0 flex-1 overflow-auto px-3 py-3">
        {latest ? (
          <div className="space-y-2.5">
            <div className="flex items-center gap-2">
              <span className={cn("rounded px-1.5 py-0.5 text-[10px] font-semibold", tone.chip)}>{tone.label}</span>
              <span className="text-[10.5px] text-muted-foreground">{latest.created_at}</span>
              {latest.level === "action" && (
                <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-600">
                  建议动任务
                </span>
              )}
              {latest.auto_applied && (
                <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-600">
                  它自己做了
                </span>
              )}
              {latest.ack_status === "rejected" && (
                <span className="rounded bg-rose-500/15 px-1.5 py-0.5 text-[10px] text-rose-600">已反驳</span>
              )}
              <span className="ml-auto rounded bg-slate-500/10 px-1.5 py-0.5 text-[10px] text-slate-600">
                {latest.model === "rule" ? "规则版" : "模型版"}
              </span>
            </div>
            <div className="whitespace-pre-wrap text-[12px] leading-relaxed">{latest.body}</div>
            {latest.cites.length > 0 && (
              <div className="text-[10.5px] text-muted-foreground">
                依据 {latest.cites.length} 条：{latest.cites.slice(0, 5).map((c) => `${c.kind}:${c.id}`).join(" · ")}
              </div>
            )}
            {latest.actions.length > 0 && (
              <div className="space-y-1">
                <div className="text-[10.5px] font-semibold text-muted-foreground">它可以替你做的动作</div>
                {latest.actions.map((a, i) => (
                  <div key={i} className="flex items-start gap-2 rounded-md border px-2 py-1.5 text-[11.5px]">
                    <span className="flex-1">
                      <span className="font-medium">
                        {a.kind === "kill_intent" ? `放弃方向 #${a.intent}` : null}
                        {a.kind === "retry_intent" ? `重跑被打断的方向 #${a.intent}` : null}
                        {a.kind === "set_next_stage" ? "换阶段（需你手动确认）" : null}
                      </span>
                      <span className="ml-1 text-muted-foreground">{a.why}</span>
                    </span>
                    <button
                      className={cn(
                        "shrink-0 rounded border px-1.5 py-0.5 text-[10.5px]",
                        applied.includes(i) ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700" : "hover:bg-accent",
                      )}
                      onClick={() => apply(a, i)}
                      disabled={applied.includes(i)}
                    >
                      {applied.includes(i) ? "已采纳" : "采纳"}
                    </button>
                  </div>
                ))}
              </div>
            )}
            <div className="border-t pt-2">
              <div className="mb-1 text-[10.5px] text-muted-foreground">不同意？写一句理由让它重判</div>
              <textarea
                className="h-16 w-full resize-none rounded-md border bg-background p-2 text-[11.5px]"
                placeholder="例如：入口 2 还有希望，它 8 分钟前还有新发现"
                value={rebut}
                onChange={(e) => setRebut(e.target.value)}
              />
              <Button size="sm" variant="outline" className="mt-1 w-full" onClick={sendRebut} disabled={busy || !rebut.trim()}>
                提交反驳
              </Button>
            </div>
          </div>
        ) : (
          <div className="rounded-lg border border-dashed px-4 py-10 text-center text-[11.5px] leading-relaxed text-muted-foreground">
            它还没看过这个任务
            <br />
            点「现在看一次」，它会读当前战果告诉你该不该继续
          </div>
        )}
      </div>

      {msg && <div className="border-t bg-muted/40 px-3 py-1.5 text-[11px] text-muted-foreground">{msg}</div>}
    </div>
  );
}
