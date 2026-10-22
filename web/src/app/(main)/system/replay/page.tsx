"use client";

import * as React from "react";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api, p2api } from "@/lib/api";
import type { ReplayCompareResponse, Task } from "@/lib/types";

// 回放看板（轻量版）：选两个任务 → 对比「意图/事实/发现/token/时长」。
// 用来回答"这次改动让任务变快还是变贵了"，不重跑 transcript。
export default function ReplayPage() {
  const [tasks, setTasks] = React.useState<Task[]>([]);
  const [a, setA] = React.useState("");
  const [b, setB] = React.useState("");
  const [data, setData] = React.useState<ReplayCompareResponse | null>(null);
  const [err, setErr] = React.useState("");

  React.useEffect(() => {
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
      .catch(() => setErr("读取任务列表失败"));
  }, []);

  const compare = React.useCallback(async () => {
    if (!a || !b) return;
    try {
      setErr("");
      setData(await p2api.replayCompare(a, b));
    } catch (e) {
      setErr(String(e));
    }
  }, [a, b]);

  React.useEffect(() => {
    void compare();
  }, [compare]);

  const row = (label: string, key: keyof ReplayCompareResponse["a"]) => (
    <tr className="border-b last:border-0">
      <td className="py-1 pr-3 text-muted-foreground">{label}</td>
      <td className="py-1 pr-3">{String(data?.a?.[key] ?? "—")}</td>
      <td className="py-1 pr-3">{String(data?.b?.[key] ?? "—")}</td>
    </tr>
  );

  return (
    <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-4">
      <div>
        <h1 className="text-lg font-semibold">回放看板</h1>
        <p className="text-sm text-muted-foreground">
          对比两个任务的成本与产出：意图/事实/发现/token/时长。用于验证改动是否真的有效。
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <select className="h-8 rounded-md border bg-background px-2 text-sm" value={a} onChange={(e) => setA(e.target.value)}>
          <option value="">基准任务 A…</option>
          {tasks.map((t) => (
            <option key={t.id} value={t.id}>
              A · {t.name || t.description || t.id}
            </option>
          ))}
        </select>
        <select className="h-8 rounded-md border bg-background px-2 text-sm" value={b} onChange={(e) => setB(e.target.value)}>
          <option value="">对比任务 B…</option>
          {tasks.map((t) => (
            <option key={t.id} value={t.id}>
              B · {t.name || t.description || t.id}
            </option>
          ))}
        </select>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      {data && (
        <>
          <div className="rounded-md border bg-muted/40 p-2 text-sm">{data.summary}</div>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">指标对比</CardTitle>
            </CardHeader>
            <CardContent>
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-3">指标</th>
                    <th className="py-1 pr-3">A（基准）</th>
                    <th className="py-1">B（对比）</th>
                  </tr>
                </thead>
                <tbody>
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
                </tbody>
              </table>
              <div className="mt-3 flex flex-wrap gap-2 text-xs">
                {Object.entries(data.delta ?? {}).map(([k, v]) => (
                  <span key={k} className="rounded border px-2 py-1">
                    {k}: {v > 0 ? "+" : ""}
                    {v}
                  </span>
                ))}
              </div>
            </CardContent>
          </Card>
        </>
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
  const [sel, setSel] = React.useState("");
  const [doc, setDoc] = React.useState<ReplaySession | null>(null);
  const [rerun, setRerun] = React.useState<ReplayRerun | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [msg, setMsg] = React.useState("");

  React.useEffect(() => {
    fetch("/api/replay/sessions", { headers: authHeaders() })
      .then((r) => (r.ok ? r.json() : { sessions: [] }))
      .then((d) => {
        setSessions(d.sessions ?? []);
        if (d.sessions?.length) setSel(d.sessions[0].id);
      })
      .catch(() => setMsg("读取会话列表失败"));
  }, []);

  const load = React.useCallback(async (id: string) => {
    if (!id) return;
    setMsg("");
    setRerun(null);
    try {
      const r = await fetch(`/api/replay/session?session=${encodeURIComponent(id)}`, { headers: authHeaders() });
      if (!r.ok) {
        setDoc(null);
        setMsg(`读取会话失败：HTTP ${r.status}`);
        return;
      }
      setDoc(await r.json());
    } catch (e) {
      setMsg(String(e));
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
        setMsg(d.error ?? `回放失败：HTTP ${r.status}`);
      } else {
        setRerun(d);
      }
    } catch (e) {
      setMsg(String(e));
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
          <select
            className="h-8 max-w-md rounded-md border bg-background px-2 text-sm"
            value={sel}
            onChange={(e) => setSel(e.target.value)}
          >
            <option value="">选择会话…</option>
            {sessions.map((s) => (
              <option key={s.id} value={s.id}>
                {s.id}（{s.mod_time}）
              </option>
            ))}
          </select>
          <Button size="sm" variant="outline" disabled={!sel} onClick={() => load(sel)}>
            读取
          </Button>
          <Button size="sm" disabled={!sel || busy} onClick={doRerun}>
            {busy ? "回放中…" : "只读重放 10 步"}
          </Button>
        </div>
        {msg && <div className="rounded-md border bg-muted/40 p-2 text-sm">{msg}</div>}

        {doc && (
          <div className="space-y-2">
            <div className="flex flex-wrap gap-2 text-xs">
              <span className="rounded border px-2 py-1">{doc.turns} 轮</span>
              <span className="rounded border px-2 py-1">
                token {doc.tokens_in}+{doc.tokens_out}
              </span>
              {(doc.tool_counts ?? []).slice(0, 8).map((t) => (
                <span key={t} className="rounded border px-2 py-1">
                  {t}
                </span>
              ))}
            </div>
            <div className="max-h-[320px] overflow-auto rounded border">
              <table className="w-full text-xs">
                <thead className="sticky top-0 bg-background text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">#</th>
                    <th className="py-1 pr-2">工具</th>
                    <th className="py-1 pr-2">只读</th>
                    <th className="py-1 pr-2">命令</th>
                    <th className="py-1">结果（截断）</th>
                  </tr>
                </thead>
                <tbody>
                  {doc.steps.map((st) => (
                    <tr key={st.index} className="border-b last:border-0">
                      <td className="py-1 pr-2">{st.index}</td>
                      <td className="py-1 pr-2">
                        {st.tool}
                        <span className="text-muted-foreground">/{st.kind}</span>
                      </td>
                      <td className="py-1 pr-2">{st.read_only ? "✅" : "—"}</td>
                      <td className="max-w-[320px] truncate py-1 pr-2" title={st.command}>
                        {st.command || "—"}
                      </td>
                      <td className="max-w-[420px] truncate py-1" title={st.result}>
                        {st.result || "—"}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        )}

        {rerun && (
          <div className="space-y-2">
            <div className="rounded-md border bg-muted/40 p-2 text-sm">{rerun.summary}</div>
            <div className="space-y-1">
              {rerun.comparisons.map((c) => (
                <div key={c.index} className="rounded border p-2 text-xs">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className={c.same ? "text-emerald-600" : "text-amber-600"}>
                      {c.same ? "一致" : "有变化"}
                    </span>
                    <span className="text-muted-foreground">重合 {(c.overlap * 100).toFixed(0)}%</span>
                    <span className="max-w-[520px] truncate" title={c.command}>
                      {c.command}
                    </span>
                  </div>
                  {(c.diff || c.err) && <div className="mt-1 text-muted-foreground">{c.diff || c.err}</div>}
                </div>
              ))}
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
