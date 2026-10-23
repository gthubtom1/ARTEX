"use client";

import * as React from "react";

import {
  Background,
  Controls,
  MiniMap,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";

import { candoapi, candoSend, type CandoNode, type CandoResponse } from "@/lib/api";
import { cn } from "@/lib/utils";

// 「能做到什么」（CARD-B01）
// 规格：docs/08-详设-B01-能做到什么.md
// 只读现有表；唯一写操作是按需 AI 细化缓存与 hint 节点。

type Kind = CandoNode["kind"];

const KIND_META: Record<Kind, { label: string; hex: string }> = {
  entry: { label: "入口", hex: "#3b82f6" },
  can: { label: "能做到", hex: "#14b8a6" },
  data: { label: "拿到了什么", hex: "#f59e0b" },
  next: { label: "下一步", hex: "#10b981" },
  blocked: { label: "卡住了", hex: "#f43f5e" },
  excluded: { label: "已排除", hex: "#64748b" },
};

const STATE_META: Record<string, { label: string; cls: string }> = {
  attacking: { label: "正在攻", cls: "bg-blue-500/15 text-blue-600" },
  taken: { label: "已拿下", cls: "bg-emerald-500/15 text-emerald-600" },
  spinning: { label: "有空转", cls: "bg-amber-500/15 text-amber-700" },
  dead: { label: "封死", cls: "bg-rose-500/15 text-rose-600" },
  self_fault: { label: "我方故障", cls: "bg-slate-500/15 text-slate-600" },
  untried: { label: "未试", cls: "bg-slate-500/10 text-slate-500" },
};

type CandoFlowNode = Node<{ node: CandoNode }, "cando">;

function CandoCard({ data, selected }: NodeProps<CandoFlowNode>) {
  const n = data.node;
  const meta = KIND_META[n.kind] ?? KIND_META.can;
  const st = STATE_META[n.state] ?? STATE_META.untried;
  return (
    <div
      className={cn(
        "w-[236px] cursor-pointer rounded-2xl bg-white shadow-sm transition-shadow hover:shadow-lg dark:bg-neutral-900",
        selected ? "ring-2 ring-blue-500" : "",
      )}
      style={{ borderLeft: `4px solid ${meta.hex}` }}
    >
      <div
        className="flex items-center gap-2 rounded-t-2xl px-3 pb-1.5 pt-2.5"
        style={{ background: `linear-gradient(90deg, ${meta.hex}1a, transparent 75%)` }}
      >
        <span
          className="flex size-6 shrink-0 items-center justify-center rounded-lg text-[11px] font-bold text-white shadow-sm"
          style={{ background: meta.hex }}
        >
          {meta.label.slice(0, 1)}
        </span>
        <span className="text-[11px] font-semibold" style={{ color: meta.hex }}>
          {meta.label}
        </span>
        <span className={cn("ml-auto rounded-md px-1.5 py-0.5 text-[10px] font-semibold", st.cls)}>{st.label}</span>
      </div>
      <div className="px-3 pb-2">
        <div className="line-clamp-2 text-[12.5px] font-semibold leading-snug">{n.title}</div>
        <div className="mt-1 line-clamp-3 text-[11.5px] leading-snug text-muted-foreground">{n.body}</div>
        {n.items && n.items.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1">
            {n.items.slice(0, 4).map((it) => (
              <span
                key={it.name}
                className={cn(
                  "rounded border border-black/5 bg-muted px-1.5 py-0.5 text-[10px]",
                  it.hot ? "border-rose-500/15 bg-rose-500/10 text-rose-700" : "",
                )}
              >
                {it.name}
              </span>
            ))}
            {n.items.length > 4 && <span className="text-[10px] text-muted-foreground">+{n.items.length - 4}</span>}
          </div>
        )}
        <div className="mt-2 flex items-center gap-1.5 text-[10.5px] text-muted-foreground">
          {n.state === "attacking" && (
            <span className="relative flex size-2 shrink-0">
              <span className="absolute inline-flex size-full animate-ping rounded-full bg-blue-400 opacity-70" />
              <span className="relative inline-flex size-2 rounded-full bg-blue-500" />
            </span>
          )}
          <span>{n.chip}</span>
        </div>
      </div>
    </div>
  );
}

const nodeTypes = { cando: CandoCard };

// 每个 kind 固定列，保证左→右读；同 kind 多个时纵向堆叠。
const COL: Record<Kind, number> = { entry: 0, can: 300, data: 600, next: 900, blocked: 900, excluded: 900 };

function buildFlow(data: CandoResponse): { nodes: CandoFlowNode[]; edges: Edge[] } {
  const nodes: CandoFlowNode[] = [];
  const edges: Edge[] = [];
  data.lanes.forEach((lane, li) => {
    const used: Record<string, number> = {};
    let prev = "";
    lane.nodes.forEach((n, ni) => {
      const col = COL[n.kind] ?? 300;
      const slot = used[`${col}`] ?? 0;
      used[`${col}`] = slot + 1;
      const id = `${lane.key}::${n.id}::${ni}`;
      nodes.push({
        id,
        type: "cando",
        position: { x: col, y: li * 230 + slot * 130 },
        data: { node: n },
      });
      if (prev) {
        edges.push({
          id: `${prev}->${id}`,
          source: prev,
          target: id,
          style: { stroke: KIND_META[n.kind]?.hex ?? "#94a3b8", strokeWidth: 1.6 },
          animated: n.state === "attacking",
        });
      }
      prev = id;
    });
  });
  return { nodes, edges };
}

export function CandoTab({ taskId }: { taskId: string }) {
  const [data, setData] = React.useState<CandoResponse | null>(null);
  const [loading, setLoading] = React.useState(true);
  const [error, setError] = React.useState("");
  const [sel, setSel] = React.useState<CandoNode | null>(null);
  const [ann, setAnn] = React.useState<{ status: string; body: string; finished_at?: string; cached?: boolean } | null>(null);
  const [busy, setBusy] = React.useState(false);
  const [sendMsg, setSendMsg] = React.useState("");

  const load = React.useCallback(() => {
    candoapi
      .get(taskId)
      .then((d) => {
        setData(d);
        setError("");
      })
      .catch(() => setError("战果数据取不到，30 秒后自动重试"))
      .finally(() => setLoading(false));
  }, [taskId]);

  React.useEffect(() => {
    load();
    const t = setInterval(load, 15000);
    return () => clearInterval(t);
  }, [load]);

  const flow = React.useMemo(() => (data ? buildFlow(data) : { nodes: [], edges: [] }), [data]);

  const prompt = sel?.prompt ?? "";
  const risky = /改数据|修改|删除|写文件|上传|UPDATE|INSERT|DELETE|DROP/i.test(prompt);

  async function copyPrompt() {
    if (!prompt) return;
    try {
      await navigator.clipboard.writeText(prompt);
      setSendMsg("已复制，粘到「会话」里发给它就行");
    } catch {
      setSendMsg("复制失败，请手动选中复制");
    }
  }

  async function sendPrompt() {
    if (!prompt || risky) return;
    try {
      await candoSend(taskId, prompt, sel?.id ?? "");
      setSendMsg("已作为提示发给它，规划者下一轮会读到");
    } catch {
      setSendMsg("发送失败（可能是任务已终止）");
    }
  }

  async function annotate() {
    if (!sel) return;
    setBusy(true);
    try {
      const r = await candoapi.annotate(taskId, sel.id);
      setAnn(r.annotation ? { ...r.annotation, cached: r.cached } : { status: "pending", body: "" });
      setSendMsg(r.cached ? "用了上次的分析（没花钱）" : "分析中…可以离开这一页，回来会更新");
    } catch {
      setSendMsg("提交失败（可能已到次数上限或前面还有一个在跑）");
    } finally {
      setBusy(false);
    }
  }

  // 轮询 pending 的细化结果
  React.useEffect(() => {
    if (!sel || ann?.status !== "pending") return;
    const t = setInterval(() => {
      candoapi
        .annotateStatus(taskId, sel.id)
        .then((a) => setAnn(a))
        .catch(() => void 0);
    }, 5000);
    return () => clearInterval(t);
  }, [ann?.status, sel, taskId]);

  if (loading && !data) {
    return <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">加载中…</div>;
  }
  if (error && !data) {
    return (
      <div className="rounded-lg border border-dashed p-10 text-center text-sm text-muted-foreground">
        {error}
        <button className="ml-2 underline" onClick={load}>
          重试
        </button>
      </div>
    );
  }
  if (!data) return null;

  const empty = data.lanes.length === 0;

  return (
    <div className="flex min-h-[70vh] flex-col gap-3">
      {/* 顶栏 */}
      <div className="flex flex-wrap items-center gap-3 rounded-xl border bg-card px-4 py-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-[13px] font-semibold">
            <span className="text-muted-foreground">目标</span>
            <span className="truncate">{data.banner.goal}</span>
            <span
              className={cn(
                "rounded px-1.5 py-0.5 text-[10px] font-semibold",
                data.banner.met ? "bg-emerald-500/15 text-emerald-600" : "bg-rose-500/15 text-rose-600",
              )}
            >
              {data.banner.met ? "已达成" : "未达成"}
            </span>
          </div>
          <div className="mt-1 text-[11.5px] text-muted-foreground">{data.banner.summary}</div>
        </div>
        <div className="ml-auto flex items-center gap-2 text-[11.5px] text-muted-foreground">
          {data.banner.budget_left && <span>还剩 {data.banner.budget_left}</span>}
        </div>
      </div>

      {/* 现在就能拿 */}
      {data.loot.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-xl border border-emerald-500/25 bg-emerald-500/5 px-4 py-2 text-[11.5px]">
          <span className="font-semibold text-emerald-700">你现在就能拿：</span>
          {data.loot.map((l) => (
            <span key={l.node_key + l.label} className="rounded-md border border-emerald-500/30 bg-white px-2 py-0.5 font-medium text-emerald-800">
              {l.label} <span className="text-emerald-600">{l.count}</span>
            </span>
          ))}
        </div>
      )}

      {/* 画布 + 侧栏 */}
      <div className="flex min-h-0 flex-1 gap-3">
        <div className="relative min-h-[560px] flex-1 overflow-hidden rounded-xl border bg-[#f0f2f7] dark:bg-neutral-950">
          {empty ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              这个任务还没有可展示的战果（没有能力卡也没有漏洞）
            </div>
          ) : (
            <ReactFlow
              nodes={flow.nodes}
              edges={flow.edges}
              nodeTypes={nodeTypes}
              fitView
              minZoom={0.3}
              maxZoom={1.6}
              onNodeClick={(_, n) => {
                setSel((n as CandoFlowNode).data.node);
                setAnn(null);
                setSendMsg("");
              }}
            >
              <Background gap={16} size={1} color="#d4d4d8" />
              <Controls showInteractive={false} />
              <MiniMap pannable zoomable />
            </ReactFlow>
          )}
        </div>

        {/* 侧栏 */}
        <aside className="flex w-[380px] shrink-0 flex-col overflow-hidden rounded-xl border bg-card">
          {!sel ? (
            <div className="p-6 text-center text-[12.5px] text-muted-foreground">
              点左边任意一张卡片，看它能做什么、拿到什么、下一步怎么走
            </div>
          ) : (
            <>
              <div className="border-b px-4 py-3">
                <div className="text-[11px] font-semibold" style={{ color: KIND_META[sel.kind]?.hex }}>
                  {KIND_META[sel.kind]?.label}
                </div>
                <div className="mt-0.5 text-[13.5px] font-semibold">{sel.title}</div>
                <div className="mt-1 text-[11.5px] text-muted-foreground">{sel.body}</div>
              </div>

              <div className="min-h-0 flex-1 overflow-auto px-4 py-3">
                {sel.items && sel.items.length > 0 && (
                  <>
                    <div className="mb-1 text-[10.5px] font-semibold text-muted-foreground">现在就能拿的东西</div>
                    <table className="w-full text-[11.5px]">
                      <thead>
                        <tr className="text-left text-[10px] text-muted-foreground">
                          <th className="py-1">能拿什么</th>
                          <th className="py-1">有多少</th>
                          <th className="py-1">说明</th>
                        </tr>
                      </thead>
                      <tbody>
                        {sel.items.map((it) => (
                          <tr key={it.name} className="border-t">
                            <td className="py-1.5 pr-1">{it.name}</td>
                            <td className="py-1.5 pr-1 whitespace-nowrap text-muted-foreground">{it.count}</td>
                            <td className="py-1.5 text-muted-foreground">{it.note}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </>
                )}

                {sel.options && sel.options.length > 0 && (
                  <>
                    <div className="mb-1 mt-3 text-[10.5px] font-semibold text-muted-foreground">
                      接下来你可以怎么走（自己选）
                    </div>
                    <div className="space-y-1">
                      {sel.options.map((o) => (
                        <div
                          key={o.text}
                          className={cn(
                            "flex items-start gap-2 rounded-md border px-2 py-1.5 text-[11.5px]",
                            o.rec ? "border-emerald-500/30 bg-emerald-500/5" : "",
                          )}
                        >
                          <span className="mt-0.5">
                            {o.rec ? "★" : "·"}
                          </span>
                          <span className="flex-1">
                            {o.text}
                            <span className="ml-1 text-muted-foreground">
                              {o.cost} · 风险{o.risk === "lo" ? "低" : o.risk === "mid" ? "中" : "高"} · {o.gain}
                            </span>
                          </span>
                        </div>
                      ))}
                    </div>
                  </>
                )}

                {ann && (
                  <div className="mt-3 rounded-md border border-violet-500/30 bg-violet-500/5 p-2 text-[11.5px]">
                    <div className="mb-1 font-semibold text-violet-700">
                      AI 分析 {ann.status === "pending" ? "（进行中…）" : ann.finished_at ? `· ${ann.finished_at}` : ""}
                      {ann.cached ? " · 复用上次结果" : ""}
                    </div>
                    <div className="whitespace-pre-wrap text-foreground/80">{ann.body || "…"}</div>
                  </div>
                )}
              </div>

              <div className="border-t px-4 py-3">
                <button
                  className="mb-2 w-full rounded-md border border-violet-500/30 bg-violet-500/5 px-2 py-1.5 text-[12px] font-medium text-violet-700 disabled:opacity-50"
                  onClick={annotate}
                  disabled={busy}
                >
                  {ann?.status === "done" ? "重新分析（会花一点额度）" : "让它详细分析"}
                </button>
                <div className="mb-1 text-[10.5px] font-semibold text-muted-foreground">可以直接发给它的指令</div>
                <pre className="max-h-24 overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-2 text-[11px]">
                  {prompt || "（这条没有可下发的指令）"}
                </pre>
                <div className="mt-2 flex gap-2">
                  <button
                    className="flex-1 rounded-md border px-2 py-1.5 text-[12px] disabled:opacity-50"
                    onClick={copyPrompt}
                    disabled={!prompt}
                  >
                    复制指令
                  </button>
                  <button
                    className="flex-1 rounded-md bg-foreground px-2 py-1.5 text-[12px] font-medium text-background disabled:opacity-40"
                    onClick={sendPrompt}
                    disabled={!prompt || risky}
                    title={risky ? "含不可逆动作，只能复制" : ""}
                  >
                    直接发给它
                  </button>
                </div>
                {risky && <div className="mt-1 text-[10.5px] text-rose-600">这条含不可逆动作，只能复制手动确认</div>}
                {sendMsg && <div className="mt-1 text-[10.5px] text-muted-foreground">{sendMsg}</div>}
              </div>
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
