"use client";

import * as React from "react";

import { Flag, Target } from "lucide-react";

import { ExplorationGraph } from "@/components/exploration-graph";
import { api, candoapi, candoSend, type CandoNode, type CandoResponse } from "@/lib/api";
import type { Edge, TaskNode } from "@/lib/types";
import { cn } from "@/lib/utils";

// 「能做到什么」（CARD-B01）
//
// 关键设计：**复用探索链路那个画布组件**（同一套布局/交互/小地图/详情 Sheet），
// 只把节点语义换成语义标签：目标 / 入口 / 能做到 / 拿到了什么 / 建议。
// 之前自己手画一份 React Flow 是错的——那只能抄到外表，抄不到关系与布局。
const LABELS = {
  goal: "目标",
  intent: "入口",
  finding: "能做到",
  fact: "拿到了什么",
  hint: "建议",
  digest: "压缩",
};

type Extra = { prompt?: string; badge?: string; evidence?: { id: number; title: string }[] };

export function CandoTab({ taskId }: { taskId: string }) {
  const [graph, setGraph] = React.useState<{ nodes: TaskNode[]; edges: Edge[] }>({ nodes: [], edges: [] });
  const [cando, setCando] = React.useState<CandoResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [sel, setSel] = React.useState<TaskNode | null>(null);
  const [msg, setMsg] = React.useState("");

  const load = React.useCallback(() => {
    api
      .explorationGraph(taskId)
      .then((g) => setGraph({ nodes: g.nodes ?? [], edges: g.edges ?? [] }))
      .catch(() => setErr("链路数据取不到，30 秒后自动重试"));
    candoapi
      .get(taskId)
      .then(setCando)
      .catch(() => void 0);
  }, [taskId]);

  React.useEffect(() => {
    load();
    const t = setInterval(load, 15000);
    return () => clearInterval(t);
  }, [load]);

  // 把 cando 的车道数据挂到图节点上（按被引用的 finding id 对齐）
  const extras = React.useMemo(() => {
    const m: Record<string, Extra> = {};
    for (const lane of cando?.lanes ?? []) {
      for (const n of lane.nodes) {
        if (n.kind !== "can" && n.kind !== "data") continue;
        for (const id of n.cites ?? []) {
          m[String(id)] = {
            prompt: n.prompt,
            badge: n.badge,
            evidence: (n.evidence ?? []).map((e) => ({ id: e.id, title: e.title })),
          };
        }
      }
    }
    return m;
  }, [cando]);

  const suggestions = React.useMemo(() => {
    const seen = new Set<string>();
    const out: { text: string; from: string }[] = [];
    for (const lane of cando?.lanes ?? []) {
      for (const n of lane.nodes) {
        if (n.kind !== "next" || !n.body) continue;
        if (seen.has(n.body)) continue;
        seen.add(n.body);
        out.push({ text: n.body, from: n.title });
      }
    }
    return out;
  }, [cando]);

  const extra = sel ? extras[sel.id] : undefined;
  const risky = /改数据|修改|删除|写文件|上传|UPDATE|INSERT|DELETE|DROP/i.test(extra?.prompt ?? "");

  async function copyPrompt(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setMsg("已复制，粘到「会话」里发给它就行");
    } catch {
      setMsg("复制失败，请手动选中复制");
    }
  }

  async function sendPrompt(text: string) {
    if (risky) return;
    try {
      await candoSend(taskId, text, String(sel?.id ?? ""));
      setMsg("已作为提示发给它，规划者下一轮会读到");
    } catch {
      setMsg("发送失败（可能任务已终止）");
    }
  }

  const loot = cando?.loot ?? [];
  const banner = cando?.banner;

  return (
    <div className="flex min-h-0 flex-col gap-2">
      {/* 顶部：一行目标（可折叠）+ 现在就能拿 */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border bg-card px-3 py-1.5 text-[12px]">
        <Flag className="size-3.5 text-emerald-500" />
        <span
          className={cn(
            "rounded px-1.5 py-0.5 text-[10px] font-semibold",
            banner?.met ? "bg-emerald-500/15 text-emerald-600" : "bg-rose-500/15 text-rose-600",
          )}
        >
          {banner?.met ? "已达成" : "未达成"}
        </span>
        <span className="min-w-0 flex-1 truncate text-muted-foreground">{banner?.goal ?? "（未写目标）"}</span>
        <span className="shrink-0 text-[11px] text-muted-foreground">{banner?.summary}</span>
      </div>

      {loot.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-emerald-500/25 bg-emerald-500/5 px-3 py-1.5 text-[11.5px]">
          <span className="font-semibold text-emerald-700 dark:text-emerald-400">现在就能拿</span>
          {loot.map((l) => (
            <span
              key={l.node_key + l.label}
              className="rounded-md border border-emerald-500/30 bg-white px-2 py-0.5 font-medium text-emerald-800 dark:bg-neutral-900 dark:text-emerald-300"
            >
              {l.label} <span className="text-emerald-600">{l.count}</span>
            </span>
          ))}
        </div>
      )}

      <div className="flex min-h-0 flex-1 gap-2">
        {/* 画布：复用探索链路组件（同一套布局/交互/小地图/详情） */}
        <div className="h-[68vh] min-h-[520px] flex-1 overflow-hidden rounded-xl border">
          {graph.nodes.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              {err || "这个任务还没有可展示的链路"}
            </div>
          ) : (
            <ExplorationGraph
              nodes={graph.nodes}
              edges={graph.edges}
              labels={LABELS}
              onSelect={(n) => setSel(n)}
              className="h-[68vh]"
            />
          )}
        </div>

        {/* 右栏：建议 + 选中节点能做什么 */}
        <aside className="flex w-[340px] shrink-0 flex-col overflow-hidden rounded-xl border bg-card">
          <div className="border-b px-3 py-2 text-[12px] font-semibold">下一步建议</div>
          <div className="max-h-40 overflow-auto border-b px-3 py-2">
            {suggestions.length === 0 ? (
              <div className="text-[11.5px] text-muted-foreground">还没有建议（等能力卡给出下一步）</div>
            ) : (
              <ul className="space-y-1.5">
                {suggestions.slice(0, 4).map((s) => (
                  <li key={s.text} className="rounded-md border px-2 py-1.5 text-[11.5px] leading-snug">
                    <div className="line-clamp-2">{s.text}</div>
                    <div className="mt-1 flex gap-1.5">
                      <button className="rounded border px-1.5 py-0.5 text-[10.5px]" onClick={() => copyPrompt(s.text)}>
                        复制
                      </button>
                      <button
                        className="rounded border border-foreground/20 bg-foreground px-1.5 py-0.5 text-[10.5px] text-background disabled:opacity-40"
                        onClick={() => sendPrompt(s.text)}
                        disabled={risky}
                      >
                        发给它
                      </button>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>

          <div className="min-h-0 flex-1 overflow-auto px-3 py-2.5">
            {!sel ? (
              <div className="flex h-full flex-col items-center justify-center gap-1.5 text-center text-[12px] text-muted-foreground">
                <Target className="size-5 text-emerald-500" />
                点画布上任意一张卡片
                <br />
                看它是什么、依据哪几条漏洞
              </div>
            ) : (
              <div className="space-y-2">
                <div className="text-[10.5px] font-semibold text-muted-foreground">
                  {LABELS[sel.type as keyof typeof LABELS] ?? sel.type} #{sel.id}
                </div>
                <div className="text-[13px] font-semibold leading-snug">{sel.payload?.slice(0, 120) || "（无描述）"}</div>
                <div className="flex flex-wrap items-center gap-1.5 text-[10.5px]">
                  <span className="rounded bg-slate-500/10 px-1.5 py-0.5 text-slate-600">状态 {sel.state}</span>
                  {sel.priority > 0 && (
                    <span className="rounded bg-blue-500/10 px-1.5 py-0.5 text-blue-600">P{sel.priority}</span>
                  )}
                  {extra?.badge && (
                    <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-emerald-600">{extra.badge}</span>
                  )}
                </div>

                {extra?.evidence && extra.evidence.length > 0 && (
                  <div>
                    <div className="mb-1 text-[10.5px] font-semibold text-muted-foreground">依据（真实上报）</div>
                    <ul className="space-y-1">
                      {extra.evidence.map((e) => (
                        <li key={e.id} className="rounded border px-2 py-1 text-[11px] leading-snug">
                          <span className="text-muted-foreground">finding #{e.id}</span> {e.title}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

                {extra?.prompt && (
                  <div>
                    <div className="mb-1 text-[10.5px] font-semibold text-muted-foreground">可以直接发给它的指令</div>
                    <pre className="max-h-24 overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-2 text-[11px]">
                      {extra.prompt}
                    </pre>
                    <div className="mt-1.5 flex gap-2">
                      <button className="flex-1 rounded-md border px-2 py-1.5 text-[12px]" onClick={() => copyPrompt(extra.prompt!)}>
                        复制指令
                      </button>
                      <button
                        className="flex-1 rounded-md bg-foreground px-2 py-1.5 text-[12px] font-medium text-background disabled:opacity-40"
                        onClick={() => sendPrompt(extra.prompt!)}
                        disabled={risky}
                        title={risky ? "含不可逆动作，只能复制" : ""}
                      >
                        直接发给它
                      </button>
                    </div>
                    {risky && <div className="mt-1 text-[10.5px] text-rose-600">这条含不可逆动作，只能复制手动确认</div>}
                  </div>
                )}
              </div>
            )}
          </div>

          {msg && <div className="border-t bg-muted/40 px-3 py-1.5 text-[11px] text-muted-foreground">{msg}</div>}
        </aside>
      </div>
    </div>
  );
}
