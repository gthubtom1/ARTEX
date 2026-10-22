"use client";

import * as React from "react";

import { RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { api, p2api } from "@/lib/api";
import type { Task } from "@/lib/types";

// verifier 复核页：独立角色对 finding 的对抗式复核结果（verified/inferred/rejected）。
// 数据来自 GET /api/p2/verifier?task_id=。
type Review = {
  finding_id: number;
  ts: string;
  content_ref: string;
  review: { verdict?: string; reason?: string; missing?: string[]; source?: string };
};

const ALL_TASKS = "all";

function authHeaders(): Record<string, string> {
  const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

const tone: Record<string, string> = {
  verified: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  inferred: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  rejected: "bg-red-500/10 text-red-600 dark:text-red-400",
};

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export default function VerifierPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [rows, setRows] = React.useState<Review[]>([]);
  const [loading, setLoading] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);
  const requestRef = React.useRef(0);

  React.useEffect(() => {
    api
      .tasks()
      .then((r) => {
        setTasks(r.tasks ?? []);
        setTaskId(r.active || r.tasks?.[0]?.id || "");
      })
      .catch(() => toast.error("读取任务列表失败"));
  }, []);

  const load = React.useCallback(async (id: string) => {
    const request = ++requestRef.current;
    setLoading(true);
    try {
      const d = await p2api.verifierReviews(id || undefined, 100);
      if (request !== requestRef.current) return;
      setRows(d.reviews ?? []);
    } catch (e) {
      if (request !== requestRef.current) return;
      setRows([]);
      toast.error(`读取复核记录失败：${errorMessage(e)}`);
    } finally {
      if (request === requestRef.current) {
        setLoaded(true);
        setLoading(false);
      }
    }
  }, []);

  const taskKeyRef = React.useRef(taskId);
  React.useEffect(() => {
    if (taskKeyRef.current === taskId) return;
    taskKeyRef.current = taskId;
    setLoaded(false);
  }, [taskId]);

  React.useEffect(() => {
    void load(taskId);
  }, [taskId, load]);

  let content: React.ReactNode;
  if (loading && !loaded) {
    content = (
      <div className="space-y-2" role="status" aria-label="加载复核记录">
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-16 w-full" />
      </div>
    );
  } else if (rows.length === 0) {
    content = (
      <Empty className="border">
        <EmptyHeader>
          <EmptyTitle>暂无复核记录</EmptyTitle>
          <EmptyDescription>任务产生 finding 后，verifier 会按冷却周期自动复核，结果会出现在这里。</EmptyDescription>
        </EmptyHeader>
      </Empty>
    );
  } else {
    content = rows.map((r) => {
      const v = r.review?.verdict ?? "unknown";
      return (
        <div key={`${r.finding_id}-${r.ts}`} className="rounded-lg border p-3">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <Badge className={tone[v] ?? ""}>{v}</Badge>
            <span className="text-sm font-medium tabular-nums">finding#{r.finding_id}</span>
            <span className="text-muted-foreground">{r.ts}</span>
            {r.review?.source && <span className="text-muted-foreground">source={r.review.source}</span>}
          </div>
          {r.review?.reason && <div className="mt-1 text-sm">{r.review.reason}</div>}
          {(r.review?.missing?.length ?? 0) > 0 && (
            <div className="text-muted-foreground mt-1 text-xs">还缺：{(r.review.missing ?? []).join("；")}</div>
          )}
        </div>
      );
    });
  }

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">复核（verifier 专判）</h1>
          <p className="text-muted-foreground text-sm">
            独立模型只看证据判断 finding 是否成立；证据不足会降级并列出缺什么，同时自动把能力卡降档。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={taskId || ALL_TASKS} onValueChange={(value) => setTaskId(value === ALL_TASKS ? "" : value)}>
            <SelectTrigger size="sm" className="w-full sm:w-56" aria-label="选择任务">
              <SelectValue placeholder="全部任务" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL_TASKS}>全部任务</SelectItem>
              {tasks.map((t) => (
                <SelectItem key={t.id} value={t.id}>
                  <span className="max-w-[18rem] truncate" title={t.name || t.description || t.id}>
                    {t.name || t.description || t.id}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button size="sm" variant="outline" disabled={loading} onClick={() => void load(taskId)}>
            {loading ? <Spinner /> : <RefreshCwIcon />}
            {loading ? "读取中…" : "刷新"}
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">复核记录（{rows.length}）</CardTitle>
          <CardDescription>verdict 由独立模型根据证据判定：verified / inferred / rejected。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">{content}</CardContent>
      </Card>
    </div>
  );
}
