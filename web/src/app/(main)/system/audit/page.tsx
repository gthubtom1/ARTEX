"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { api, p2api } from "@/lib/api";
import type { P2AuditEntry, Task } from "@/lib/types";

// 审计页：audit_log（每步动作、参数摘要、结果）。只追加，不可改。
export default function AuditPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [entries, setEntries] = React.useState<P2AuditEntry[]>([]);
  const [q, setQ] = React.useState("");
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  React.useEffect(() => {
    api
      .tasks()
      .then((r) => setTasks(r.tasks ?? []))
      .catch(() => setErr("读取任务列表失败"));
  }, []);

  const load = React.useCallback(async (id: string) => {
    setLoading(true);
    try {
      setErr("");
      const r = await p2api.p2Audit(id || undefined, 300);
      setEntries(r.entries ?? []);
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load(taskId);
  }, [taskId, load]);

  const rows = entries.filter((e) => {
    const n = q.trim().toLowerCase();
    if (!n) return true;
    return `${e.actor} ${e.tool} ${e.target} ${e.action} ${e.result}`.toLowerCase().includes(n);
  });

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">审计日志</h1>
          <p className="text-sm text-muted-foreground">谁在什么时候对什么做了什么（参数只存摘要，不存原文）。</p>
        </div>
        <div className="flex items-center gap-2">
          <Input className="h-8 w-40" placeholder="过滤…" value={q} onChange={(e) => setQ(e.target.value)} />
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
          <CardTitle className="text-sm">记录（{rows.length}）</CardTitle>
        </CardHeader>
        <CardContent>
          {rows.length === 0 ? (
            <div className="text-sm text-muted-foreground">没有审计记录。</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">时间</th>
                    <th className="py-1 pr-2">执行者</th>
                    <th className="py-1 pr-2">工具</th>
                    <th className="py-1 pr-2">动作</th>
                    <th className="py-1 pr-2">结果</th>
                    <th className="py-1">目标 / 细节</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((e) => (
                    <tr key={e.id} className="border-b last:border-0">
                      <td className="py-1 pr-2 whitespace-nowrap">{e.ts}</td>
                      <td className="py-1 pr-2">{e.actor || "—"}</td>
                      <td className="py-1 pr-2">{e.tool || "—"}</td>
                      <td className="py-1 pr-2">{e.action || "—"}</td>
                      <td className="py-1 pr-2">
                        <Badge variant={e.result === "ok" ? "outline" : "destructive"}>{e.result || "—"}</Badge>
                      </td>
                      <td className="max-w-[420px] truncate py-1" title={`${e.target} ${e.err ?? ""}`}>
                        {e.target || "—"}
                        {e.err ? ` · ${e.err}` : ""}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
