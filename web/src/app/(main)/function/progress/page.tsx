"use client";

import * as React from "react";

import { AlertTriangleIcon, CheckCircle2Icon, CircleDashedIcon, HourglassIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { roadmapapi, type RoadmapItem, type RoadmapResponse } from "@/lib/api";
import { cn } from "@/lib/utils";

// 进展清单页（2026-10-25 用户原话：「你这个有个清单我都不知道到底有哪些没有完成，
// 你为什么不能在我的页面上面显示出来呢」）。
//
// 设计纪律（docs/28 界面规格）：
//   - 顶部先给一句话结论（还剩几件），不让用户自己算；
//   - 每条固定 76px 状态标在**最左**（绿=做完 / 琥珀=没做 / 红=等你点头或换环境）；
//   - 没做的必须写"为什么没做"和"怎么算做完"，否则后端会把它标成"没写原因"（红字）；
//   - 默认只看没做的，做完的折叠起来（一屏一问）。

const STATUS_META: Record<string, { label: string; cls: string; icon: typeof CheckCircle2Icon }> = {
  done: { label: "已完成", cls: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-300", icon: CheckCircle2Icon },
  doing: { label: "在做", cls: "bg-sky-500/15 text-sky-700 dark:text-sky-300", icon: HourglassIcon },
  todo: { label: "没做", cls: "bg-amber-500/15 text-amber-700 dark:text-amber-300", icon: CircleDashedIcon },
  blocked: { label: "等条件", cls: "bg-rose-500/15 text-rose-700 dark:text-rose-300", icon: AlertTriangleIcon },
};

function StatusChip({ status }: { status: string }) {
  const meta = STATUS_META[status] ?? STATUS_META.todo;
  const Icon = meta.icon;
  return (
    <span
      className={cn(
        "inline-flex w-[76px] shrink-0 items-center justify-center gap-1 rounded px-1.5 py-0.5 text-[11px] font-medium",
        meta.cls,
      )}
    >
      <Icon className="size-3" />
      {meta.label}
    </span>
  );
}

function ItemRow({ item }: { item: RoadmapItem }) {
  const missingWhy = item.why_not?.includes("没写原因");
  const missingProof = item.evidence?.includes("没写判据");
  return (
    <div className="flex items-start gap-2 border-b py-2 last:border-b-0">
      <StatusChip status={item.status} />
      <div className="min-w-0 flex-1">
        <div className="text-sm font-medium">{item.title}</div>
        {item.status === "done" ? (
          <div className="mt-0.5 text-xs text-muted-foreground">判据：{item.evidence}</div>
        ) : (
          <>
            <div className={cn("mt-0.5 text-xs", missingWhy ? "text-rose-600" : "text-muted-foreground")}>
              为什么没做：{item.why_not}
            </div>
            {item.accept && <div className="mt-0.5 text-xs text-muted-foreground">怎么算做完：{item.accept}</div>}
          </>
        )}
        {missingProof && <div className="mt-0.5 text-xs text-rose-600">这条标了已完成，但没写判据</div>}
      </div>
    </div>
  );
}

export default function ProgressPage() {
  const [data, setData] = React.useState<RoadmapResponse | null>(null);
  const [onlyOpen, setOnlyOpen] = React.useState(true);
  const [showDone, setShowDone] = React.useState<Record<string, boolean>>({});
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
    roadmapapi
      .get()
      .then(setData)
      .catch(() => setErr("进展清单读不到（后端可能还没升级到带 /api/roadmap 的版本）"));
  }, []);

  if (err) {
    return (
      <div className="p-6 text-sm text-rose-600">
        {err}
        <div className="mt-2 text-muted-foreground">下一步：确认服务版本 ≥ 0.11.57，然后刷新这一页。</div>
      </div>
    );
  }
  if (!data) {
    return (
      <div className="space-y-3 p-6">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    );
  }

  return (
    <div className="mx-auto max-w-4xl space-y-3 p-4">
      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-base">这个产品现在到哪了</CardTitle>
        </CardHeader>
        <CardContent className="space-y-2">
          <div className="text-lg font-semibold">{data.headline}</div>
          <div className="flex flex-wrap items-center gap-3 text-sm">
            <span className="inline-flex items-center gap-1">
              <span className="size-2 rounded-full bg-emerald-500" /> 已完成 {data.done}
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="size-2 rounded-full bg-amber-500" /> 没做 {data.todo - data.blocked}
            </span>
            <span className="inline-flex items-center gap-1">
              <span className="size-2 rounded-full bg-rose-500" /> 等条件 {data.blocked}
            </span>
            <span className="text-muted-foreground">共 {data.total} 件事 · 清单版本 {data.version}（{data.updated}）</span>
          </div>
          <div className="text-xs text-muted-foreground">{data.intro}</div>
          <div className="flex gap-2 pt-1">
            <Button size="sm" variant={onlyOpen ? "default" : "outline"} onClick={() => setOnlyOpen(true)}>
              只看没做的
            </Button>
            <Button size="sm" variant={!onlyOpen ? "default" : "outline"} onClick={() => setOnlyOpen(false)}>
              全部
            </Button>
          </div>
        </CardContent>
      </Card>

      {data.groups.map((g) => {
        const items = onlyOpen ? g.items.filter((i) => i.status !== "done") : g.items;
        const doneCount = g.items.filter((i) => i.status === "done").length;
        const open = showDone[g.id] ?? false;
        if (items.length === 0 && onlyOpen) {
          return (
            <Card key={g.id}>
              <CardContent className="flex items-center gap-2 py-3 text-sm">
                <StatusChip status="done" />
                <span className="font-medium">{g.title}</span>
                <span className="text-muted-foreground">这一组已经全部做完（{doneCount} 件）</span>
              </CardContent>
            </Card>
          );
        }
        return (
          <Card key={g.id}>
            <CardHeader className="pb-1">
              <CardTitle className="flex items-baseline gap-2 text-sm">
                {g.title}
                <span className="text-xs font-normal text-muted-foreground">{g.why}</span>
              </CardTitle>
            </CardHeader>
            <CardContent className="pt-0">
              {items.map((it) => (
                <ItemRow key={it.id} item={it} />
              ))}
              {!onlyOpen && doneCount > 0 && (
                <button
                  type="button"
                  className="mt-1 text-xs text-muted-foreground underline-offset-2 hover:underline"
                  onClick={() => setShowDone((s) => ({ ...s, [g.id]: !open }))}
                >
                  {open ? "收起已完成的" : `已完成 ${doneCount} 件也在上面 👆`}
                </button>
              )}
            </CardContent>
          </Card>
        );
      })}

      <p className="pb-6 text-xs text-muted-foreground">
        这张单子来自仓库的 <code>server/roadmap.json</code>：改它 = 改这张页 = 改发布说明。
        「没做」的每一条都必须写清为什么没做、怎么算做完，写不清会被标红。
      </p>
    </div>
  );
}
