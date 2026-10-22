"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";
import type { Task } from "@/lib/types";

// 报告页：选任务 → 取 Markdown 报告（已含能力菜单/覆盖度/持有台账）→ 预览/下载。
// 不引入 markdown 渲染库：报告是交付物，原文展示最可靠。
export default function ReportPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [md, setMd] = React.useState("");
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
    if (!id) return;
    setLoading(true);
    setErr("");
    try {
      const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
      const r = await fetch(`/api/report?task=${encodeURIComponent(id)}`, {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (!r.ok) {
        setErr(`读取报告失败：HTTP ${r.status}`);
        setMd("");
        return;
      }
      setMd(await r.text());
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load(taskId);
  }, [taskId, load]);

  function download() {
    const blob = new Blob([md], { type: "text/markdown;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `artex-report-${taskId || "task"}.md`;
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">报告</h1>
          <p className="text-sm text-muted-foreground">
            报告 = 发现 + 能力菜单 + 13 组覆盖度 + 持有台账 + 负结论（可直接交）。
          </p>
        </div>
        <div className="flex items-center gap-2">
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
          <Button size="sm" variant="outline" disabled={loading || !taskId} onClick={() => load(taskId)}>
            {loading ? "生成中…" : "重新生成"}
          </Button>
          <Button size="sm" disabled={!md} onClick={download}>
            下载 .md
          </Button>
        </div>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">预览（Markdown 原文）</CardTitle>
        </CardHeader>
        <CardContent>
          {md ? (
            <pre className="max-h-[70vh] overflow-auto whitespace-pre-wrap text-xs leading-relaxed">{md}</pre>
          ) : (
            <div className="text-sm text-muted-foreground">选一个任务后生成报告。</div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
