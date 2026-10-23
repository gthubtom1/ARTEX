"use client";

import * as React from "react";

import {
  Background,
  BackgroundVariant,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Panel,
  Position,
  ReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  Bug,
  Compass,
  Database,
  Flag,
  Layers,
  Lightbulb,
  type LucideIcon,
} from "lucide-react";

import { candoapi, candoSend, type CandoNode, type CandoResponse } from "@/lib/api";
import { cn } from "@/lib/utils";

// 「能做到什么」（CARD-B01）
// 规格：docs/08-详设-B01-能做到什么.md
// 视觉参照：探索链路（同一套节点/连线/小地图/图例语言）

type Kind = CandoNode["kind"];

type KindMeta = {
  label: string;
  icon: LucideIcon;
  iconBg: string;
  hex: string;
};

// 与探索链路同一套色：目标绿 / 意图蓝 / 事实琥珀 / 漏洞玫红 / 提示紫 / 压缩青 / 起点灰
const KIND_META: Record<Kind, KindMeta> = {
  entry: { label: "入口", icon: Compass, iconBg: "bg-blue-500", hex: "#3b82f6" },
  can: { label: "能做到", icon: Lightbulb, iconBg: "bg-violet-500", hex: "#8b5cf6" },
  data: { label: "拿到了什么", icon: Database, iconBg: "bg-amber-500", hex: "#f59e0b" },
  next: { label: "下一步", icon: Flag, iconBg: "bg-emerald-500", hex: "#10b981" },
  blocked: { label: "卡住了", icon: Bug, iconBg: "bg-rose-500", hex: "#f43f5e" },
  excluded: { label: "已排除", icon: Layers, iconBg: "bg-slate-500", hex: "#64748b" },
};

const TONE_META: Record<string, { chip: string; dot: string }> = {
  success: { chip: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400", dot: "bg-emerald-500" },
  info: { chip: "bg-blue-500/15 text-blue-600 dark:text-blue-400", dot: "bg-blue-500" },
  warn: { chip: "bg-amber-500/15 text-amber-600 dark:text-amber-400", dot: "bg-amber-500" },
  danger: { chip: "bg-rose-500/15 text-rose-600 dark:text-rose-400", dot: "bg-rose-500" },
  muted: { chip: "bg-slate-500/10 text-slate-500", dot: "bg-slate-300" },
};

// 连线语义（与探索链路同一套写法：文字 + 颜色 + 箭头）
const REL: Record<string, { label: string; color: string }> = {
  entry: { label: "打通", color: "#3b82f6" },
  can: { label: "能做到", color: "#8b5cf6" },
  data: { label: "产出", color: "#f59e0b" },
  next: { label: "接着做", color: "#10b981" },
  blocked: { label: "卡点", color: "#f43f5e" },
};

type CandoRFNode = Node<{ node: CandoNode }, "cando">;

function CandoCard({ data, selected }: NodeProps<CandoRFNode>) {
  const n = data.node;
  const meta = KIND_META[n.kind] ?? KIND_META.can;
  const tone = TONE_META[n.tone] ?? TONE_META.muted;
  const Icon = meta.icon;
  const live = !!n.live;
  const taken = n.kind === "can" && n.tone === "success";

  return (
    <div
      className={cn(
        "w-[240px] cursor-pointer rounded-2xl border-2 transition-colors",
        selected ? "border-blue-500" : live ? "border-blue-400/70" : "border-transparent",
      )}
    >
      <div className="group relative rounded-[14px] border border-black/[0.06] bg-white shadow-sm transition-shadow hover:shadow-lg dark:border-white/10 dark:bg-neutral-900">
        {taken && (
          <span className="absolute -top-2 left-2 z-10 rounded bg-emerald-500 px-1.5 py-0.5 text-[9.5px] font-bold text-white shadow-sm">
            已拿下
          </span>
        )}
        <Handle type="target" position={Position.Left} className="!size-2 !border-2 !border-neutral-300 !bg-white opacity-0" />
        <div className="flex items-center gap-2 px-3 pb-1.5 pt-3">
          <span className={cn("flex size-6 shrink-0 items-center justify-center rounded-lg shadow-sm", meta.iconBg)}>
            <Icon className="size-3.5 text-white" />
          </span>
          <span className="grow truncate text-[13px] font-semibold text-neutral-700 dark:text-neutral-100">{meta.label}</span>
          {live && (
            <span className="relative flex size-2 shrink-0">
              <span className="absolute inline-flex size-full animate-ping rounded-full bg-blue-400 opacity-70" />
              <span className="relative inline-flex size-2 rounded-full bg-blue-500" />
            </span>
          )}
          <span className={cn("shrink-0 rounded-md px-1.5 py-0.5 text-[10px] font-semibold", tone.chip)}>{n.badge}</span>
        </div>

        <div className="px-3 pb-1 text-[12.5px] font-medium leading-snug text-neutral-700 dark:text-neutral-200">
          <span className="line-clamp-2">{n.title}</span>
        </div>
        <div className="line-clamp-2 px-3 pb-2 text-[11.5px] leading-snug text-neutral-500 dark:text-neutral-400">{n.body}</div>

        {n.items && n.items.length > 0 && (
          <div className="flex flex-wrap gap-1 px-3 pb-2">
            {n.items.slice(0, 3).map((it) => (
              <span
                key={it.name}
                className={cn(
                  "rounded border border-black/5 bg-muted px-1.5 py-0.5 text-[10px] text-neutral-600 dark:text-neutral-300",
                  it.hot ? "border-rose-500/15 bg-rose-500/10 text-rose-600 dark:text-rose-300" : "",
                )}
              >
                {it.name}
              </span>
            ))}
            {n.items.length > 3 && <span className="text-[10px] text-neutral-400">+{n.items.length - 3}</span>}
          </div>
        )}

        <div className="flex items-center justify-between gap-2 px-3 pb-2.5">
          <span className="flex min-w-0 items-center gap-1.5 text-[10px] text-neutral-400 dark:text-neutral-500">
            <span className={cn("size-1.5 rounded-full", tone.dot)} />
            {live ? <span>正在攻</span> : null}
          </span>
          {(n.evidence?.length ?? 0) > 0 && (
            <span className="shrink-0 font-mono text-[10px] text-neutral-400">证据 {n.evidence!.length}</span>
          )}
        </div>
        <Handle type="source" position={Position.Right} className="!size-2 !border-2 !border-neutral-300 !bg-white opacity-0" />
      </div>
    </div>
  );
}

const nodeTypes = { cando: CandoCard };

const COL: Record<Kind, number> = { entry: 0, can: 300, data: 600, next: 900, blocked: 900, excluded: 900 };
const ROW = 210;

function buildFlow(data: CandoResponse): { nodes: CandoRFNode[]; edges: Edge[] } {
  const nodes: CandoRFNode[] = [];
  const edges: Edge[] = [];
  data.lanes.forEach((lane, li) => {
    const used: Record<number, number> = {};
    let prev = "";
    lane.nodes.forEach((n, ni) => {
      const col = COL[n.kind] ?? 300;
      const slot = used[col] ?? 0;
      used[col] = slot + 1;
      const id = `${lane.key}::${n.id}::${ni}`;
      nodes.push({ id, type: "cando", position: { x: col, y: li * ROW + slot * 120 }, data: { node: n } });
      if (prev) {
        const rel = REL[n.kind] ?? REL.can;
        const animated = !!n.live;
        edges.push({
          id: `${prev}->${id}`,
          source: prev,
          target: id,
          label: rel.label,
          type: "default",
          animated,
          labelShowBg: false,
          labelStyle: { fontSize: 10, fontWeight: 600, fill: rel.color },
          style: { stroke: rel.color, strokeWidth: 2, strokeOpacity: animated ? 0.9 : 0.55 },
          markerEnd: { type: MarkerType.ArrowClosed, color: rel.color, width: 14, height: 14 },
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
  const [msg, setMsg] = React.useState("");
  const [bannerOpen, setBannerOpen] = React.useState(true);

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
      setMsg("已复制，粘到「会话」里发给它就行");
    } catch {
      setMsg("复制失败，请手动选中复制");
    }
  }

  async function sendPrompt() {
    if (!prompt || risky) return;
    try {
      await candoSend(taskId, prompt, sel?.id ?? "");
      setMsg("已作为提示发给它，规划者下一轮会读到");
    } catch {
      setMsg("发送失败（可能任务已终止）");
    }
  }

  async function annotate() {
    if (!sel) return;
    setBusy(true);
    try {
      const r = await candoapi.annotate(taskId, sel.id);
      setAnn(r.annotation ? { ...r.annotation, cached: r.cached } : { status: "pending", body: "" });
      setMsg(r.cached ? "用了上次的分析（没花钱）" : "分析中…可以离开这一页，回来会更新");
    } catch {
      setMsg("提交失败（可能已到次数上限或前面还有一个在跑）");
    } finally {
      setBusy(false);
    }
  }

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
    return (
      <div className="space-y-3">
        <div className="h-9 animate-pulse rounded-lg bg-muted" />
        <div className="h-[68vh] animate-pulse rounded-xl bg-muted" />
      </div>
    );
  }
  if (error && !data) {
    return (
      <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed py-16 text-sm text-muted-foreground">
        {error}
        <button className="underline" onClick={load}>
          重试
        </button>
      </div>
    );
  }
  if (!data) return null;

  const empty = data.lanes.length === 0;
  const loot = data.loot ?? [];

  return (
    <div className="flex min-h-0 flex-col gap-2">
      {/* 顶部：一条可折叠的小条（不占大块空间） */}
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg border bg-card px-3 py-1.5 text-[12px]">
        <button
          className="flex items-center gap-1 font-medium text-muted-foreground hover:text-foreground"
          onClick={() => setBannerOpen((o) => !o)}
          title={bannerOpen ? "收起" : "展开"}
        >
          <Flag className="size-3.5 text-emerald-500" />
          {bannerOpen ? "收起" : "目标"}
        </button>
        <span
          className={cn(
            "rounded px-1.5 py-0.5 text-[10px] font-semibold",
            data.banner.met ? "bg-emerald-500/15 text-emerald-600" : "bg-rose-500/15 text-rose-600",
          )}
        >
          {data.banner.met ? "已达成" : "未达成"}
        </span>
        {bannerOpen && <span className="min-w-0 flex-1 truncate text-muted-foreground">{data.banner.goal}</span>}
        <span className="ml-auto shrink-0 text-[11px] text-muted-foreground">{data.banner.summary}</span>
        {data.banner.budget_left && <span className="shrink-0 text-[11px] text-muted-foreground">· 还剩 {data.banner.budget_left}</span>}
      </div>

      {/* 现在就能拿（紧凑） */}
      {loot.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-emerald-500/25 bg-emerald-500/5 px-3 py-1.5 text-[11.5px]">
          <span className="font-semibold text-emerald-700 dark:text-emerald-400">现在就能拿</span>
          {loot.map((l) => (
            <button
              key={l.node_key + l.label}
              className="rounded-md border border-emerald-500/30 bg-white px-2 py-0.5 font-medium text-emerald-800 hover:bg-emerald-50 dark:bg-neutral-900 dark:text-emerald-300"
              onClick={() => {
                const hit = data.lanes.flatMap((ln) => ln.nodes).find((n) => n.id === l.node_key);
                if (hit) setSel(hit);
              }}
            >
              {l.label} <span className="text-emerald-600">{l.count}</span>
            </button>
          ))}
        </div>
      )}

      <div className="flex min-h-0 flex-1 gap-2">
        {/* 画布：必须给确定高度，否则 React Flow 渲染成 0 高（之前"空外表"的根因） */}
        <div className="relative h-[68vh] min-h-[520px] flex-1 overflow-hidden rounded-xl border">
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
              fitViewOptions={{ padding: 0.2 }}
              proOptions={{ hideAttribution: true }}
              onlyRenderVisibleElements
              minZoom={0.15}
              maxZoom={1.6}
              onNodeClick={(_, n) => {
                setSel((n as CandoRFNode).data.node);
                setAnn(null);
                setMsg("");
              }}
              className="!bg-[#f0f2f7] dark:!bg-neutral-950"
            >
              <Background variant={BackgroundVariant.Dots} gap={16} size={1} className="text-neutral-400/50 dark:text-neutral-700/60" />
              <Controls
                showInteractive={false}
                className="!rounded-lg !border !shadow-sm [&>button]:!border-border [&>button]:!bg-card [&>button:hover]:!bg-accent [&_svg]:!fill-foreground"
              />
              <MiniMap
                pannable
                zoomable
                className="!rounded-lg !border !shadow-sm !bg-card"
                maskColor="rgb(148 163 184 / 0.18)"
                nodeColor={(node) => {
                  const nd = (node.data as { node?: CandoNode } | undefined)?.node;
                  return nd ? (KIND_META[nd.kind]?.hex ?? "#94a3b8") : "#94a3b8";
                }}
                nodeStrokeWidth={0}
                nodeBorderRadius={4}
              />
              <Panel position="top-left">
                <div className="flex flex-col gap-2 rounded-lg border bg-card/95 p-2.5 text-[11px] shadow-sm backdrop-blur">
                  <div className="flex flex-wrap gap-x-3 gap-y-1">
                    {(Object.keys(KIND_META) as Kind[]).map((k) => {
                      const m = KIND_META[k];
                      const I = m.icon;
                      return (
                        <span key={k} className="inline-flex items-center gap-1.5 text-foreground">
                          <span className={cn("flex size-4 items-center justify-center rounded", m.iconBg)}>
                            <I className="size-2.5 text-white" />
                          </span>
                          {m.label}
                        </span>
                      );
                    })}
                  </div>
                  <div className="flex flex-wrap gap-x-3 gap-y-1 border-t pt-1.5 text-muted-foreground">
                    {Object.values(REL).map((r) => (
                      <span key={r.label} className="inline-flex items-center gap-1.5">
                        <span className="h-0.5 w-4 rounded-full" style={{ backgroundColor: r.color }} />
                        {r.label}
                      </span>
                    ))}
                  </div>
                </div>
              </Panel>
            </ReactFlow>
          )}
        </div>

        {/* 右侧详情 */}
        <aside className="flex w-[360px] shrink-0 flex-col overflow-hidden rounded-xl border bg-card">
          {!sel ? (
            <div className="p-6 text-center text-[12px] leading-relaxed text-muted-foreground">
              点左边任意一张卡片
              <br />
              看它能做什么、拿到什么、下一步怎么走
            </div>
          ) : (
            <>
              <div className="border-b px-3 py-2.5">
                <div className="flex items-center gap-2">
                  <span className={cn("flex size-5 items-center justify-center rounded", KIND_META[sel.kind]?.iconBg)}>
                    {React.createElement(KIND_META[sel.kind]?.icon ?? Compass, { className: "size-3 text-white" })}
                  </span>
                  <span className="text-[11px] font-semibold text-muted-foreground">{KIND_META[sel.kind]?.label}</span>
                  <span
                    className={cn(
                      "ml-auto rounded px-1.5 py-0.5 text-[10px] font-semibold",
                      (TONE_META[sel.tone] ?? TONE_META.muted).chip,
                    )}
                  >
                    {sel.badge}
                  </span>
                </div>
                <div className="mt-1 text-[13px] font-semibold leading-snug">{sel.title}</div>
                <div className="mt-0.5 text-[11.5px] leading-snug text-muted-foreground">{sel.body}</div>
              </div>

              <div className="min-h-0 flex-1 overflow-auto px-3 py-2.5">
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
                            <td className="whitespace-nowrap py-1.5 pr-1 text-muted-foreground">{it.count}</td>
                            <td className="py-1.5 text-muted-foreground">{it.note}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </>
                )}

                {sel.evidence && sel.evidence.length > 0 && (
                  <>
                    <div className="mb-1 mt-3 text-[10.5px] font-semibold text-muted-foreground">依据（真实上报的漏洞）</div>
                    <ul className="space-y-1">
                      {sel.evidence.map((e) => (
                        <li key={`${e.kind}-${e.id}`} className="rounded border px-2 py-1 text-[11px] leading-snug">
                          <span className="text-muted-foreground">
                            {e.kind} #{e.id}
                          </span>{" "}
                          {e.title}
                        </li>
                      ))}
                    </ul>
                  </>
                )}

                {sel.options && sel.options.length > 0 && (
                  <>
                    <div className="mb-1 mt-3 text-[10.5px] font-semibold text-muted-foreground">接下来你可以怎么走（自己选）</div>
                    <div className="space-y-1">
                      {sel.options.map((o) => (
                        <div
                          key={o.text}
                          className={cn(
                            "flex items-start gap-2 rounded-md border px-2 py-1.5 text-[11.5px]",
                            o.rec ? "border-emerald-500/30 bg-emerald-500/5" : "",
                          )}
                        >
                          <span className="mt-0.5">{o.rec ? "★" : "·"}</span>
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
                    <div className="mb-1 font-semibold text-violet-700 dark:text-violet-300">
                      AI 分析 {ann.status === "pending" ? "（进行中…）" : ann.finished_at ? `· ${ann.finished_at}` : ""}
                      {ann.cached ? " · 复用上次结果" : ""}
                    </div>
                    <div className="whitespace-pre-wrap text-foreground/80">{ann.body || "…"}</div>
                  </div>
                )}
              </div>

              <div className="border-t px-3 py-2.5">
                <button
                  className="mb-2 w-full rounded-md border border-violet-500/30 bg-violet-500/5 px-2 py-1.5 text-[12px] font-medium text-violet-700 disabled:opacity-50 dark:text-violet-300"
                  onClick={annotate}
                  disabled={busy}
                >
                  {ann?.status === "done" ? "重新分析（会花一点额度）" : "让它详细分析"}
                </button>
                <div className="mb-1 text-[10.5px] font-semibold text-muted-foreground">可以直接发给它的指令</div>
                <pre className="max-h-20 overflow-auto whitespace-pre-wrap rounded-md border bg-muted/40 p-2 text-[11px]">
                  {prompt || "（这条没有可下发的指令）"}
                </pre>
                <div className="mt-2 flex gap-2">
                  <button className="flex-1 rounded-md border px-2 py-1.5 text-[12px] disabled:opacity-50" onClick={copyPrompt} disabled={!prompt}>
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
                {msg && <div className="mt-1 text-[10.5px] text-muted-foreground">{msg}</div>}
              </div>
            </>
          )}
        </aside>
      </div>
    </div>
  );
}
