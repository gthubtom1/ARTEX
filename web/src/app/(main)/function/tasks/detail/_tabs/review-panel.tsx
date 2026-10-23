"use client";

import * as React from "react";

import { ChevronDownIcon, Loader2Icon, RefreshCwIcon, SparklesIcon, XIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { reviewapi, type ReviewAction, type ReviewRun } from "@/lib/api";
import { cn } from "@/lib/utils";

// 复盘官（CARD-C01）
// 规格：docs/09-详设-C01-复盘官.md
// 形态参照「旁路提问」：胶囊开关 → 右侧常驻面板；区别是它会主动给结论 + 可采纳。

const VERDICT: Record<string, { label: string; chip: string }> = {
  continue: { label: "继续", chip: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  pivot: { label: "转向", chip: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  wrapup: { label: "该收口了", chip: "bg-rose-500/15 text-rose-600 dark:text-rose-400" },
};

export function ReviewCapsule({ taskId }: { taskId: string }) {
  const [open, setOpen] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [latest, setLatest] = React.useState<ReviewRun | null>(null);
  const [notesVersion, setNotesVersion] = React.useState(0);
  const [notes, setNotes] = React.useState("");
  const [showNotes, setShowNotes] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [rebut, setRebut] = React.useState("");
  const [applied, setApplied] = React.useState<number[]>([]);

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
    if (!open) return;
    load();
  }, [open, load]);

  async function runOnce() {
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
  }

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
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen((o) => !o)} title="复盘官：整条方向该不该继续">
        <SparklesIcon className="size-3.5" />
        复盘官
        {open && <span className="ml-1 inline-flex size-1.5 rounded-full bg-emerald-500" />}
      </Button>

      {open && (
        <div className="fixed bottom-4 right-4 top-20 z-40 flex w-[380px] flex-col overflow-hidden rounded-xl border bg-card shadow-2xl">
          {/* 头 */}
          <div className="flex items-start gap-2 border-b px-3 py-2.5">
            <span className="flex size-6 items-center justify-center rounded-lg bg-violet-500 text-[11px] font-bold text-white">
              复
            </span>
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-1.5 text-[12.5px] font-semibold">
                复盘官
                <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-600">常驻</span>
              </div>
              <div className="text-[10.5px] text-muted-foreground">只读快照 + 自己的笔记 · 不跑命令</div>
            </div>
            <button className="text-muted-foreground hover:text-foreground" onClick={() => setOpen(false)} aria-label="关闭">
              <XIcon className="size-4" />
            </button>
          </div>

          {/* 工具行 */}
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
            <pre className="max-h-32 overflow-auto whitespace-pre-wrap border-b bg-muted/40 px-3 py-2 text-[11px] leading-relaxed">
              {notes || "它还没有笔记（每评审一次会更新）"}
            </pre>
          )}

          {/* 结论 */}
          <div className="min-h-0 flex-1 overflow-auto px-3 py-3">
            {latest ? (
              <div className="space-y-2.5">
                <div className="flex items-center gap-2">
                  <span className={cn("rounded px-1.5 py-0.5 text-[10px] font-semibold", tone.chip)}>{tone.label}</span>
                  <span className="text-[10.5px] text-muted-foreground">{latest.created_at}</span>
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
                            applied.includes(i)
                              ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-700"
                              : "hover:bg-accent",
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
      )}
    </>
  );
}
