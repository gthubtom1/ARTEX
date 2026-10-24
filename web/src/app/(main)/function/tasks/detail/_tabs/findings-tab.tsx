"use client";

import * as React from "react";

import Link from "next/link";

import { ArrowDownIcon, ArrowUpIcon, ArrowUpRightIcon, ChevronRightIcon } from "lucide-react";
import { toast } from "sonner";

import { FindingCaseList } from "@/components/finding-case-list";
import { StatusBadge } from "@/components/status-badge";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger } from "@/components/ui/select";
import { api, briefapi } from "@/lib/api";
import { useStoredSortPreference } from "@/lib/sort-preference";
import { statusMeta } from "@/lib/status";
import type { Finding, FindingStatus } from "@/lib/types";
import { cn } from "@/lib/utils";

type FindingSortField = "time";

const FINDING_SORT_FIELDS: readonly FindingSortField[] = ["time"];
const FINDING_SORT_PREFERENCE_KEY = "artex_task_findings_sort";

function findingLabel(finding: Finding): string {
  if (finding.name?.trim()) return finding.name;
  if (finding.vulnclass?.trim()) return finding.vulnclass;
  return "未分类";
}

const FINDING_STATUSES: FindingStatus[] = [
  "pending",
  "in_progress",
  "confirmed",
  "resolved",
  "fixed",
  "false_positive",
  "ignored",
  "duplicate",
  "risk_accepted",
];

function Row({
  f,
  contextTaskId,
  onStatus,
  standing,
}: {
  f: Finding;
  contextTaskId: string;
  onStatus: (f: Finding, next: FindingStatus) => void;
  /** 三段式（docs/23）：已成立 / 差一步 / 换不到。来自 /brief，与战况卡同源。 */
  standing?: { standing: "got" | "one_step" | "no_gain"; gap?: string };
}) {
  const [open, setOpen] = React.useState(false);
  return (
    <div className="border-b last:border-b-0">
      <div className="flex w-full items-center gap-3 px-4 py-3 text-sm hover:bg-accent/40">
        <button
          type="button"
          onClick={() => setOpen((o) => !o)}
          className="flex min-w-0 flex-1 items-center gap-3 text-left"
        >
          <ChevronRightIcon
            className={cn("size-4 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")}
          />
          <StatusBadge domain="severity" value={f.severity} dot />
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <div className="flex min-w-0 items-center gap-2">
              <span className="truncate font-medium">{findingLabel(f)}</span>
              {standing && (
                <span
                  className={cn(
                    "shrink-0 rounded px-1 text-[9.5px] font-bold",
                    standing.standing === "got"
                      ? "bg-emerald-600 text-white"
                      : standing.standing === "no_gain"
                        ? "bg-slate-500/25 text-slate-600 dark:text-slate-300"
                        : "bg-amber-500/20 text-amber-700 dark:text-amber-300",
                  )}
                  title={
                    standing.standing === "got"
                      ? "已成立：对应能力行已拿到，有证据"
                      : standing.standing === "no_gain"
                        ? "换不到东西：这条洞没有通向任何能力行"
                        : `差一步：还缺「${standing.gap || "下一个能力"}」`
                  }
                >
                  {standing.standing === "got" ? "已成立" : standing.standing === "no_gain" ? "换不到" : "差一步"}
                </span>
              )}
              {f.inherited && f.source_task_id && (
                <Badge variant="outline" className="shrink-0">
                  来源 #{f.source_task_id} · 只读
                </Badge>
              )}
            </div>
            <span className="truncate text-xs text-muted-foreground">{f.summary}</span>
          </div>
        </button>
        <Badge variant="outline">流量证据 {f.traffic_count ?? 0} 条</Badge>
        {f.assets && f.assets.length > 0 && (
          <div className="hidden shrink-0 flex-wrap justify-end gap-1 sm:flex">
            {f.assets.slice(0, 2).map((a) => (
              <code
                key={a.id}
                className="max-w-[10rem] truncate rounded bg-muted px-1.5 py-0.5 font-mono text-xs"
                title={`${a.type} · ${a.label}`}
              >
                {a.label}
              </code>
            ))}
            {f.assets.length > 2 && <span className="text-xs text-muted-foreground">+{f.assets.length - 2}</span>}
          </div>
        )}
        {f.finding_id && !f.inherited ? (
          <Select value={f.status} onValueChange={(v) => onStatus(f, v as FindingStatus)}>
            <SelectTrigger size="sm" className="h-7 w-28 shrink-0 border-none px-1 shadow-none focus-visible:ring-0">
              <StatusBadge domain="finding" value={f.status} dot />
            </SelectTrigger>
            <SelectContent position="popper" align="end">
              <SelectGroup>
                {FINDING_STATUSES.map((st) => (
                  <SelectItem key={st} value={st}>
                    {statusMeta("finding", st).label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        ) : (
          <StatusBadge domain="finding" value={f.status} dot />
        )}
        <span className="hidden shrink-0 text-xs text-muted-foreground md:block">
          {new Date(f.ts).toLocaleString("zh-CN")}
        </span>
        {f.finding_id && (
          <Link
            href={
              f.inherited
                ? `/function/findings/detail?id=${f.finding_id}&context_task=${contextTaskId}`
                : `/function/findings/detail?id=${f.finding_id}`
            }
            className="text-muted-foreground hover:text-primary inline-flex shrink-0 items-center gap-0.5 text-xs"
            title="查看漏洞详情"
          >
            详情
            <ArrowUpRightIcon className="size-3" />
          </Link>
        )}
      </div>
      {open && (
        <div className="bg-muted/30 px-4 pb-4 pl-11">
          <div className="mb-1 text-xs font-medium text-muted-foreground">证据 / PoC</div>
          <pre className="overflow-auto rounded-md border bg-background p-3 font-mono text-xs whitespace-pre-wrap">
            {f.evidence}
          </pre>
        </div>
      )}
    </div>
  );
}

export function FindingsTab({ taskId }: { taskId: string }) {
  const [findings, setFindings] = React.useState<Finding[]>([]);
  // 三段式映射（id → got/one_step/no_gain）：一次拉 /brief，和战况卡同一口径。
  const [standingByID, setStandingByID] = React.useState<
    Record<number, { standing: "got" | "one_step" | "no_gain"; gap?: string }>
  >({});
  const [summary, setSummary] = React.useState<{ got: number; oneStep: number; noGain: number; gap: string } | null>(
    null,
  );
  const [sortPreference, setSortPreference] = useStoredSortPreference(
    FINDING_SORT_PREFERENCE_KEY,
    FINDING_SORT_FIELDS,
    "time",
    "desc",
  );

  React.useEffect(() => {
    let active = true;
    // 战况（三段式）——只拉一次，失败就退化成"没有标签"，不影响列表
    briefapi
      .get(taskId)
      .then((b) => {
        if (!active) return;
        const map: Record<number, { standing: "got" | "one_step" | "no_gain"; gap?: string }> = {};
        for (const c of b.can ?? []) {
          map[c.finding_id] = { standing: c.standing ?? "one_step", gap: c.gap };
        }
        setStandingByID(map);
        const oneStepCan = (b.can ?? []).filter((c) => (c.standing ?? "one_step") === "one_step");
        setSummary({
          got: (b.can ?? []).filter((c) => c.standing === "got").length,
          oneStep: oneStepCan.length,
          noGain: (b.can ?? []).filter((c) => c.standing === "no_gain").length,
          gap: oneStepCan[0]?.gap ?? "",
        });
      })
      .catch(() => void 0);
    const load = () => {
      api
        .findings(taskId)
        .then((fs) => {
          if (!active) return;
          setFindings(fs);
          // 案子行的 matched_ids 可能是 findings 表 id（13），而 /brief 给的是节点 id（341）。
          // 这里把两份 id 都映射到同一个 standing，避免"查不到就不显示"。
          setStandingByID((prev) => {
            const next = { ...prev };
            for (const f of fs) {
              const st = prev[Number(f.id)] ?? (f.finding_id ? prev[Number(f.finding_id)] : undefined);
              if (!st) continue;
              next[Number(f.id)] = st;
              if (f.finding_id) next[Number(f.finding_id)] = st;
            }
            return next;
          });
        })
        .catch(() => {
          // Keep the last successful snapshot during transient refresh failures.
        });
    };
    load();
    const t = setInterval(load, 3000);
    return () => {
      active = false;
      clearInterval(t);
    };
  }, [taskId]);

  const onStatus = React.useCallback(async (f: Finding, next: FindingStatus) => {
    if (f.inherited || !f.finding_id || next === f.status) return;
    const prev = f.status;
    setFindings((cur) => cur.map((x) => (x.id === f.id ? { ...x, status: next } : x)));
    try {
      await api.setFindingStatus(f.finding_id, next);
      toast.success(`已标记为「${statusMeta("finding", next).label}」`);
    } catch (e) {
      setFindings((cur) => cur.map((x) => (x.id === f.id ? { ...x, status: prev } : x)));
      toast.error("更新失败：" + (e as Error).message);
    }
  }, []);

  const items = findings
    .filter((f) => f.task_id === taskId || f.inherited)
    .sort((a, b) => {
      const compared = Date.parse(a.ts) - Date.parse(b.ts);
      if (compared !== 0) return sortPreference.direction === "asc" ? compared : -compared;
      return b.id.localeCompare(a.id, undefined, { numeric: true });
    });

  return (
    <div className="flex flex-col gap-4">
      {/* 三段式汇总（docs/23）：不依赖 id 匹配，直接从 /brief 数出来，
          保证"洞值不值钱"在发现页也一眼可见（与战况卡同源） */}
      {summary && (
        <div className="mx-4 mb-2 flex flex-wrap items-center gap-2 rounded border px-3 py-1.5 text-[11.5px] lg:mx-6">
          <span className="text-[10px] text-muted-foreground">这些洞值不值钱</span>
          <span className="rounded bg-emerald-600 px-1 text-[10px] font-bold text-white">已成立 {summary.got}</span>
          <span className="rounded bg-amber-500/20 px-1 text-[10px] font-bold text-amber-700 dark:text-amber-300">
            差一步 {summary.oneStep}
          </span>
          <span className="rounded bg-slate-500/25 px-1 text-[10px] font-bold text-slate-600 dark:text-slate-300">
            换不到 {summary.noGain}
          </span>
          {summary.gap && <span className="text-muted-foreground">最接近的一个：缺「{summary.gap}」</span>}
        </div>
      )}
      <FindingCaseList query={{ task: taskId, sort: "time" }} standingByID={standingByID} />
      {[
        ...new Set(
          items
            .filter((f) => f.inherited)
            .map((f) => f.source_task_id)
            .filter((id): id is string => !!id),
        ),
      ].map((source) => (
        <div key={source}>
          <p className="mb-2 text-sm text-muted-foreground">继承任务 #{source} · 只读</p>
          <FindingCaseList query={{ task: source, sort: "time" }} contextTask={taskId} readOnly />
        </div>
      ))}
      <details>
        <summary className="cursor-pointer text-sm text-muted-foreground">查看原始上报列表</summary>
        <Card className="overflow-hidden py-0">
          <CardContent className="px-0">
            <div className="flex items-center border-b px-4 py-2 text-xs text-muted-foreground">
              <span className="min-w-0 flex-1">漏洞</span>
              <button
                type="button"
                className="inline-flex items-center gap-1 outline-none focus-visible:underline"
                aria-label={`发现时间当前${sortPreference.direction === "asc" ? "正序" : "倒序"}，点击切换排序方向`}
                onClick={() =>
                  setSortPreference((current) => ({
                    field: "time",
                    direction: current.direction === "asc" ? "desc" : "asc",
                  }))
                }
              >
                <span>发现时间</span>
                {sortPreference.direction === "asc" ? (
                  <ArrowUpIcon className="size-3.5" />
                ) : (
                  <ArrowDownIcon className="size-3.5" />
                )}
              </button>
            </div>
            {items.map((f) => (
              <Row
                key={f.id}
                f={f}
                contextTaskId={taskId}
                onStatus={onStatus}
                standing={
                  // 注意：/brief 的 finding_id 是 exploration 节点 id（= findings[].id），
                  // 不是 findings 表的 finding_id（真机上是 341 vs 13，用错了就永远匹配不上）
                  standingByID[Number(f.id)] ?? (f.finding_id ? standingByID[Number(f.finding_id)] : undefined)
                }
              />
            ))}
            {items.length === 0 && (
              <p className="px-4 py-8 text-center text-sm text-muted-foreground">本任务及直接关联任务暂无确认发现。</p>
            )}
          </CardContent>
        </Card>
      </details>
    </div>
  );
}
