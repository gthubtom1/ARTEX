"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import type { Task } from "@/lib/types";

// verifier 复核页：独立角色对 finding 的对抗式复核结果（verified/inferred/rejected）。
// 数据来自 GET /api/p2/verifier?task_id=。
type Review = {
  finding_id: number;
  ts: string;
  content_ref: string;
  review: { verdict?: string; reason?: string; missing?: string[]; source?: string };
};

function authHeaders(): Record<string, string> {
  const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const tone: Record<string, string> = {
  verified: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  inferred: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  rejected: "bg-red-500/10 text-red-600 dark:text-red-400",
};

export default function VerifierPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [rows, setRows] = React.useState<Review[]>([]);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  React.useEffect(() => {
    api
      .tasks()
      .then((r) => {
        setTasks(r.tasks ?? []);
        setTaskId(r.active || r.tasks?.[0]?.id || "");
      })
      .catch(() => setErr("读取任务列表失败"));
  }, []);

  const load = React.useCallback(async (id: string) => {
    setLoading(true);
    try {
      setErr("");
      const qs = id ? `?task_id=${encodeURIComponent(id)}` : "";
      const r = await fetch(`/api/p2/verifier${qs}`, { headers: authHeaders() });
      if (!r.ok) {
        setErr(`读取失败：HTTP ${r.status}`);
        setRows([]);
        return;
      }
      const d = await r.json();
      setRows(d.reviews ?? []);
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load(taskId);
  }, [taskId, load]);

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">复核（verifier 专判）</h1>
          <p className="text-sm text-muted-foreground">
            独立模型只看证据判断 finding 是否成立；证据不足会降级并列出缺什么，同时自动把能力卡降档。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <select
            className="h-8 rounded-md border bg-background px-2 text-sm"
            value={taskId}
            onChange={(e) => setTaskId(e.target.value)}
          >
            <option value="">全部任务</option>
            {tasks.map((t) => (
              <option key={t.id} value={t.id}>
                {t.name || t.description || t.id}
              </option>
            ))}
          </select>
          <Button size="sm" variant="outline" disabled={loading} onClick={() => load(taskId)}>
            {loading ? "读取中…" : "刷新"}
          </Button>
        </div>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">复核记录（{rows.length}）</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          {rows.length === 0 ? (
            <div className="text-sm text-muted-foreground">
              还没有复核记录：任务产生 finding 后，verifier 会按冷却周期自动复核。
            </div>
          ) : (
            rows.map((r) => {
              const v = r.review?.verdict ?? "unknown";
              return (
                <div key={`${r.finding_id}-${r.ts}`} className="rounded-md border p-2">
                  <div className="flex flex-wrap items-center gap-2 text-xs">
                    <Badge className={tone[v] ?? ""}>{v}</Badge>
                    <span className="font-medium">finding#{r.finding_id}</span>
                    <span className="text-muted-foreground">{r.ts}</span>
                    {r.review?.source && <span className="text-muted-foreground">source={r.review.source}</span>}
                  </div>
                  {r.review?.reason && <div className="mt-1 text-sm">{r.review.reason}</div>}
                  {(r.review?.missing?.length ?? 0) > 0 && (
                    <div className="mt-1 text-xs text-muted-foreground">
                      还缺：{(r.review.missing ?? []).join("；")}
                    </div>
                  )}
                </div>
              );
            })
          )}
        </CardContent>
      </Card>
    </div>
  );
}
