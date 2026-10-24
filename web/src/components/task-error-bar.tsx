"use client";

import * as React from "react";

import { useRouter } from "next/navigation";
import { AlertTriangleIcon, ChevronDownIcon, ChevronUpIcon, WrenchIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { p8api, type TaskErrorsResponse } from "@/lib/api";

// P8-8：任务详情的「错误可解释」聚合条。
//
// 零基础用户最常问的不是"这个洞怎么打"，而是"它为什么不动了"。这里把散在活动流/
// 审计/工具输出里的错误按固定 5 类聚成一行，每类带一句人话 + 一个「一键修」跳转。
//
// 没有错误时不渲染（不占位置）。

const CLASS_STYLE: Record<string, string> = {
  env_error: "border-amber-500/40 bg-amber-500/5 text-amber-700 dark:text-amber-400",
  tool_missing: "border-sky-500/40 bg-sky-500/5 text-sky-700 dark:text-sky-400",
  target_blocked: "border-orange-500/40 bg-orange-500/5 text-orange-700 dark:text-orange-400",
  self_error: "border-rose-500/40 bg-rose-500/5 text-rose-700 dark:text-rose-400",
  policy_blocked: "border-violet-500/40 bg-violet-500/5 text-violet-700 dark:text-violet-400",
};

export function TaskErrorBar({ taskId, windowMin = 60 }: { taskId: string; windowMin?: number }) {
  const router = useRouter();
  const [data, setData] = React.useState<TaskErrorsResponse | null>(null);
  const [expanded, setExpanded] = React.useState<string | null>(null);

  const load = React.useCallback(async () => {
    if (!taskId) return;
    try {
      const r = await p8api.taskErrors(taskId, windowMin);
      setData(r);
    } catch {
      // 错误面板本身失败不应该打扰用户（它是辅助信息，不是主流程）。
      setData(null);
    }
  }, [taskId, windowMin]);

  React.useEffect(() => {
    void load();
    const t = setInterval(() => void load(), 30_000);
    return () => clearInterval(t);
  }, [load]);

  if (!data || !data.groups || data.groups.length === 0) return null;

  function fix(target: string) {
    if (!target) return;
    if (target.startsWith("open:")) router.push(target.slice(5));
  }

  return (
    <div className="border-b bg-muted/20 px-4 py-2 lg:px-6">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-[11px]">
        <span className="flex items-center gap-1 font-medium text-muted-foreground">
          <AlertTriangleIcon className="size-3.5" />
          当前卡在
        </span>
        {data.groups.map((g) => (
          <button
            key={g.class}
            type="button"
            onClick={() => setExpanded(expanded === g.class ? null : g.class)}
            className={
              "flex items-center gap-1 rounded-full border px-2 py-0.5 transition hover:brightness-95 " +
              (CLASS_STYLE[g.class] ?? "border-border bg-muted/40")
            }
            title={g.sample}
          >
            <span>{g.label}</span>
            <span className="font-mono tabular-nums">×{g.count}</span>
            {expanded === g.class ? <ChevronUpIcon className="size-3" /> : <ChevronDownIcon className="size-3" />}
          </button>
        ))}
        <span className="text-muted-foreground">
          近 {data.window_min} 分钟 · 共 {data.total} 条
        </span>
        <Button
          variant="ghost"
          size="sm"
          className="ml-auto h-6 px-2 text-[11px]"
          onClick={() => {
            const first = data.groups.find((g) => g.fix);
            if (first) fix(first.fix);
          }}
          disabled={!data.groups.some((g) => g.fix)}
        >
          <WrenchIcon className="mr-1 size-3" />
          一键修
        </Button>
      </div>

      {expanded && (() => {
        const g = data.groups.find((x) => x.class === expanded);
        if (!g) return null;
        return (
          <div className="mt-2 rounded-md border bg-background px-2.5 py-2 text-[11px]">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{g.label}</span>
              {g.install ? (
                <button
                  type="button"
                  className="rounded border border-amber-500/50 bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] text-amber-700 hover:bg-amber-500/20 dark:text-amber-300"
                  title="点击复制，装完到「系统 → 环境」点重新扫描"
                  onClick={() => void navigator.clipboard.writeText(g.install ?? "")}
                >
                  复制安装命令：{g.install.length > 46 ? g.install.slice(0, 46) + "…" : g.install}
                </button>
              ) : g.tools && g.tools.length > 0 ? (
                <span className="font-mono text-[10px] text-muted-foreground">工具：{g.tools.join(" / ")}</span>
              ) : null}
              {g.fix ? (
                <Button variant="outline" size="sm" className="ml-auto h-6 px-2 text-[11px]" onClick={() => fix(g.fix)}>
                  去处理
                </Button>
              ) : null}
            </div>
            <p className="mt-1 text-muted-foreground">{g.action}</p>
            {g.sample ? (
              <pre className="mt-1 max-h-24 overflow-auto whitespace-pre-wrap break-all rounded bg-muted/40 p-1.5 font-mono text-[10px] text-muted-foreground">
                {g.sample}
              </pre>
            ) : null}
          </div>
        );
      })()}
    </div>
  );
}
