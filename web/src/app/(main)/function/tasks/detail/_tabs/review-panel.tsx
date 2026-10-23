"use client";

import * as React from "react";

import { Loader2Icon, SparklesIcon, XIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { reviewapi, type ReviewAction, type ReviewRun } from "@/lib/api";
import { cn } from "@/lib/utils";

// 复盘官（CARD-C01）
// 规格：docs/09-详设-C01-复盘官.md
// 它只读结构化快照 + 自己的笔记；给的是"整条方向还成不成立"，不是某条洞真假。

const VERDICT_LABEL: Record<string, string> = {
  continue: "继续",
  pivot: "转向",
  wrapup: "该收口了",
};

function verdictTone(v: string) {
  if (v === "wrapup") return "bg-rose-500/15 text-rose-600";
  if (v === "pivot") return "bg-amber-500/15 text-amber-700";
  return "bg-emerald-500/15 text-emerald-600";
}

export function ReviewCapsule({ taskId }: { taskId: string }) {
  const [open, setOpen] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [latest, setLatest] = React.useState<ReviewRun | null>(null);
  const [notesVersion, setNotesVersion] = React.useState(0);
  const [msg, setMsg] = React.useState("");
  const [rebut, setRebutt] = React.useState("");

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
    try {
      const r = await reviewapi.run(taskId);
      setLatest(r.run ?? null);
      setMsg(r.run?.model === "rule" ? "这次是规则版判断（模型暂时不可用）" : "看完了");
    } catch {
      setMsg("它现在看不了（可能前面还有一个在评审，或任务已终止）");
    } finally {
      setBusy(false);
    }
  }

  async function apply(action: ReviewAction) {
    if (!latest) return;
    try {
      const r = await reviewapi.apply(taskId, latest.id, [action]);
      setMsg(r.applied > 0 ? "已经照它说的做了" : "没能执行（这条方向状态已经变了）");
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
      setMsg("它重新判了一遍");
      setRebutt("");
    } catch {
      setMsg("反驳没送到（可能前面还有一个在评审）");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen((o) => !o)} title="复盘官：整条方向该不该继续">
        <SparklesIcon className="size-3.5" />
        复盘官
        {open && <span className="ml-1 inline-flex size-1.5 rounded-full bg-emerald-500" />}
      </Button>

      {open && (
        <div className="fixed bottom-4 right-4 top-20 z-40 flex w-[380px] flex-col overflow-hidden rounded-xl border bg-card shadow-2xl">
          <div className="flex items-center gap-2 border-b px-3 py-2">
            <span className="flex size-5 items-center justify-center rounded bg-violet-500 text-[10px] font-bold text-white">复</span>
            <div className="min-w-0">
              <div className="text-[12.5px] font-semibold">复盘官</div>
              <div className="text-[10.5px] text-muted-foreground">只读快照 + 自己的笔记 · 不跑命令</div>
            </div>
            <button className="ml-auto text-muted-foreground hover:text-foreground" onClick={() => setOpen(false)}>
              <XIcon className="size-4" />
            </button>
          </div>

          <div className="flex min-h-0 flex-1 flex-col gap-3 overflow-auto p-3">
            <Button size="sm" variant="outline" onClick={runOnce} disabled={busy} className="w-full">
              {busy ? <Loader2Icon className="size-3.5 animate-spin" /> : null}
              现在看一次
            </Button>

            {latest ? (
              <div className="rounded-lg border p-3">
                <div className="flex items-center gap-2">
                  <span className={cn("rounded px-1.5 py-0.5 text-[10px] font-semibold", verdictTone(latest.verdict))}>
                    {VERDICT_LABEL[latest.verdict] ?? latest.verdict}
                  </span>
                  <span className="text-[10.5px] text-muted-foreground">{latest.created_at}</span>
                  {latest.model === "rule" && (
                    <span className="rounded bg-slate-500/10 px-1.5 py-0.5 text-[10px] text-slate-600">规则版</span>
                  )}
                </div>
                <div className="mt-2 whitespace-pre-wrap text-[12px] leading-relaxed">{latest.body}</div>
                {latest.cites.length > 0 && (
                  <div className="mt-1 text-[10.5px] text-muted-foreground">
                    依据 {latest.cites.length} 条：
                    {latest.cites.slice(0, 4).map((c) => `${c.kind}:${c.id}`).join(" · ")}
                  </div>
                )}

                {latest.actions.length > 0 && (
                  <div className="mt-2 space-y-1">
                    {latest.actions.map((a, i) => (
                      <div key={i} className="flex items-start gap-2 rounded border px-2 py-1.5 text-[11.5px]">
                        <span className="flex-1">
                          {a.kind === "kill_intent" && `放弃一条方向（#${a.intent}）`}
                          {a.kind === "retry_intent" && `重跑一条被打断的方向（#${a.intent}）`}
                          {a.kind === "set_next_stage" && `换阶段（需你手动确认）`}
                          <span className="ml-1 text-muted-foreground">{a.why}</span>
                        </span>
                        <button
                          className="rounded border px-1.5 py-0.5 text-[10.5px] hover:bg-accent"
                          onClick={() => apply(a)}
                        >
                          采纳
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                <div className="mt-3 border-t pt-2">
                  <div className="mb-1 text-[10.5px] text-muted-foreground">不同意？写一句理由让它重判</div>
                  <textarea
                    className="h-16 w-full resize-none rounded-md border bg-background p-2 text-[11.5px]"
                    placeholder="例如：入口 2 还有希望，它 8 分钟前还有新发现"
                    value={rebut}
                    onChange={(e) => setRebutt(e.target.value)}
                  />
                  <Button size="sm" variant="outline" className="mt-1 w-full" onClick={sendRebut} disabled={busy || !rebut.trim()}>
                    提交反驳
                  </Button>
                </div>
              </div>
            ) : (
              <div className="rounded-lg border border-dashed p-6 text-center text-[11.5px] text-muted-foreground">
                它还没看过。点上面的按钮让它看一眼。
              </div>
            )}

            {notesVersion > 0 && (
              <div className="text-[10.5px] text-muted-foreground">它的笔记已更新 {notesVersion} 版（越用越懂这个任务）</div>
            )}
            {msg && <div className="rounded bg-muted px-2 py-1.5 text-[11px] text-muted-foreground">{msg}</div>}
          </div>
        </div>
      )}
    </>
  );
}
