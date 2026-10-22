"use client";

import * as React from "react";

import { RefreshCwIcon, ScrollTextIcon, SearchIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api, p2api } from "@/lib/api";
import type { P2AuditEntry, Task } from "@/lib/types";
import { cn } from "@/lib/utils";

// 审计页：audit_log（每步动作、参数摘要、结果）。只追加，不可改。
export default function AuditPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [entries, setEntries] = React.useState<P2AuditEntry[]>([]);
  const [q, setQ] = React.useState("");
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);

  React.useEffect(() => {
    api
      .tasks()
      .then((r) => setTasks(r.tasks ?? []))
      .catch((e) => {
        const message = `读取任务列表失败：${String(e)}`;
        setErr(message);
        toast.error(message);
      });
  }, []);

  const load = React.useCallback(async (id: string, notify = false) => {
    setLoading(true);
    try {
      setErr("");
      const r = await p2api.p2Audit(id || undefined, 300);
      setEntries(r.entries ?? []);
      if (notify) toast.success(`已加载 ${r.entries?.length ?? 0} 条审计记录`);
    } catch (e) {
      const message = String(e);
      setErr(message);
      toast.error(message);
    } finally {
      setLoading(false);
      setLoaded(true);
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
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">审计日志</h1>
          <p className="text-muted-foreground text-sm">
            谁在什么时候对什么做了什么（参数只存摘要，不存原文）。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
            <SearchIcon className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 w-40 pl-8"
              placeholder="过滤…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <Select value={taskId || "all"} onValueChange={(v) => setTaskId(v === "all" ? "" : v)}>
            <SelectTrigger size="sm" className="w-48">
              <SelectValue placeholder="全部任务" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部任务</SelectItem>
              {tasks.map((t) => (
                <SelectItem key={t.id} value={t.id}>
                  {t.name || t.description || t.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button size="sm" variant="outline" disabled={loading} onClick={() => load(taskId, true)}>
            <RefreshCwIcon className={cn("size-4", loading && "animate-spin")} />
            刷新
          </Button>
        </div>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">记录（{rows.length}）</CardTitle>
        </CardHeader>
        <CardContent>
          {!loaded ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : rows.length === 0 ? (
            <Empty className="border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <ScrollTextIcon />
                </EmptyMedia>
                <EmptyTitle>没有审计记录</EmptyTitle>
                <EmptyDescription>调整任务或过滤条件后重试；任务执行动作后会在这里留下不可更改的审计记录。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="max-h-[62vh] overflow-auto rounded-md border">
              <Table className="text-xs">
                <TableHeader className="sticky top-0 z-10 bg-card">
                  <TableRow>
                    <TableHead className="h-8">时间</TableHead>
                    <TableHead className="h-8">执行者</TableHead>
                    <TableHead className="h-8">工具</TableHead>
                    <TableHead className="h-8">动作</TableHead>
                    <TableHead className="h-8">结果</TableHead>
                    <TableHead className="h-8">目标 / 细节</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((e) => (
                    <TableRow key={e.id}>
                      <TableCell className="py-1 whitespace-nowrap">{e.ts}</TableCell>
                      <TableCell className="py-1">{e.actor || "—"}</TableCell>
                      <TableCell className="py-1">{e.tool || "—"}</TableCell>
                      <TableCell className="py-1">{e.action || "—"}</TableCell>
                      <TableCell className="py-1">
                        <Badge variant={e.result === "ok" ? "outline" : "destructive"}>{e.result || "—"}</Badge>
                      </TableCell>
                      <TableCell className="max-w-[420px] truncate py-1" title={`${e.target} ${e.err ?? ""}`}>
                        {e.target || "—"}
                        {e.err ? ` · ${e.err}` : ""}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
