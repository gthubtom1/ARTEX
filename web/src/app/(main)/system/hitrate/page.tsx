"use client";

import * as React from "react";

import { CheckCircle2Icon, CircleSlashIcon, ClipboardCopyIcon, RefreshCwIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api, hitrateapi, type HitrateResponse } from "@/lib/api";
import type { Task } from "@/lib/types";
import { cn } from "@/lib/utils";

// 机制命中率（B-09 / E01 度量）：只读。
//
// 为什么要有这一页：`/api/system/hitrate` 一直存在，但只有一个接口——
// 想判断「哪个二开机制其实从没真用过」得手跑 SQL。这里把 16 张表按任务摊开，
// 0 的一眼能看见（说明代码写了、机制没被触发）。
//
// 纪律：页面只显示后端算出来的数字，不猜、不补；某个表查询失败时后端给 0，
// 所以「0」的语义是「这次任务没写入」，不是「这个机制不存在」。
const TABLE_CN: Record<string, string> = {
  capabilities: "能力卡",
  assets_owned: "资产台账",
  asset_checks: "资产复查",
  assumptions: "假设台账",
  routes: "路由",
  evidence_timeline: "证据时间线",
  finding_traffic_bindings: "漏洞↔流量绑定",
  finding_cases: "漏洞案例",
  creds_vault: "凭据库",
  recheck_state: "复查状态",
  task_intelligence: "任务情报",
  skill_usage: "技能使用",
  tool_usage: "工具使用",
  cando_annotations: "能力卡 AI 细化",
  review_runs: "审查官运行",
  review_notes: "审查官笔记",
};

const TOOL_CN: Record<string, string> = {
  bash: "Bash",
  webfetch: "WebFetch",
  nmap: "nmap",
  httpx: "httpx",
  nuclei: "nuclei",
  subfinder: "subfinder",
  naabu: "naabu",
  katana: "katana",
  sqlmap: "sqlmap",
  report_finding: "上报漏洞",
  read_file: "读文件",
  write_file: "写文件",
};

function toolLabel(name: string) {
  return TOOL_CN[name] ?? name;
}

export default function HitratePage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [taskId, setTaskId] = React.useState("");
  const [data, setData] = React.useState<HitrateResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);

  // 任务列表只在进页面时取一次；默认选当前活动任务，否则选最新一条。
  React.useEffect(() => {
    let alive = true;
    api
      .tasks()
      .then((r) => {
        if (!alive) return;
        const list = r.tasks ?? [];
        setTasks(list);
        const pick = (r.active && list.some((t) => t.id === r.active) && r.active) || list[0]?.id || "";
        setTaskId((cur) => cur || pick);
      })
      .catch(() => alive && setErr("任务列表取不到"));
    return () => {
      alive = false;
    };
  }, []);

  const load = React.useCallback(
    async (id: string) => {
      if (!id) {
        setData(null);
        setLoaded(true);
        return;
      }
      setLoading(true);
      try {
        const r = await hitrateapi.get(id);
        setData(r);
        setErr("");
      } catch (e) {
        setErr(String(e));
        setData(null);
      } finally {
        setLoading(false);
        setLoaded(true);
      }
    },
    [],
  );

  React.useEffect(() => {
    if (taskId) void load(taskId);
  }, [taskId, load]);

  const tables = React.useMemo(() => Object.entries(data?.tables ?? {}), [data]);
  const tools = React.useMemo(
    () => Object.entries(data?.tools ?? {}).sort((a, b) => b[1] - a[1]),
    [data],
  );
  const needs = data?.needs ?? {};
  const filled = tables.filter(([, n]) => n > 0).length;
  const empty = tables.length - filled;
  const totalRows = tables.reduce((s, [, n]) => s + n, 0);
  const maxTool = tools.length > 0 ? Math.max(...tools.map(([, n]) => n)) : 0;

  function copyJSON() {
    if (!data) return;
    void navigator.clipboard.writeText(JSON.stringify({ task: taskId, ...data }, null, 2));
  }

  const current = tasks.find((t) => t.id === taskId);

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">机制命中率</h1>
          <p className="text-muted-foreground text-sm">
            按任务看 16 张二开表有没有真被写入。某一项是 0 = 对应机制这次没被触发（不是不存在）。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <NativeSelect
            className="w-[320px]"
            value={taskId}
            onChange={(e) => setTaskId(e.target.value)}
            aria-label="选择任务"
          >
            {tasks.length === 0 && <NativeSelectOption value="">（还没有任务）</NativeSelectOption>}
            {tasks.map((t) => (
              <NativeSelectOption key={t.id} value={t.id}>
                #{t.id} {t.name || t.goal || "未命名任务"}
              </NativeSelectOption>
            ))}
          </NativeSelect>
          <Button size="sm" variant="outline" onClick={() => void load(taskId)} disabled={!taskId || loading}>
            <RefreshCwIcon className={cn("size-3.5", loading && "animate-spin")} />
            刷新
          </Button>
          <Button size="sm" variant="outline" onClick={copyJSON} disabled={!data}>
            <ClipboardCopyIcon className="size-3.5" />
            复制 JSON
          </Button>
        </div>
      </div>

      {err && <div className="rounded-md border border-rose-500/40 bg-rose-500/5 px-3 py-2 text-[12px] text-rose-600">{err}</div>}

      {!loaded && (
        <div className="grid gap-2">
          {[0, 1, 2].map((i) => (
            <Skeleton key={i} className="h-10 w-full" />
          ))}
        </div>
      )}

      {loaded && tasks.length === 0 && (
        <Empty>
          <EmptyHeader>
            <EmptyTitle>还没有任务</EmptyTitle>
            <EmptyDescription>先建一个任务跑一轮，这里才有数字可看。</EmptyDescription>
          </EmptyHeader>
        </Empty>
      )}

      {loaded && tasks.length > 0 && (
        <>
          <div className="grid gap-3 sm:grid-cols-3">
            <Card>
              <CardHeader className="pb-1">
                <CardDescription>有数据的机制</CardDescription>
                <CardTitle className="text-2xl">
                  {filled}
                  <span className="text-muted-foreground text-sm font-normal"> / {tables.length}</span>
                </CardTitle>
              </CardHeader>
            </Card>
            <Card>
              <CardHeader className="pb-1">
                <CardDescription>空表</CardDescription>
                <CardTitle className={cn("text-2xl", empty > 0 && "text-amber-600")}>{empty}</CardTitle>
              </CardHeader>
            </Card>
            <Card>
              <CardHeader className="pb-1">
                <CardDescription>写入行数合计</CardDescription>
                <CardTitle className="text-2xl">{totalRows.toLocaleString("en-US")}</CardTitle>
              </CardHeader>
            </Card>
          </div>

          {current && (
            <div className="text-muted-foreground text-[11.5px]">
              当前任务：#{current.id} {current.name || current.goal}
            </div>
          )}

          <Card>
            <CardHeader>
              <CardTitle className="text-base">16 张表</CardTitle>
              <CardDescription>数字来自后端实时 count；查询失败也记 0，看板不隐藏空格。</CardDescription>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-[240px]">表</TableHead>
                    <TableHead className="w-[140px]">机制</TableHead>
                    <TableHead className="w-[90px] text-right">行数</TableHead>
                    <TableHead>判定</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tables.map(([name, n]) => (
                    <TableRow key={name}>
                      <TableCell className="font-mono text-[12px]">{name}</TableCell>
                      <TableCell className="text-[12px]">{TABLE_CN[name] ?? "—"}</TableCell>
                      <TableCell className={cn("text-right text-[12px] font-semibold", n === 0 && "text-muted-foreground/60")}>
                        {n}
                      </TableCell>
                      <TableCell>
                        {n > 0 ? (
                          <Badge variant="secondary" className="gap-1 bg-emerald-500/10 text-emerald-600">
                            <CheckCircle2Icon className="size-3" />
                            有数据
                          </Badge>
                        ) : (
                          <div className="flex flex-wrap items-center gap-1.5">
                            <Badge variant="secondary" className="gap-1 text-muted-foreground">
                              <CircleSlashIcon className="size-3" />
                              空表
                            </Badge>
                            {needs[name] && (
                              <span className="text-[10.5px] text-muted-foreground">{needs[name]}</span>
                            )}
                          </div>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="text-base">工具调用分布</CardTitle>
              <CardDescription>这个任务所属探索里的 tool_use 顶词统计（前 20）。</CardDescription>
            </CardHeader>
            <CardContent>
              {tools.length === 0 ? (
                <div className="text-muted-foreground py-6 text-center text-[12px]">这个任务还没有工具调用记录。</div>
              ) : (
                <div className="flex flex-col gap-1.5">
                  {tools.map(([name, n]) => (
                    <div key={name} className="flex items-center gap-2">
                      <span className="w-[130px] shrink-0 truncate text-[12px]" title={name}>
                        {toolLabel(name)}
                      </span>
                      <div className="bg-muted h-2 flex-1 overflow-hidden rounded">
                        <div
                          className="h-full rounded bg-sky-500/70"
                          style={{ width: `${maxTool > 0 ? Math.max(2, Math.round((n / maxTool) * 100)) : 0}%` }}
                        />
                      </div>
                      <span className="w-[52px] shrink-0 text-right text-[11.5px] tabular-nums">{n}</span>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
