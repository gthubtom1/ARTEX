"use client";

import * as React from "react";

import Link from "next/link";

import { ChevronDownIcon, CopyIcon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { type BenchCaseResult, type BenchRun, type BenchRunRow, type BenchSummary, benchapi } from "@/lib/api";
import { cn } from "@/lib/utils";

// 基准跑分面板：只读。最近一次 / 历史 / 单次明细 / 用例清单四块。
// 纪律：同 set + 同模型才可比，所以环比只在后端按 (set, model) 配对后才展示。

type BenchCaseInfo = {
  key: string;
  name: string;
  category: string;
  url: string;
  timeout_s: number;
};

type BenchCasesResponse = {
  cases: BenchCaseInfo[];
  count: number;
  dir: string;
  hint?: string;
  detail?: string;
};

const SKELETON_ROWS = ["one", "two", "three", "four", "five"];

const OUTCOME_LABELS: Record<string, string> = {
  passed: "通过",
  failed: "失败",
  timeout: "超时",
  error: "错误",
};

const OUTCOME_TONES: Record<string, string> = {
  passed: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  failed: "bg-red-500/10 text-red-600 dark:text-red-400",
  timeout: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  error: "bg-muted text-foreground/60",
};

function fmtPct(ratio: number) {
  if (!Number.isFinite(ratio)) return "—";
  return `${(ratio * 100).toFixed(1)}%`;
}

function fmtDelta(pp: number) {
  if (pp > 0) return `▲ +${pp.toFixed(1)}pp`;
  if (pp < 0) return `▼ ${pp.toFixed(1)}pp`;
  return "0.0pp";
}

function fmtDur(ms: number) {
  if (!Number.isFinite(ms) || ms <= 0) return "—";
  const totalSec = Math.round(ms / 1000);
  if (totalSec < 60) return `${totalSec}s`;
  const min = Math.floor(totalSec / 60);
  const sec = totalSec % 60;
  return `${min}m ${String(sec).padStart(2, "0")}s`;
}

function fmtTok(n: number) {
  if (!Number.isFinite(n) || n <= 0) return "0";
  return Math.round(n).toLocaleString("en-US");
}

function fmtTime(ts?: string) {
  if (!ts) return "—";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return ts;
  return d.toLocaleString();
}

function deltaTone(pp: number) {
  if (pp > 0) return "text-emerald-600 dark:text-emerald-400";
  if (pp < 0) return "text-red-600 dark:text-red-400";
  return "text-muted-foreground";
}

// 后端 /bench/runs 里的 delta_pass_rate 是两次通过率之差（0..1），
// 展示统一换算成百分点（pp）。
function deltaPpOf(row: BenchRunRow) {
  return row.delta_pass_rate * 100;
}

function outcomeOf(c: BenchCaseResult) {
  return c.outcome || (c.passed ? "passed" : "failed");
}

function Stat({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="rounded-lg border bg-muted/30 px-3 py-2">
      <div className="text-muted-foreground text-xs">{label}</div>
      <div className="font-medium text-xl tabular-nums">{value}</div>
      {hint ? <div className="text-muted-foreground text-xs">{hint}</div> : null}
    </div>
  );
}

export default function BenchPage() {
  const [runs, setRuns] = React.useState<BenchRunRow[] | null>(null);
  const [resultsDir, setResultsDir] = React.useState("");
  const [runsLoading, setRunsLoading] = React.useState(true);
  const [latestKey, setLatestKey] = React.useState<string | null>(null);
  const [latestSummary, setLatestSummary] = React.useState<BenchSummary | null>(null);
  const [selectedKey, setSelectedKey] = React.useState<string | null>(null);
  const [selected, setSelected] = React.useState<BenchRun | null>(null);
  const [selectedLoading, setSelectedLoading] = React.useState(false);
  const [casesInfo, setCasesInfo] = React.useState<BenchCasesResponse | null>(null);
  const [casesLoading, setCasesLoading] = React.useState(true);
  const [casesOpen, setCasesOpen] = React.useState(false);
  const [_reloadSeq, setReloadSeq] = React.useState(0);

  const loadRuns = React.useCallback(async () => {
    setRunsLoading(true);
    try {
      const data = await benchapi.runs();
      setRuns(data.runs);
      setResultsDir(data.results_dir);
      const latest = data.runs[0]?.run_key ?? null;
      setLatestKey(latest);
      // 选中的那次被删掉/过期时回落到最近一次。
      setSelectedKey((current) => (current && data.runs.some((row) => row.run_key === current) ? current : latest));
      setReloadSeq((n) => n + 1);
    } catch (e) {
      toast.error(`加载跑批记录失败：${(e as Error).message}`);
    } finally {
      setRunsLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void loadRuns();
  }, [loadRuns]);

  // 最近一次运行的 summary（只有它带 avg_first_finding_ms）。
  React.useEffect(() => {
    if (!latestKey) {
      setLatestSummary(null);
      return;
    }
    let cancelled = false;
    benchapi
      .run(latestKey)
      .then((detail) => {
        if (!cancelled) setLatestSummary(detail.summary);
      })
      .catch((e: unknown) => {
        if (!cancelled) toast.error(`加载最近一次运行汇总失败：${(e as Error).message}`);
      });
    return () => {
      cancelled = true;
    };
  }, [latestKey]);

  // 选中运行的逐题明细。
  React.useEffect(() => {
    if (!selectedKey) {
      setSelected(null);
      return;
    }
    let cancelled = false;
    setSelectedLoading(true);
    benchapi
      .run(selectedKey)
      .then((detail) => {
        if (!cancelled) setSelected(detail.run);
      })
      .catch((e: unknown) => {
        if (!cancelled) toast.error(`加载运行明细失败：${(e as Error).message}`);
      })
      .finally(() => {
        if (!cancelled) setSelectedLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedKey]);

  // 用例清单：与历史记录相互独立，刷新时一起拉。
  React.useEffect(() => {
    let cancelled = false;
    setCasesLoading(true);
    benchapi
      .cases()
      .then((data) => {
        if (!cancelled) setCasesInfo(data);
      })
      .catch((e: unknown) => {
        if (!cancelled) toast.error(`加载用例清单失败：${(e as Error).message}`);
      })
      .finally(() => {
        if (!cancelled) setCasesLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const latest = runs?.[0] ?? null;
  const latestPp = latest ? deltaPpOf(latest) : 0;

  async function copyCommand() {
    const setName = latest?.set_name ?? "<set_name>";
    const profile = latest?.model_profile ?? "<id>";
    const cmd = `python scripts/bench/run.py --set ${setName} --profile ${profile}`;
    try {
      await navigator.clipboard.writeText(cmd);
      toast.success("跑批命令已复制");
    } catch (e) {
      toast.error(`复制失败：${(e as Error).message}`);
    }
  }

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="font-semibold text-lg">基准跑分</h1>
        <p className="text-muted-foreground text-sm">同 set + 同模型才可比；每改一条机制都应该跑一次并贴对比。</p>
      </div>

      {/* 区域 1 · 最近一次运行 */}
      {runs === null && runsLoading ? (
        <Card>
          <CardContent className="p-4">
            <Skeleton className="h-24 w-full" />
          </CardContent>
        </Card>
      ) : null}

      {latest ? (
        <Card>
          <CardHeader>
            <CardTitle>最近一次运行</CardTitle>
            <CardDescription>
              {latest.set_name} / {latest.model_profile} · {fmtTime(latest.created_at)}
            </CardDescription>
            <CardAction className="flex flex-wrap gap-2">
              <Button size="sm" variant="outline" disabled={runsLoading} onClick={() => void loadRuns()}>
                <RefreshCwIcon className={runsLoading ? "animate-spin" : undefined} />
                刷新
              </Button>
              <Button size="sm" variant="outline" onClick={() => void copyCommand()}>
                <CopyIcon /> 复制跑批命令
              </Button>
            </CardAction>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <div className="flex flex-wrap items-end gap-x-8 gap-y-2">
              <div className="flex items-end gap-3">
                <span className="font-semibold text-4xl tabular-nums">{fmtPct(latest.pass_rate)}</span>
                <span className="pb-1 text-muted-foreground text-sm tabular-nums">
                  {latest.passed}/{latest.total} 通过
                </span>
              </div>
              <div className="pb-1 text-sm">
                {latest.prev_run_key ? (
                  <span className={cn("tabular-nums", deltaTone(latestPp))}>
                    {fmtDelta(latestPp)}
                    <span className="ml-2 text-muted-foreground text-xs">vs {latest.prev_run_key}</span>
                  </span>
                ) : (
                  <span className="text-muted-foreground">首次</span>
                )}
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4 sm:grid-cols-3">
              <Stat label="平均耗时" value={fmtDur(latestSummary?.avg_duration_ms ?? 0)} hint="每题平均" />
              <Stat label="首个战果" value={fmtDur(latestSummary?.avg_first_finding_ms ?? 0)} hint="平均" />
              <Stat label="Token" value={fmtTok(latest.total_tokens)} hint="总计" />
            </div>
            <p className="font-mono text-muted-foreground text-xs">
              python scripts/bench/run.py --set {latest.set_name} --profile {latest.model_profile}
            </p>
          </CardContent>
        </Card>
      ) : null}

      {/* 区域 2 · 历史跑批表 */}
      <Card className="gap-0 py-0">
        <CardHeader className="px-4 py-3">
          <CardTitle>历史跑批</CardTitle>
          <CardDescription>点一行查看逐题明细；环比只跟最近一次同 set + 同模型比。</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          {runs === null && runsLoading ? (
            <div className="flex flex-col gap-2 px-4 pb-4">
              {SKELETON_ROWS.map((key) => (
                <Skeleton key={key} className="h-6 w-full" />
              ))}
            </div>
          ) : (runs?.length ?? 0) === 0 ? (
            <div className="px-4 pb-4">
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyTitle>还没有跑批记录</EmptyTitle>
                  <EmptyDescription>
                    先 <code className="font-mono">docker compose -f bench/compose.yml up -d</code> 起靶标， 然后跑{" "}
                    <code className="font-mono">python scripts/bench/run.py --set smoke</code>。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-40">时间</TableHead>
                  <TableHead>set</TableHead>
                  <TableHead>模型</TableHead>
                  <TableHead className="w-20">通过</TableHead>
                  <TableHead className="w-28">通过率</TableHead>
                  <TableHead className="w-24">环比</TableHead>
                  <TableHead className="w-24">平均耗时</TableHead>
                  <TableHead className="w-24">token</TableHead>
                  <TableHead className="w-20">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(runs ?? []).map((row) => {
                  const pp = deltaPpOf(row);
                  const active = selectedKey === row.run_key;
                  return (
                    <TableRow
                      key={row.run_key}
                      aria-selected={active}
                      className={cn("cursor-pointer", active && "bg-muted/50")}
                      onClick={() => setSelectedKey(row.run_key)}
                    >
                      <TableCell className="text-muted-foreground text-xs tabular-nums">
                        {fmtTime(row.created_at)}
                      </TableCell>
                      <TableCell className="text-xs">{row.set_name || "—"}</TableCell>
                      <TableCell className="max-w-[160px] truncate text-xs" title={row.model_profile}>
                        {row.model_profile || "—"}
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">
                        {row.passed}/{row.total}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <div className="h-1.5 w-[60px] overflow-hidden rounded-full bg-muted">
                            <div
                              className="h-full rounded-full bg-emerald-500"
                              style={{ width: `${Math.min(100, Math.max(0, row.pass_rate * 100))}%` }}
                            />
                          </div>
                          <span className="text-xs tabular-nums">{fmtPct(row.pass_rate)}</span>
                        </div>
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">
                        {row.delta_pass_rate === 0 && !row.prev_run_key ? (
                          <span className="text-muted-foreground">—</span>
                        ) : (
                          <span className={deltaTone(pp)}>{fmtDelta(pp)}</span>
                        )}
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">{fmtDur(row.duration_ms)}</TableCell>
                      <TableCell className="text-xs tabular-nums">{fmtTok(row.total_tokens)}</TableCell>
                      <TableCell>
                        <Button
                          size="xs"
                          variant={active ? "secondary" : "ghost"}
                          onClick={(event) => {
                            event.stopPropagation();
                            setSelectedKey(row.run_key);
                          }}
                        >
                          明细
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* 区域 3 · 选中运行的明细 */}
      <Card className="gap-0 py-0">
        <CardHeader className="px-4 py-3">
          <CardTitle>运行明细</CardTitle>
          <CardDescription>
            {selected ? (
              <span className="flex flex-wrap items-center gap-x-3 gap-y-1">
                <span className="font-mono text-xs" title={resultsDir}>
                  {resultsDir || "—"}
                </span>
                <span className="tabular-nums">共 {selected.cases.length} 题</span>
                <span>{selected.set_name}</span>
                <span className="max-w-[220px] truncate font-mono text-xs" title={selected.model_profile}>
                  {selected.model_profile}
                </span>
              </span>
            ) : (
              "点上面的历史行查看这次运行的逐题结果。"
            )}
          </CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          {selectedLoading && !selected ? (
            <div className="flex flex-col gap-2 px-4 pb-4">
              {SKELETON_ROWS.map((key) => (
                <Skeleton key={key} className="h-6 w-full" />
              ))}
            </div>
          ) : !selected ? (
            <div className="px-4 pb-4">
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyTitle>还没有选中的运行</EmptyTitle>
                  <EmptyDescription>
                    {runs && runs.length > 0
                      ? "点上面任意一行，这里会列出每道题的通过情况。"
                      : "还没有跑批记录，先跑一次 smoke 再回来看。"}
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-28">题号</TableHead>
                  <TableHead className="w-24">类别</TableHead>
                  <TableHead className="w-20">结果</TableHead>
                  <TableHead className="w-24">耗时</TableHead>
                  <TableHead className="w-24">首个战果</TableHead>
                  <TableHead className="w-24">token</TableHead>
                  <TableHead>任务</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {selected.cases.map((c) => {
                  const outcome = outcomeOf(c);
                  return (
                    <TableRow key={c.key}>
                      <TableCell className="font-medium font-mono text-xs">{c.key}</TableCell>
                      <TableCell className="text-xs">{c.category || "—"}</TableCell>
                      <TableCell>
                        <Badge variant="outline" className={OUTCOME_TONES[outcome] ?? ""}>
                          {OUTCOME_LABELS[outcome] ?? outcome}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs tabular-nums">{fmtDur(c.duration_ms)}</TableCell>
                      <TableCell className="text-xs tabular-nums">{fmtDur(c.first_finding_ms)}</TableCell>
                      <TableCell className="text-xs tabular-nums">{fmtTok(c.tokens)}</TableCell>
                      <TableCell className="max-w-[200px] truncate text-xs">
                        {c.task_id ? (
                          <Link
                            href={`/function/tasks?focus=${encodeURIComponent(c.task_id)}`}
                            className="text-primary underline-offset-4 hover:underline"
                            title={`查看任务 ${c.task_id}`}
                          >
                            {c.task_id}
                          </Link>
                        ) : (
                          "—"
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* 区域 4 · 用例清单（可折叠） */}
      <Collapsible open={casesOpen} onOpenChange={setCasesOpen}>
        <Card className="gap-0 py-0">
          <CardHeader className="flex flex-row items-center justify-between gap-3 px-4 py-3">
            <div className="min-w-0">
              <CardTitle>用例清单</CardTitle>
              <CardDescription className="truncate font-mono text-xs" title={casesInfo?.hint}>
                {casesInfo?.hint ?? "python scripts/bench/run.py --set smoke --profile <id>"}
              </CardDescription>
            </div>
            <CollapsibleTrigger asChild>
              <Button size="sm" variant="ghost">
                {casesOpen ? "收起" : `展开（${casesInfo?.count ?? 0}）`}
                <ChevronDownIcon className={cn("transition-transform", casesOpen && "rotate-180")} />
              </Button>
            </CollapsibleTrigger>
          </CardHeader>
          <CollapsibleContent>
            <CardContent className="px-0">
              {casesLoading && !casesInfo ? (
                <div className="flex flex-col gap-2 px-4 pb-4">
                  {SKELETON_ROWS.map((key) => (
                    <Skeleton key={key} className="h-6 w-full" />
                  ))}
                </div>
              ) : (casesInfo?.cases.length ?? 0) === 0 ? (
                <div className="px-4 pb-4">
                  <Empty className="border border-dashed">
                    <EmptyHeader>
                      <EmptyTitle>没有读到用例</EmptyTitle>
                      <EmptyDescription>
                        {casesInfo?.detail ?? "检查 bench/cases 目录里是否有 yaml 用例。"}
                      </EmptyDescription>
                    </EmptyHeader>
                  </Empty>
                </div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-32">key</TableHead>
                      <TableHead>名称</TableHead>
                      <TableHead className="w-28">类别</TableHead>
                      <TableHead>URL</TableHead>
                      <TableHead className="w-20">超时</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {(casesInfo?.cases ?? []).map((c) => (
                      <TableRow key={c.key}>
                        <TableCell className="font-medium font-mono text-xs">{c.key}</TableCell>
                        <TableCell className="text-xs">{c.name || "—"}</TableCell>
                        <TableCell className="text-xs">{c.category || "—"}</TableCell>
                        <TableCell className="max-w-[320px] truncate font-mono text-xs" title={c.url}>
                          {c.url || "—"}
                        </TableCell>
                        <TableCell className="text-xs tabular-nums">{c.timeout_s}s</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </CollapsibleContent>
        </Card>
      </Collapsible>
    </div>
  );
}
