"use client";

import * as React from "react";

import { ChevronRightIcon } from "lucide-react";
import { toast } from "sonner";

import { TablePagination } from "@/components/table-pagination";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Spinner } from "@/components/ui/spinner";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import { api, p2api } from "@/lib/api";
import type { CapabilitiesResponse, CapabilityCard, ProgressResponse, Task } from "@/lib/types";
import { cn } from "@/lib/utils";

// 能力菜单页：零基础用户只看这一页就能决定"要不要继续"。
// 三档按钮 = 商业决策，不是技术决策（默认永远是「只拿证据」）。

const ALL_TASKS = "all";
const PAGE_SIZE = 10;

type CapabilityAction = "evidence" | "continue" | "switch";

const ACTION_LABEL: Record<CapabilityAction, string> = {
  evidence: "只拿证据（推荐）",
  continue: "继续深挖（15 分钟）",
  switch: "换目标",
};

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

const capabilityStatusLabel: Record<string, string> = {
  open: "待裁决",
  approved: "已批准",
  declined: "不继续",
  done: "已完成",
};

const capabilityStatusTone: Record<string, string> = {
  open: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  approved: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  declined: "bg-muted text-muted-foreground",
  done: "bg-blue-500/15 text-blue-600 dark:text-blue-400",
};

function isPending(card: CapabilityCard) {
  return !card.status || card.status === "open";
}

function capStatusLabel(status: string) {
  return capabilityStatusLabel[status] || status || "待裁决";
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export default function CapabilitiesPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [data, setData] = React.useState<CapabilitiesResponse | null>(null);
  const [prog, setProg] = React.useState<ProgressResponse | null>(null);
  const [loading, setLoading] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);
  const [busy, setBusy] = React.useState(0);
  const [selected, setSelected] = React.useState<Set<number>>(() => new Set());
  const [batchBusy, setBatchBusy] = React.useState(false);
  const [confirmAction, setConfirmAction] = React.useState<CapabilityAction | null>(null);
  const [pendingOnly, setPendingOnly] = React.useState(false);
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(PAGE_SIZE);
  const [collapsedGroups, setCollapsedGroups] = React.useState<Set<number>>(() => new Set());
  const requestRef = React.useRef(0);

  React.useEffect(() => {
    api
      .tasks()
      .then((r) => {
        setTasks(r.tasks ?? []);
        const active = r.active || r.tasks?.[0]?.id || "";
        setTaskId(active);
      })
      .catch(() => toast.error("读取任务列表失败（后端未启动？）"));
  }, []);

  const load = React.useCallback(async (id: string) => {
    const request = ++requestRef.current;
    if (!id) {
      setData(null);
      setProg(null);
      setLoaded(false);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const cards = await p2api.capabilities(id);
      if (request !== requestRef.current) return;
      setData(cards);
      setLoaded(true);
      try {
        const progress = await p2api.progress(id);
        if (request !== requestRef.current) return;
        setProg(progress);
      } catch {
        if (request !== requestRef.current) return;
        setProg(null);
      }
    } catch (e) {
      if (request !== requestRef.current) return;
      toast.error(`加载能力卡失败：${errorMessage(e)}`);
    } finally {
      if (request === requestRef.current) setLoading(false);
    }
  }, []);

  // 换任务时清空旧卡片，先展示骨架屏，避免把上一个任务的结果短暂当成新结果。
  const taskKeyRef = React.useRef(taskId);
  React.useEffect(() => {
    if (taskKeyRef.current === taskId) return;
    taskKeyRef.current = taskId;
    setLoaded(false);
    setData(null);
    setProg(null);
    setPage(1);
    setCollapsedGroups(new Set());
  }, [taskId]);

  React.useEffect(() => {
    void load(taskId);
  }, [taskId, load]);

  // 刷新后把已不存在的卡从选择里剔除（批量裁决成功会清空选择）。
  React.useEffect(() => {
    const ids = new Set((data?.cards ?? []).map((c) => c.id));
    setSelected((prev) => {
      const next = new Set([...prev].filter((id) => ids.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [data]);

  // 换筛选回到第一页。
  const pendingOnlyRef = React.useRef(pendingOnly);
  React.useEffect(() => {
    if (pendingOnlyRef.current === pendingOnly) return;
    pendingOnlyRef.current = pendingOnly;
    setPage(1);
  }, [pendingOnly]);

  async function decide(cap: CapabilityCard, action: CapabilityAction) {
    setBusy(cap.id);
    try {
      const r = await p2api.decideCapability(cap.id, action, taskId);
      toast.success(`已裁决：${r.verdict.status} —— ${r.verdict.hint}`);
      await load(taskId);
    } catch (e) {
      toast.error(`裁决失败：${errorMessage(e)}`);
    } finally {
      setBusy(0);
    }
  }

  async function decideBatch(action: CapabilityAction) {
    if (selected.size === 0 || batchBusy) return;
    const ids = [...selected];
    setBatchBusy(true);
    try {
      const r = await p2api.decideCapabilitiesBatch(ids, action, taskId);
      const okCount = r.results?.length ?? 0;
      const errs = r.errors ?? [];
      if (errs.length) {
        toast.error(`批量裁决：成功 ${okCount} 张，失败 ${errs.length} 张（${errs.join("；")}）`);
      } else {
        toast.success(`已批量裁决 ${okCount} 张能力卡`);
      }
      setSelected(new Set());
      await load(taskId);
    } catch (e) {
      toast.error(`批量裁决失败：${errorMessage(e)}`);
    } finally {
      setBatchBusy(false);
      setConfirmAction(null);
    }
  }

  function toggle(capId: number, checked: boolean) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) next.add(capId);
      else next.delete(capId);
      return next;
    });
  }

  const cards = data?.cards ?? [];
  const pendingAll = cards.filter(isPending);
  const filtered = React.useMemo(() => (pendingOnly ? cards.filter(isPending) : cards), [cards, pendingOnly]);

  React.useEffect(() => {
    const lastPage = Math.max(1, Math.ceil(filtered.length / pageSize));
    if (page > lastPage) setPage(lastPage);
  }, [filtered.length, page, pageSize]);

  const safePage = Math.min(Math.max(1, page), Math.max(1, Math.ceil(filtered.length / pageSize)));
  const pageCards = filtered.slice((safePage - 1) * pageSize, safePage * pageSize);
  const pageCardIds = pageCards.map((c) => c.id);
  const pageAllSelected = pageCardIds.length > 0 && pageCardIds.every((id) => selected.has(id));

  // 组名取自覆盖度自检（同一份响应），组内计数按当前筛选口径统计。
  const groupNames = React.useMemo(() => new Map((data?.coverage ?? []).map((row) => [row.group, row.name])), [data]);
  const groupTotals = React.useMemo(() => {
    const totals = new Map<number, { total: number; pending: number }>();
    for (const card of filtered) {
      const entry = totals.get(card.group) ?? { total: 0, pending: 0 };
      entry.total += 1;
      if (isPending(card)) entry.pending += 1;
      totals.set(card.group, entry);
    }
    return totals;
  }, [filtered]);
  const pageGroups = React.useMemo(() => {
    const grouped = new Map<number, CapabilityCard[]>();
    for (const card of pageCards) {
      const list = grouped.get(card.group) ?? [];
      list.push(card);
      grouped.set(card.group, list);
    }
    return [...grouped.entries()].sort((a, b) => a[0] - b[0]);
  }, [pageCards]);

  function toggleGroup(group: number) {
    setCollapsedGroups((prev) => {
      const next = new Set(prev);
      if (next.has(group)) next.delete(group);
      else next.add(group);
      return next;
    });
  }

  function selectPage() {
    setSelected((prev) => {
      const next = new Set(prev);
      for (const id of pageCardIds) {
        if (pageAllSelected) next.delete(id);
        else next.add(id);
      }
      return next;
    });
  }

  function selectPending() {
    setSelected(new Set(pendingAll.map((c) => c.id)));
  }

  const emptyState = (() => {
    if (!taskId) {
      return {
        title: "先选择一个任务",
        description: "选择任务后，这里会列出这个站能做什么、有什么证据。",
      };
    }
    if (pendingOnly && pendingAll.length === 0 && cards.length > 0) {
      return {
        title: "没有待裁决的能力卡",
        description: "所有能力卡都已裁决，切回「全部」可以查看历史结果。",
      };
    }
    return {
      title: "暂无能力卡",
      description: "跑一个任务后会自动派生能力卡。",
    };
  })();

  const coverage = data?.coverage ?? [];

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">能力菜单</h1>
          <p className="text-muted-foreground text-sm">
            这个站能做什么、有什么证据、要不要继续。默认「只拿证据」最省风险。
          </p>
        </div>
        <Select value={taskId || ALL_TASKS} onValueChange={(value) => setTaskId(value === ALL_TASKS ? "" : value)}>
          <SelectTrigger size="sm" className="w-full sm:w-64" aria-label="选择任务">
            <SelectValue placeholder="选择任务" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL_TASKS}>选择任务…</SelectItem>
            {tasks.map((t) => (
              <SelectItem key={t.id} value={t.id}>
                <span className="max-w-[18rem] truncate" title={t.name || t.description || t.id}>
                  {t.name || t.description || t.id}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {prog && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">进度自检（顾问视角）</CardTitle>
            <CardDescription>{prog.diagnosis?.diagnosis || "暂无诊断"}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex flex-wrap gap-2">
              {(prog.stall?.stuck ?? []).map((s) => (
                <Badge key={s} variant="outline">
                  {s}
                </Badge>
              ))}
              <Badge variant="outline">连续无产出 {prog.stall?.no_progress_rounds ?? 0} 轮</Badge>
              <Badge variant="outline">覆盖 {(prog.stall?.coverage_pct ?? 0).toFixed(0)}%</Badge>
              <Badge variant="outline">预算 {(prog.stall?.budget_burn_pct ?? 0).toFixed(0)}%</Badge>
            </div>
            {(prog.diagnosis?.hints ?? []).map((h) => (
              <div key={h} className="rounded-md bg-muted/50 p-2 text-xs">
                {h}
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <ToggleGroup
          type="single"
          value={pendingOnly ? "pending" : "all"}
          onValueChange={(value) => value && setPendingOnly(value === "pending")}
          variant="outline"
          size="sm"
          spacing={0}
        >
          <ToggleGroupItem value="all">全部</ToggleGroupItem>
          <ToggleGroupItem value="pending">仅看待裁决</ToggleGroupItem>
        </ToggleGroup>
        <span className="text-muted-foreground text-xs tabular-nums">
          共 {cards.length} 张 · 待裁决 {pendingAll.length} 张
        </span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {selected.size > 0 && (
            <span className="text-muted-foreground text-xs tabular-nums">已选 {selected.size} 张</span>
          )}
          <Button size="sm" variant="outline" disabled={pageCardIds.length === 0} onClick={selectPage}>
            {pageAllSelected ? `取消本页（${pageCardIds.length}）` : `全选本页（${pageCardIds.length}）`}
          </Button>
          <Button size="sm" variant="outline" disabled={pendingAll.length === 0} onClick={selectPending}>
            全选待裁决（{pendingAll.length}）
          </Button>
          <Button size="sm" variant="ghost" disabled={selected.size === 0} onClick={() => setSelected(new Set())}>
            清空选择
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-2 rounded-lg border bg-muted/30 p-2">
        <span className="text-sm font-medium tabular-nums">已选 {selected.size} 张</span>
        <Button size="sm" disabled={batchBusy || selected.size === 0} onClick={() => setConfirmAction("evidence")}>
          只拿证据（推荐）
        </Button>
        <Button
          size="sm"
          variant="outline"
          disabled={batchBusy || selected.size === 0}
          onClick={() => setConfirmAction("continue")}
        >
          继续深挖（15 分钟）
        </Button>
        <Button
          size="sm"
          variant="ghost"
          disabled={batchBusy || selected.size === 0}
          onClick={() => setConfirmAction("switch")}
        >
          换目标
        </Button>
        {batchBusy && (
          <span className="text-muted-foreground flex items-center gap-1.5 text-xs">
            <Spinner />
            批量裁决中…
          </span>
        )}
      </div>

      {loading && !loaded ? (
        <div className="flex flex-col gap-3" role="status" aria-label="加载能力卡">
          <Card>
            <CardHeader>
              <Skeleton className="h-4 w-32" />
              <Skeleton className="mt-2 h-3 w-56" />
            </CardHeader>
            <CardContent className="space-y-3">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-20 w-full" />
            </CardContent>
          </Card>
        </div>
      ) : filtered.length === 0 ? (
        <Card>
          <CardContent>
            <Empty className="border">
              <EmptyHeader>
                <EmptyTitle>{emptyState.title}</EmptyTitle>
                <EmptyDescription>{emptyState.description}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </CardContent>
        </Card>
      ) : (
        <>
          <div className="flex flex-col gap-3">
            {pageGroups.map(([group, groupCards]) => {
              const collapsed = collapsedGroups.has(group);
              const totals = groupTotals.get(group) ?? { total: groupCards.length, pending: 0 };
              return (
                <Card key={group} className="gap-0 py-0">
                  <CardHeader className="px-4 py-3">
                    <div className="flex min-w-0 flex-wrap items-center gap-3">
                      <button
                        type="button"
                        className="flex min-w-0 flex-1 items-center gap-3 text-left"
                        aria-expanded={!collapsed}
                        onClick={() => toggleGroup(group)}
                      >
                        <ChevronRightIcon
                          className={cn(
                            "text-muted-foreground size-4 shrink-0 transition-transform",
                            !collapsed && "rotate-90",
                          )}
                        />
                        <div className="flex min-w-0 flex-col gap-1">
                          <CardTitle className="truncate text-sm">
                            组 {group}
                            {groupNames.get(group) ? ` · ${groupNames.get(group)}` : ""}
                          </CardTitle>
                          <CardDescription className="tabular-nums">
                            共 {totals.total} 张 · 待裁决 {totals.pending} 张 · 本页 {groupCards.length} 张
                          </CardDescription>
                        </div>
                      </button>
                      {totals.pending > 0 && (
                        <Badge className="border-transparent bg-amber-500/15 text-amber-600 dark:text-amber-400">
                          {totals.pending} 待裁决
                        </Badge>
                      )}
                    </div>
                  </CardHeader>
                  {!collapsed && (
                    <CardContent className="space-y-3 px-4">
                      {groupCards.map((c) => {
                        const checked = selected.has(c.id);
                        return (
                          <div
                            key={c.id}
                            className={cn(
                              "rounded-lg border p-3 transition-colors",
                              checked && "border-primary/40 bg-primary/5",
                            )}
                          >
                            <div className="flex flex-wrap items-center gap-2">
                              <Checkbox
                                aria-label={`选择能力卡 ${c.id}`}
                                checked={checked}
                                disabled={batchBusy}
                                onCheckedChange={(value) => toggle(c.id, value === true)}
                              />
                              <Badge className={cn("border-transparent", proofTone[c.proof_level] ?? "")}>
                                {c.proof_level}
                              </Badge>
                              <span className="text-sm font-medium">{c.can}</span>
                              <span className="text-muted-foreground text-xs">
                                {c.confidence === "observed" ? "亲眼看" : "推断"}
                              </span>
                              <Badge
                                className={cn(
                                  "ml-auto border-transparent",
                                  capabilityStatusTone[c.status || "open"] ?? "bg-muted text-muted-foreground",
                                )}
                              >
                                {capStatusLabel(c.status)}
                              </Badge>
                            </div>
                            {c.plain && <div className="text-muted-foreground mt-1 text-sm">{c.plain}</div>}
                            {c.surface && <div className="mt-1 text-xs">入口：{c.surface}</div>}
                            {c.evidence && <div className="text-muted-foreground mt-1 text-xs">证据：{c.evidence}</div>}
                            {c.impact && <div className="mt-1 text-xs">影响：{c.impact}</div>}
                            {c.risk && (
                              <div className="mt-1 text-xs text-amber-600 dark:text-amber-400">代价：{c.risk}</div>
                            )}
                            <div className="mt-2 flex flex-wrap gap-2">
                              <Button
                                size="sm"
                                disabled={batchBusy || busy === c.id}
                                onClick={() => decide(c, "evidence")}
                              >
                                {busy === c.id ? <Spinner /> : null}
                                只拿证据（推荐）
                              </Button>
                              <Button
                                size="sm"
                                variant="outline"
                                disabled={batchBusy || busy === c.id}
                                onClick={() => decide(c, "continue")}
                              >
                                继续深挖（15 分钟）
                              </Button>
                              <Button
                                size="sm"
                                variant="ghost"
                                disabled={batchBusy || busy === c.id}
                                onClick={() => decide(c, "switch")}
                              >
                                换目标
                              </Button>
                            </div>
                          </div>
                        );
                      })}
                    </CardContent>
                  )}
                </Card>
              );
            })}
          </div>

          <Card className="gap-0 py-0">
            <CardContent className="px-0">
              <TablePagination
                page={safePage}
                pageSize={pageSize}
                total={filtered.length}
                onPageChange={setPage}
                onPageSizeChange={(nextSize) => {
                  setPageSize(nextSize);
                  setPage(1);
                }}
                pageSizeOptions={[10, 20, 50]}
              />
            </CardContent>
          </Card>
        </>
      )}

      {coverage.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">覆盖度自检（哪些组测过）</CardTitle>
            <CardDescription>已测组会显示能力卡数量，未测组会标出「未测」。</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-1.5 text-xs sm:grid-cols-3 md:grid-cols-4">
              {coverage.map((r) => (
                <div
                  key={r.group}
                  className={cn(
                    "flex items-center justify-between gap-2 rounded-md border px-2 py-1",
                    r.tested ? "border-emerald-500/40 bg-emerald-500/5" : "text-muted-foreground",
                  )}
                >
                  <span className="truncate" title={`${r.group}. ${r.name}`}>
                    {r.group}. {r.name}
                  </span>
                  <span className="shrink-0 tabular-nums">{r.tested ? `${r.count} 项` : "未测"}</span>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {data?.plain && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">给非技术读者的原话</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="whitespace-pre-wrap text-xs leading-relaxed">{data.plain}</pre>
          </CardContent>
        </Card>
      )}

      <AlertDialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open && !batchBusy) setConfirmAction(null);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认批量裁决 {selected.size} 张能力卡？</AlertDialogTitle>
            <AlertDialogDescription>
              将对所选能力卡统一执行「{confirmAction ? ACTION_LABEL[confirmAction] : ""}」，裁决会立即写入台账，
              并影响后续探索方向。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={batchBusy}>取消</AlertDialogCancel>
            <AlertDialogAction
              disabled={batchBusy || selected.size === 0}
              onClick={(event) => {
                event.preventDefault();
                if (confirmAction) void decideBatch(confirmAction);
              }}
            >
              {batchBusy && <Spinner />}
              {batchBusy ? "裁决中…" : "确认裁决"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
