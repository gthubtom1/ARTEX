"use client";

import * as React from "react";

import { Loader2Icon, PlayIcon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api, p2api } from "@/lib/api";
import type { ReplayCompareResponse, Task } from "@/lib/types";
import { cn } from "@/lib/utils";

// 回放看板（轻量版）：选两个任务 → 对比「意图/事实/发现/token/时长」。
// 用来回答"这次改动让任务变快还是变贵了"，不重跑 transcript。
export default function ReplayPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [tasksLoading, setTasksLoading] = React.useState(true);
  const [a, setA] = React.useState("");
  const [b, setB] = React.useState("");
  const [data, setData] = React.useState<ReplayCompareResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  React.useEffect(() => {
    setTasksLoading(true);
    api
      .tasks()
      .then((r) => {
        setTasks(r.tasks ?? []);
        const list = r.tasks ?? [];
        if (list.length >= 2) {
          setA(list[1].id);
          setB(list[0].id);
        } else if (list.length === 1) {
          setA(list[0].id);
          setB(list[0].id);
        }
      })
      .catch((e) => {
        const message = `读取任务列表失败：${String(e)}`;
        setErr(message);
        toast.error(message);
      })
      .finally(() => setTasksLoading(false));
  }, []);

  const compare = React.useCallback(
    async (notify = false) => {
      if (!a || !b) {
        setData(null);
        return;
      }
      setLoading(true);
      setData(null);
      try {
        setErr("");
        const r = await p2api.replayCompare(a, b);
        setData(r);
        if (notify) toast.success("对比完成");
      } catch (e) {
        const message = String(e);
        setErr(message);
        toast.error(message);
      } finally {
        setLoading(false);
      }
    },
    [a, b],
  );

  React.useEffect(() => {
    void compare();
  }, [compare]);

  const row = (label: string, key: keyof ReplayCompareResponse["a"]) => (
    <TableRow>
      <TableCell className="py-1 text-muted-foreground">{label}</TableCell>
      <TableCell className="py-1">{String(data?.a?.[key] ?? "—")}</TableCell>
      <TableCell className="py-1">{String(data?.b?.[key] ?? "—")}</TableCell>
    </TableRow>
  );

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">回放看板</h1>
        <p className="text-muted-foreground text-sm">
          对比两个任务的成本与产出：意图/事实/发现/token/时长。用于验证改动是否真的有效。
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        {tasksLoading ? (
          <Skeleton className="h-8 w-64" />
        ) : (
          <Select value={a || "none"} onValueChange={(v) => setA(v === "none" ? "" : v)}>
            <SelectTrigger className="w-full sm:w-72">
              <SelectValue placeholder="基准任务 A…" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">基准任务 A…</SelectItem>
              {tasks.map((t) => (
                <SelectItem key={t.id} value={t.id}>
                  A · {t.name || t.description || t.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        {tasksLoading ? (
          <Skeleton className="h-8 w-64" />
        ) : (
          <Select value={b || "none"} onValueChange={(v) => setB(v === "none" ? "" : v)}>
            <SelectTrigger className="w-full sm:w-72">
              <SelectValue placeholder="对比任务 B…" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="none">对比任务 B…</SelectItem>
              {tasks.map((t) => (
                <SelectItem key={t.id} value={t.id}>
                  B · {t.name || t.description || t.id}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <Button size="sm" variant="outline" disabled={!a || !b || loading} onClick={() => compare(true)}>
          <RefreshCwIcon className={cn("size-4", loading && "animate-spin")} />
          对比
        </Button>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      {tasksLoading || (loading && !data) ? (
        <div className="space-y-2">
          <Skeleton className="h-10 w-full" />
          <Skeleton className="h-56 w-full" />
        </div>
      ) : data ? (
        <>
          <div className="rounded-md border bg-muted/40 p-3 text-sm">{data.summary}</div>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">指标对比</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="max-h-[48vh] overflow-auto rounded-md border">
                <Table className="text-xs">
                  <TableHeader className="sticky top-0 z-10 bg-card">
                    <TableRow>
                      <TableHead className="h-8">指标</TableHead>
                      <TableHead className="h-8">A（基准）</TableHead>
                      <TableHead className="h-8">B（对比）</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {row("任务", "name")}
                    {row("状态", "status")}
                    {row("意图数", "intents")}
                    {row("事实数", "facts")}
                    {row("发现数", "findings")}
                    {row("hint 数", "hints")}
                    {row("输入 token", "input_tokens")}
                    {row("输出 token", "output_tokens")}
                    {row("时长（秒）", "duration_seconds")}
                    {row("发现/意图", "findings_per_intent")}
                    {row("事实/意图", "facts_per_intent")}
                    {row("token/发现", "tokens_per_finding")}
                  </TableBody>
                </Table>
              </div>
              <div className="mt-3 flex flex-wrap gap-2 text-xs">
                {Object.entries(data.delta ?? {}).map(([k, v]) => (
                  <Badge key={k} variant="outline" className="font-mono font-normal">
                    {k}: {v > 0 ? "+" : ""}
                    {v}
                  </Badge>
                ))}
              </div>
            </CardContent>
          </Card>
        </>
      ) : (
        <Card>
          <CardContent>
            <Empty className="border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <PlayIcon />
                </EmptyMedia>
                <EmptyTitle>还没有可对比的结果</EmptyTitle>
                <EmptyDescription>至少选择一个基准任务 A 和一个对比任务 B，然后点击「对比」。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </CardContent>
        </Card>
      )}

      <TranscriptReplay />
    </div>
  );
}

// ── 真回放：从 transcript 还原步骤，只读重放并对比 ──
type ReplayStep = {
  index: number;
  tool: string;
  kind: string;
  command: string;
  result: string;
  is_error: boolean;
  read_only: boolean;
};
type ReplaySession = {
  session: string;
  turns: number;
  tokens_in: number;
  tokens_out: number;
  steps: ReplayStep[];
  tool_counts: string[];
};
type ReplayComparison = {
  index: number;
  tool: string;
  command: string;
  same: boolean;
  overlap: number;
  diff?: string;
  err?: string;
};
type ReplayRerun = { summary: string; comparisons: ReplayComparison[] };

function authHeaders(): Record<string, string> {
  const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

function TranscriptReplay() {
  const [sessions, setSessions] = React.useState<{ id: string; mod_time: string; size: number }[]>([]);
  const [sessionsLoading, setSessionsLoading] = React.useState(true);
  const [sel, setSel] = React.useState("");
  const [doc, setDoc] = React.useState<ReplaySession | null>(null);
  const [docLoading, setDocLoading] = React.useState(false);
  const [rerun, setRerun] = React.useState<ReplayRerun | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [msg, setMsg] = React.useState("");

  React.useEffect(() => {
    setSessionsLoading(true);
    fetch("/api/replay/sessions", { headers: authHeaders() })
      .then((r) => (r.ok ? r.json() : { sessions: [] }))
      .then((d) => {
        setSessions(d.sessions ?? []);
        if (d.sessions?.length) setSel(d.sessions[0].id);
      })
      .catch((e) => {
        const message = `读取会话列表失败：${String(e)}`;
        setMsg(message);
        toast.error(message);
      })
      .finally(() => setSessionsLoading(false));
  }, []);

  const load = React.useCallback(async (id: string, notify = false) => {
    if (!id) return;
    setMsg("");
    setRerun(null);
    setDocLoading(true);
    try {
      const r = await fetch(`/api/replay/session?session=${encodeURIComponent(id)}`, { headers: authHeaders() });
      if (!r.ok) {
        setDoc(null);
        const message = `读取会话失败：HTTP ${r.status}`;
        setMsg(message);
        toast.error(message);
        return;
      }
      const d = await r.json();
      setDoc(d);
      if (notify) toast.success("已读取会话");
    } catch (e) {
      const message = String(e);
      setMsg(message);
      toast.error(message);
    } finally {
      setDocLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load(sel);
  }, [sel, load]);

  async function doRerun() {
    if (!sel) return;
    setBusy(true);
    setMsg("");
    try {
      const r = await fetch(`/api/replay/rerun?session=${encodeURIComponent(sel)}&limit=10`, {
        method: "POST",
        headers: authHeaders(),
      });
      const d = await r.json();
      if (!r.ok) {
        const message = d.error ?? `回放失败：HTTP ${r.status}`;
        setMsg(message);
        toast.error(message);
      } else {
        setRerun(d);
        toast.success(d.summary || "只读重放完成");
      }
    } catch (e) {
      const message = String(e);
      setMsg(message);
      toast.error(message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-sm">真回放（transcript 只读重放）</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="text-xs text-muted-foreground">
          从会话日志还原每一步工具调用；只重放白名单内的只读命令（curl GET / nmap -sV / nc -z / dig…），写操作自动跳过。
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {sessionsLoading ? (
            <Skeleton className="h-8 w-80" />
          ) : (
            <Select value={sel || "none"} onValueChange={(v) => setSel(v === "none" ? "" : v)}>
              <SelectTrigger className="w-full sm:w-80">
                <SelectValue placeholder="选择会话…" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="none">选择会话…</SelectItem>
                {sessions.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    {s.id}（{s.mod_time}）
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
          <Button
            size="sm"
            variant="outline"
            disabled={!sel || docLoading}
            onClick={() => load(sel, true)}
          >
            <RefreshCwIcon className={cn("size-4", docLoading && "animate-spin")} />
            读取
          </Button>
          <Button size="sm" disabled={!sel || busy} onClick={doRerun}>
            {busy ? <Loader2Icon className="size-4 animate-spin" /> : <PlayIcon className="size-4" />}
            {busy ? "回放中…" : "只读重放 10 步"}
          </Button>
        </div>
        {msg && <div className="rounded-md border bg-muted/40 p-2 text-sm">{msg}</div>}

        {docLoading && !doc ? (
          <Skeleton className="h-48 w-full" />
        ) : doc ? (
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2 text-xs">
              <Badge variant="outline" className="font-normal">
                {doc.turns} 轮
              </Badge>
              <Badge variant="outline" className="font-normal">
                token {doc.tokens_in}+{doc.tokens_out}
              </Badge>
              {(doc.tool_counts ?? []).slice(0, 8).map((t) => (
                <Badge key={t} variant="outline" className="font-normal">
                  {t}
                </Badge>
              ))}
            </div>
            <div className="max-h-[360px] overflow-auto rounded-md border">
              <Table className="text-xs">
                <TableHeader className="sticky top-0 z-10 bg-card">
                  <TableRow>
                    <TableHead className="h-8">#</TableHead>
                    <TableHead className="h-8">工具</TableHead>
                    <TableHead className="h-8">只读</TableHead>
                    <TableHead className="h-8">命令</TableHead>
                    <TableHead className="h-8">结果（截断）</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {doc.steps.map((st) => (
                    <TableRow key={st.index}>
                      <TableCell className="py-1">{st.index}</TableCell>
                      <TableCell className="py-1">
                        {st.tool}
                        <span className="text-muted-foreground">/{st.kind}</span>
                      </TableCell>
                      <TableCell className="py-1">{st.read_only ? "✅" : "—"}</TableCell>
                      <TableCell className="max-w-[320px] truncate py-1" title={st.command}>
                        {st.command || "—"}
                      </TableCell>
                      <TableCell className="max-w-[420px] truncate py-1" title={st.result}>
                        {st.result || "—"}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          </div>
        ) : null}

        {rerun && (
          <div className="space-y-2">
            <div className="rounded-md border bg-muted/40 p-2 text-sm">{rerun.summary}</div>
            {(rerun.comparisons?.length ?? 0) === 0 ? (
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyTitle>没有可重放的步骤</EmptyTitle>
                  <EmptyDescription>该会话里没有命中只读白名单的工具调用。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className="max-h-[360px] overflow-auto rounded-md border">
                <Table className="text-xs">
                  <TableHeader className="sticky top-0 z-10 bg-card">
                    <TableRow>
                      <TableHead className="h-8">#</TableHead>
                      <TableHead className="h-8">状态</TableHead>
                      <TableHead className="h-8">重合</TableHead>
                      <TableHead className="h-8">命令</TableHead>
                      <TableHead className="h-8">差异</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rerun.comparisons.map((c) => (
                      <TableRow key={c.index}>
                        <TableCell className="py-1">{c.index}</TableCell>
                        <TableCell className="py-1">
                          <span className={c.same ? "text-emerald-600" : "text-amber-600"}>
                            {c.same ? "一致" : "有变化"}
                          </span>
                        </TableCell>
                        <TableCell className="py-1 tabular-nums">{(c.overlap * 100).toFixed(0)}%</TableCell>
                        <TableCell className="max-w-[320px] truncate py-1" title={c.command}>
                          {c.command || "—"}
                        </TableCell>
                        <TableCell className="max-w-[420px] truncate py-1" title={c.diff || c.err || ""}>
                          {c.diff || c.err || "—"}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
}
