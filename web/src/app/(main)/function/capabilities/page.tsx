"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api, p2api } from "@/lib/api";
import type { CapabilitiesResponse, CapabilityCard, ProgressResponse, Task } from "@/lib/types";
import { cn } from "@/lib/utils";

// 能力菜单页：零基础用户只看这一页就能决定"要不要继续"。
// 三档按钮 = 商业决策，不是技术决策（默认永远是「只拿证据」）。

const proofTone: Record<string, string> = {
  P1: "bg-muted text-foreground/70",
  P2: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  P3: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  P4: "bg-red-500/10 text-red-600 dark:text-red-400",
  D1: "bg-muted text-foreground/70",
  D2: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  D3: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  D4: "bg-red-500/10 text-red-600 dark:text-red-400",
};

export default function CapabilitiesPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [data, setData] = React.useState<CapabilitiesResponse | null>(null);
  const [prog, setProg] = React.useState<ProgressResponse | null>(null);
  const [busy, setBusy] = React.useState(0);
  const [selected, setSelected] = React.useState<number[]>([]);
  const [batchBusy, setBatchBusy] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    api
      .tasks()
      .then((r) => {
        setTasks(r.tasks ?? []);
        const active = r.active || r.tasks?.[0]?.id || "";
        setTaskId(active);
      })
      .catch(() => setErr("读取任务列表失败（后端未启动？）"));
  }, []);

  const load = React.useCallback(async (id: string) => {
    if (!id) return;
    setErr("");
    try {
      const cards = await p2api.capabilities(id);
      setData(cards);
      try {
        setProg(await p2api.progress(id));
      } catch {
        setProg(null);
      }
    } catch (e) {
      setErr(String(e));
    }
  }, []);

  React.useEffect(() => {
    void load(taskId);
  }, [taskId, load]);

  React.useEffect(() => {
    // 刷新后把已不存在的卡从选择里剔除（批量裁决成功会清空选择）。
    const ids = new Set((data?.cards ?? []).map((c) => c.id));
    setSelected((prev) => prev.filter((id) => ids.has(id)));
  }, [data]);

  async function decide(cap: CapabilityCard, action: "evidence" | "continue" | "switch") {
    setBusy(cap.id);
    setMsg("");
    try {
      const r = await p2api.decideCapability(cap.id, action, taskId);
      setMsg(`已裁决：${r.verdict.status} —— ${r.verdict.hint}`);
      await load(taskId);
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(0);
    }
  }

  function toggle(capId: number) {
    setSelected((prev) => (prev.includes(capId) ? prev.filter((id) => id !== capId) : [...prev, capId]));
  }

  async function decideBatch(action: "evidence" | "continue" | "switch") {
    if (selected.length === 0 || batchBusy) return;
    setBatchBusy(true);
    setMsg("");
    setErr("");
    try {
      const r = await p2api.decideCapabilitiesBatch(selected, action, taskId);
      const okCount = r.results?.length ?? 0;
      const errs = r.errors ?? [];
      const lines = [
        `批量裁决（${action}）：成功 ${okCount} 张${errs.length ? `，失败 ${errs.length} 张` : ""}`,
        ...(errs.length ? [`失败：${errs.join("；")}`] : []),
      ];
      setMsg(lines.join("\n"));
      if (errs.length) setErr(errs.join("；"));
      setSelected([]);
      await load(taskId);
    } catch (e) {
      setErr(String(e));
    } finally {
      setBatchBusy(false);
    }
  }

  const cards = data?.cards ?? [];
  const pending = cards.filter((c) => !c.status || c.status === "open");

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">能力菜单</h1>
          <p className="text-sm text-muted-foreground">
            这个站能做什么、有什么证据、要不要继续。默认「只拿证据」最省风险。
          </p>
        </div>
        <select
          className="h-8 rounded-md border bg-background px-2 text-sm"
          value={taskId}
          onChange={(e) => setTaskId(e.target.value)}
        >
          <option value="">选择任务…</option>
          {tasks.map((t) => (
            <option key={t.id} value={t.id}>
              {t.name || t.description || t.id}
            </option>
          ))}
        </select>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}
      {msg && <div className="whitespace-pre-wrap rounded-md border bg-muted/40 p-2 text-sm">{msg}</div>}

      {prog && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">进度自检（顾问视角）</CardTitle>
          </CardHeader>
          <CardContent className="space-y-1 text-sm">
            <div className="text-muted-foreground">{prog.diagnosis?.diagnosis || "暂无诊断"}</div>
            <div className="flex flex-wrap gap-2 text-xs">
              {(prog.stall?.stuck ?? []).map((s) => (
                <Badge key={s} variant="outline">
                  {s}
                </Badge>
              ))}
              <Badge variant="outline">连续无产出 {prog.stall?.no_progress_rounds ?? 0} 轮</Badge>
              <Badge variant="outline">覆盖 {(prog.stall?.coverage_pct ?? 0).toFixed(0)}%</Badge>
              <Badge variant="outline">预算 {(prog.stall?.budget_burn_pct ?? 0).toFixed(0)}%</Badge>
            </div>
            {(prog.diagnosis?.hints ?? []).map((h, i) => (
              <div key={i} className="rounded bg-muted/50 p-2 text-xs">
                {h}
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">白话清单（{cards.length} 项，待裁决 {pending.length} 项）</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 p-2 text-sm">
            <span className="font-medium">已选 {selected.length} 张</span>
            <Button
              size="sm"
              disabled={batchBusy || selected.length === 0}
              onClick={() => decideBatch("evidence")}
            >
              只拿证据（推荐）
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={batchBusy || selected.length === 0}
              onClick={() => decideBatch("continue")}
            >
              继续深挖（15 分钟）
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={batchBusy || selected.length === 0}
              onClick={() => decideBatch("switch")}
            >
              换目标
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={batchBusy || selected.length === 0}
              onClick={() => setSelected([])}
            >
              清空选择
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={batchBusy || pending.length === 0}
              onClick={() => setSelected(pending.map((c) => c.id))}
            >
              全选待裁决（{pending.length}）
            </Button>
            {batchBusy && <span className="text-xs text-muted-foreground">批量裁决中…</span>}
          </div>
          {cards.length === 0 && <div className="text-sm text-muted-foreground">暂无能力卡：跑一个任务后会自动派生。</div>}
          {cards.map((c) => (
            <div key={c.id} className="rounded-md border p-3">
              <div className="flex flex-wrap items-center gap-2">
                <input
                  type="checkbox"
                  className="h-4 w-4 accent-blue-600"
                  aria-label={`选择能力卡 ${c.id}`}
                  checked={selected.includes(c.id)}
                  disabled={batchBusy}
                  onChange={() => toggle(c.id)}
                />
                <Badge className={cn("text-xs", proofTone[c.proof_level] ?? "")}>{c.proof_level}</Badge>
                <span className="text-sm font-medium">
                  【组 {c.group}】{c.can}
                </span>
                <span className="text-xs text-muted-foreground">{c.confidence === "observed" ? "亲眼看" : "推断"}</span>
                <span className="ml-auto text-xs text-muted-foreground">{c.status || "open"}</span>
              </div>
              {c.plain && <div className="mt-1 text-sm text-muted-foreground">{c.plain}</div>}
              {c.surface && <div className="mt-1 text-xs">入口：{c.surface}</div>}
              {c.evidence && <div className="mt-1 text-xs text-muted-foreground">证据：{c.evidence}</div>}
              {c.impact && <div className="mt-1 text-xs">影响：{c.impact}</div>}
              {c.risk && <div className="mt-1 text-xs text-amber-600 dark:text-amber-400">代价：{c.risk}</div>}
              <div className="mt-2 flex flex-wrap gap-2">
                <Button size="sm" disabled={busy === c.id} onClick={() => decide(c, "evidence")}>
                  只拿证据（推荐）
                </Button>
                <Button size="sm" variant="outline" disabled={busy === c.id} onClick={() => decide(c, "continue")}>
                  继续深挖（15 分钟）
                </Button>
                <Button size="sm" variant="ghost" disabled={busy === c.id} onClick={() => decide(c, "switch")}>
                  换目标
                </Button>
              </div>
            </div>
          ))}
        </CardContent>
      </Card>

      {(data?.coverage?.length ?? 0) > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">覆盖度自检（哪些组测过）</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-1 text-xs sm:grid-cols-3 md:grid-cols-4">
              {data!.coverage.map((r) => (
                <div
                  key={r.group}
                  className={cn(
                    "flex items-center justify-between rounded border px-2 py-1",
                    r.tested ? "border-emerald-500/40 bg-emerald-500/5" : "text-muted-foreground",
                  )}
                >
                  <span>
                    {r.group}. {r.name}
                  </span>
                  <span>{r.tested ? `${r.count} 项` : "未测"}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {data?.plain && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">给非技术读者的原话</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="whitespace-pre-wrap text-xs leading-relaxed">{data.plain}</pre>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
