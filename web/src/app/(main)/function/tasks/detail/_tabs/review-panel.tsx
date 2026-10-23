"use client";

import * as React from "react";

import { ChevronRightIcon, Loader2Icon, RefreshCwIcon, SparklesIcon } from "lucide-react";

import { QuickPhrases } from "@/components/quick-phrases";
import { Button } from "@/components/ui/button";
import { api, http, type ReviewAction, type ReviewRun, type ReviewState, reviewapi } from "@/lib/api";
import { cn } from "@/lib/utils";

// 审查官（CARD-C01，原「复盘官」）
//
// 布局见 docs/14-复盘官-最终方案.md §5.0（定稿）：
//   ① 头部（固定）  审查官 + 档位三选一 + 自动看过/自动处理 + 关闭
//   ② 状态条（固定一行） 上次看过 HH:MM · 未读 N 条 · 笔记 vN
//   ③ 内容区（flex-1 min-h-0 overflow-y-auto，**唯一滚动区**）
//        主结论（只展开一条）+ 其余折叠 + 笔记折叠
//   ④ 输入框（shrink-0 贴底，永不消失）
// 空白只允许出现在 ③ 内部，④ 必须贴底。
const VERDICT: Record<string, { label: string; chip: string }> = {
  continue: { label: "继续", chip: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  pivot: { label: "转向", chip: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  wrapup: { label: "该收口了", chip: "bg-rose-500/15 text-rose-600 dark:text-rose-400" },
};

const TIER_LABEL: Record<string, string> = {
  observe: "只看",
  lowrisk: "低风险自动",
  auto: "全自动",
};

// docs/14 §6 的 ack 回执；后端暂未接线（只有 apply / rebut），下面做降级。
type AckVerdict = "accepted" | "held" | "rejected";

const ACK_MSG: Record<AckVerdict, string> = {
  accepted: "已采纳",
  held: "好，我不动",
  rejected: "已忽略",
};

const TIER_HINT: Record<string, string> = {
  observe: "只看：只留结论卡，不动任何方向",
  lowrisk: "低风险自动：只自动做「重开被自己接口打断的」与「标死目标明确拒绝≥3次的」",
  auto: "全自动：同上，配额更高（仍不碰目标/证据）",
};

// 后端可能已经/将要下发的字段（api.ts 还没声明，按约定不改 api.ts，用局部类型兜住）。
type PanelRun = ReviewRun & {
  ack_deadline?: string | number;
  ack_deadline_at?: string | number;
  ack_expires_at?: string | number;
  ack_ttl?: number;
  // 预期信号 / 观察窗口 / 结果校验（后端并行开发中，缺失即降级：不显示）
  expectation?: string;
  observe_until?: string;
  outcome?: PanelOutcome;
};

// 结果校验三态 + 空（空 = 后端还没判或没这个字段）
type PanelOutcome = "effective" | "ineffective" | "voided" | "";

// 主结论卡底部状态带的三种终态样式
const OUTCOME_BAND: Record<"effective" | "ineffective" | "voided", { text: string; cls: string }> = {
  effective: {
    text: "✓ 已生效（预期信号出现了）",
    cls: "border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400",
  },
  ineffective: {
    text: "△ 没效果（预期没出现，可能已升级）",
    cls: "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400",
  },
  voided: {
    text: "⊘ 已撤回（被新证据推翻）",
    cls: "border-slate-400/40 bg-slate-500/10 text-slate-600 dark:text-slate-400",
  },
};

// 历史列表每条前面那个小徽标
const OUTCOME_BADGE: Record<"effective" | "ineffective" | "voided", { text: string; chip: string }> = {
  effective: { text: "✓ 生效", chip: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400" },
  ineffective: { text: "△ 没效果", chip: "bg-amber-500/15 text-amber-600 dark:text-amber-400" },
  voided: { text: "⊘ 撤回", chip: "bg-slate-500/15 text-slate-500 dark:text-slate-400" },
};

// 动作白名单未来会多一个 propose_intent（docs/14 §3），类型上先兜一层。
type PanelAction = ReviewAction & { text?: string; target?: string; summary?: string };

const LS_KEY = (taskId: string) => `artex.review.enabled.${taskId}`;

export function useReviewEnabled(taskId: string) {
  const [enabled, setEnabled] = React.useState(false);
  React.useEffect(() => {
    try {
      setEnabled(localStorage.getItem(LS_KEY(taskId)) === "1");
    } catch {
      /* ignore */
    }
  }, [taskId]);
  const set = React.useCallback(
    (v: boolean) => {
      setEnabled(v);
      try {
        localStorage.setItem(LS_KEY(taskId), v ? "1" : "0");
      } catch {
        /* ignore */
      }
    },
    [taskId],
  );
  return { enabled, setEnabled: set };
}

// useReviewState 轮询审查官状态（未读红点 / 生效档位 / 介入与自动次数 / 熔断）。
// 轻量接口（GET review/state），不拉结论正文，所以可以 15s 一刷。
export function useReviewState(taskId: string) {
  const [state, setState] = React.useState<ReviewState | null>(null);
  const reload = React.useCallback(() => {
    reviewapi
      .state(taskId)
      .then(setState)
      .catch(() => void 0);
  }, [taskId]);
  React.useEffect(() => {
    reload();
    const t = setInterval(reload, 15_000);
    return () => clearInterval(t);
  }, [reload]);
  return { state, reload };
}

export function ReviewCapsule({
  taskId,
  open,
  onToggle,
  enabled,
}: {
  taskId: string;
  open: boolean;
  onToggle: () => void;
  enabled: boolean;
}) {
  const { state } = useReviewState(taskId);
  const unseen = state?.unseen ?? 0;
  // 它自己看过但没人看结论 → 红点一直亮（docs/13：「你不理它，红点不消」）。
  const tripped = state?.tripped;
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={onToggle}
      title={
        tripped
          ? `审查官已熔断：${state?.trip_reason || "自动动作没带来看得见的改善"}`
          : `审查官：整条方向该不该继续（${TIER_LABEL[state?.tier ?? ""] ?? "低风险自动"}）`
      }
      className={cn(unseen > 0 && "border-violet-500/50")}
    >
      <SparklesIcon className="size-3.5" />
      审查官
      {enabled && (
        <span className="ml-1 rounded bg-emerald-500/15 px-1 text-[9px] font-semibold text-emerald-600">开</span>
      )}
      {tripped && <span className="ml-1 rounded bg-rose-500/15 px-1 text-[9px] font-semibold text-rose-600">熔断</span>}
      {unseen > 0 && (
        <span className="ml-1 inline-flex min-w-[14px] items-center justify-center rounded-full bg-violet-500 px-1 text-[9px] font-bold text-white">
          {unseen > 9 ? "9+" : unseen}
        </span>
      )}
      {open && <span className="ml-1 inline-flex size-1.5 rounded-full bg-emerald-500" />}
    </Button>
  );
}

// ── 小工具 ────────────────────────────────────────────────────────────────

// created_at 是后端 to_char 出来的 "YYYY-MM-DD HH:MM"（本地时区）。
function hhmm(ts?: string): string {
  if (!ts) return "--:--";
  const m = ts.match(/(\d{2}):(\d{2})\s*$/);
  if (m) return `${m[1]}:${m[2]}`;
  const d = new Date(ts);
  if (!Number.isNaN(d.getTime())) {
    return `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  }
  return ts;
}

function oneLiner(body: string, max = 42): string {
  const t = (body || "").replace(/\s+/g, " ").trim();
  if (!t) return "（没写正文）";
  const first = t.split(/[。！？!?；;]/)[0] || t;
  return first.length > max ? `${first.slice(0, max)}…` : first;
}

function verdictMeta(v?: string) {
  return VERDICT[v ?? ""] ?? VERDICT.continue;
}

// ack_deadline 可能是 RFC3339 / "YYYY-MM-DD HH:MM" / unix 秒 / unix 毫秒。
function ackDeadlineOf(run: ReviewRun | null): number | null {
  if (!run) return null;
  const r = run as PanelRun;
  const raw = r.ack_deadline ?? r.ack_deadline_at ?? r.ack_expires_at;
  if (typeof raw === "number" && Number.isFinite(raw)) {
    return raw > 1e12 ? raw : raw * 1000;
  }
  if (typeof raw === "string" && raw.trim()) {
    const asNum = Number(raw);
    if (Number.isFinite(asNum) && asNum > 0) return asNum > 1e12 ? asNum : asNum * 1000;
    const t = Date.parse(raw.includes("T") ? raw : raw.replace(" ", "T"));
    if (Number.isFinite(t)) return t;
  }
  if (typeof r.ack_ttl === "number" && r.ack_ttl > 0) {
    const created = Date.parse((run.created_at || "").replace(" ", "T"));
    return Number.isFinite(created) ? created + r.ack_ttl * 1000 : null;
  }
  return null;
}

function ackStatusOf(run: ReviewRun | null): string {
  return ((run?.ack_status ?? "") as string).trim();
}

// outcome 缺失 / 空串 / 未知值 → 一律当「没有」，整段状态带不显示。
function outcomeOf(run: ReviewRun | null | undefined): PanelOutcome {
  const raw = ((run as PanelRun | null | undefined)?.outcome ?? "").trim();
  return raw === "effective" || raw === "ineffective" || raw === "voided" ? raw : "";
}

function expectationOf(run: ReviewRun | null | undefined): string {
  return ((run as PanelRun | null | undefined)?.expectation ?? "").trim();
}

// observe_until 是 ISO 时间；再兜一手 "YYYY-MM-DD HH:MM"（本地时区，后端 to_char 风格）。
function observeUntilOf(run: ReviewRun | null | undefined): number | null {
  const raw = (run as PanelRun | null | undefined)?.observe_until;
  if (typeof raw !== "string" || !raw.trim()) return null;
  const s = raw.trim();
  const t = Date.parse(s.includes("T") ? s : s.replace(" ", "T"));
  return Number.isFinite(t) ? t : null;
}

// useCountdown：给了 deadline 就每秒重算剩下的秒数；到 0 后停在 0。
function useCountdown(deadline: number | null): number | null {
  const [now, setNow] = React.useState(() => Date.now());
  React.useEffect(() => {
    if (!deadline) return;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [deadline]);
  if (!deadline) return null;
  return Math.max(0, Math.ceil((deadline - now) / 1000));
}

// useNow：按固定间隔（观察中 60s）重算当前时间，用于「还剩 N 分钟」；active=false 时不起定时器。
function useNow(intervalMs: number, active: boolean): number {
  const [now, setNow] = React.useState(() => Date.now());
  React.useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs, active]);
  return now;
}

// intent.payload 是 JSON（含 summary/text/description），非 JSON 就原样展示。
function intentDetailText(payload?: string): string {
  const raw = (payload ?? "").trim();
  if (!raw) return "";
  try {
    const obj: unknown = JSON.parse(raw);
    if (obj && typeof obj === "object") {
      const o = obj as Record<string, unknown>;
      for (const f of ["summary", "text", "description", "title"]) {
        const v = o[f];
        if (typeof v === "string" && v.trim()) return v.trim();
      }
    }
  } catch {
    return raw;
  }
  return raw;
}

function actionLabel(a: PanelAction): string {
  if (a.kind === "kill_intent") return `放弃方向 #${a.intent ?? "?"}`;
  if (a.kind === "retry_intent") return `重跑被打断的方向 #${a.intent ?? "?"}`;
  if (a.kind === "set_next_stage") return "换阶段（需你手动确认）";
  // propose_intent（docs/14 §3）：只提议，不自己派活
  const t = (a.text ?? a.target ?? a.summary ?? "").trim();
  return t ? `建议新方向：${t}` : "建议新方向（它没写具体方向）";
}

// 依据里「经验：<KB标题>」不是 cite（cites 只有 intent/finding/fact），
// 只能从正文里取；取不到就整项省略（docs/14 §5.0：没有就省略）。
function kbTitleOf(body: string): { title: string; line: string } | null {
  const m = body.match(/经验[：:]\s*([^\n。；;]{2,40})/);
  if (!m) return null;
  const title = m[1].trim();
  const line = body.split("\n").find((l) => l.includes(m[0])) ?? m[0];
  return title ? { title, line: line.trim() } : null;
}

type QaItem = { key: string; question: string; answer: string; at: string };

// ── 面板 ──────────────────────────────────────────────────────────────────

export function ReviewPanel({
  taskId,
  onClose,
  enabled,
  setEnabled,
}: {
  taskId: string;
  onClose: () => void;
  enabled: boolean;
  setEnabled: (v: boolean) => void;
}) {
  const [busy, setBusy] = React.useState(false);
  const [runs, setRuns] = React.useState<ReviewRun[]>([]);
  const [latest, setLatest] = React.useState<ReviewRun | null>(null);
  const [selectedId, setSelectedId] = React.useState<number | null>(null);
  const [notesVersion, setNotesVersion] = React.useState(0);
  const [notes, setNotes] = React.useState("");
  const [showNotes, setShowNotes] = React.useState(false);
  const [showHistory, setShowHistory] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [rebut, setRebut] = React.useState("");
  const rebutRef = React.useRef<HTMLTextAreaElement>(null);
  const [qa, setQa] = React.useState<QaItem[]>([]);

  // 动作回执：key = `${runId}:${actionIndex}`
  const [actionStates, setActionStates] = React.useState<Record<string, AckVerdict>>({});
  const [actionBusy, setActionBusy] = React.useState<string | null>(null);
  // ack 接口一次不通就整个会话降级，避免每次都打 404。
  const [ackUnsupported, setAckUnsupported] = React.useState(false);

  // 依据展开
  const [openCite, setOpenCite] = React.useState<string | null>(null);
  const [citeDetail, setCiteDetail] = React.useState<Record<string, { loading: boolean; text: string }>>({});
  const [showKb, setShowKb] = React.useState(false);

  const { state, reload } = useReviewState(taskId);

  const load = React.useCallback(() => {
    reviewapi
      .latest(taskId)
      .then((r) => {
        setLatest(r.run ?? null);
        setNotesVersion(r.notes_version ?? 0);
      })
      .catch(() => void 0);
    reviewapi
      .runs(taskId, 20)
      .then((r) => setRuns(r.runs ?? []))
      .catch(() => void 0);
  }, [taskId]);

  React.useEffect(() => {
    load();
    // 结论/未读数保持新鲜（轻量接口，跟 state 一样 15s 一刷）
    const t = setInterval(load, 15_000);
    return () => clearInterval(t);
  }, [load]);

  // 面板打开 = 人看到了：把未读结论标记为已读（红点消），并刷新状态。
  // biome-ignore lint/correctness/useExhaustiveDependencies: latest?.id 函数体里用不到，但新结论出现时要重新标已读
  React.useEffect(() => {
    reviewapi
      .seen(taskId)
      .then(() => reload())
      .catch(() => void 0);
  }, [taskId, latest?.id, reload]);

  // 面板切换任务后，本地问答与回执不跨任务串味。
  // biome-ignore lint/correctness/useExhaustiveDependencies: 只有 taskId 变化才需要重置这些本地状态
  React.useEffect(() => {
    setQa([]);
    setActionStates({});
    setAckUnsupported(false);
    setOpenCite(null);
  }, [taskId]);

  // 新结论出现 → 自动展开最新那条（「只展开最新的一条结论」）。
  const latestId = latest?.id ?? null;
  const lastLatestRef = React.useRef<number | null>(null);
  React.useEffect(() => {
    if (latestId == null) return;
    if (lastLatestRef.current !== latestId) {
      lastLatestRef.current = latestId;
      setSelectedId(latestId);
    }
  }, [latestId]);

  const main = React.useMemo(() => {
    if (selectedId != null) {
      const hit = runs.find((r) => r.id === selectedId);
      if (hit) return hit;
      if (latest && latest.id === selectedId) return latest;
    }
    return latest;
  }, [runs, latest, selectedId]);

  const others = React.useMemo(() => runs.filter((r) => r.id !== main?.id), [runs, main?.id]);
  const unseen = state?.unseen ?? 0;
  const tier = state?.tier ?? "lowrisk";
  const tripped = state?.tripped;
  const lastSeenAt = latest?.finished_at || latest?.created_at || "";
  const vTone = verdictMeta(main?.verdict);
  const isRunning = main?.status === "running";

  const panelActions = (main?.actions ?? []) as PanelAction[];
  // 依据去重（cites 里可能有重复的 kind#id，重了就没有稳定的 key）
  const cites = React.useMemo(() => {
    const out: Array<{ kind: string; id: number }> = [];
    const seen = new Set<string>();
    for (const c of main?.cites ?? []) {
      const key = `${c.kind}#${c.id}`;
      if (seen.has(key)) continue;
      seen.add(key);
      out.push(c);
      if (out.length >= 6) break;
    }
    return out;
  }, [main?.cites]);
  const ackStatus = ackStatusOf(main);
  // 整条结论级的回执（ack_status）也能当作每个动作的初始状态
  const runAck: AckVerdict | undefined =
    ackStatus === "accepted" || ackStatus === "held" || ackStatus === "rejected"
      ? (ackStatus as AckVerdict)
      : undefined;
  const backendDeadline = ackDeadlineOf(main);
  const localDeadlineRef = React.useRef<{ id: number; at: number } | null>(null);
  // 后端还没下发 ack_deadline 时，用本地 90s 兜一个（提示里注明是前端估算）。
  const localDeadline = React.useMemo(() => {
    if (!main || backendDeadline) return null;
    if (ackStatus) return null;
    if (main.level !== "action") return null;
    if (tier === "observe") return null;
    const cur = localDeadlineRef.current;
    if (cur && cur.id === main.id) return cur.at;
    const at = Date.now() + 90_000;
    localDeadlineRef.current = { id: main.id, at };
    return at;
  }, [main, backendDeadline, ackStatus, tier]);
  const deadline = backendDeadline ?? localDeadline;
  const left = useCountdown(deadline);
  const expired = left === 0;

  const runOnce = React.useCallback(async () => {
    setBusy(true);
    setMsg("");
    try {
      const r = await reviewapi.run(taskId);
      if (r.run) {
        setLatest(r.run);
        setSelectedId(r.run.id);
      }
      setMsg(r.run?.model === "rule" ? "这次是规则版判断（模型暂时不可用）" : "看完了");
      load();
    } catch {
      setMsg("它现在看不了（前面可能还有一个在评审，或任务已终止）");
    } finally {
      setBusy(false);
    }
  }, [taskId, load]);

  // 开关打开时：本页面停留期间每 10 分钟自动看一次
  React.useEffect(() => {
    if (!enabled) return;
    const t = setInterval(() => void runOnce(), 10 * 60 * 1000);
    return () => clearInterval(t);
  }, [enabled, runOnce]);

  // 降级路径：后端没接 ack 时，采纳仍然走原来的 apply，held/rejected 只本地忽略。
  async function applyOnly(action: PanelAction, key: string) {
    if (!main) return;
    try {
      const r = await reviewapi.apply(taskId, main.id, [action]);
      if (r.applied > 0) {
        setActionStates((s) => ({ ...s, [key]: "accepted" }));
        setMsg("已经照它说的做了");
      } else {
        setMsg("没能执行（这条方向的状态已经变了）");
      }
    } catch {
      setMsg("执行失败");
    }
  }

  async function ackAction(action: PanelAction, idx: number, verdict: AckVerdict) {
    if (!main) return;
    const key = `${main.id}:${idx}`;
    if (actionBusy === key) return;
    setActionBusy(key);
    setMsg("");
    try {
      // 1) 先按 docs/14 §6 打 ack（POST /review/{run}/ack, body {action}）
      if (!ackUnsupported) {
        try {
          await http<{ ok: boolean }>(`/tasks/${encodeURIComponent(taskId)}/review/${main.id}/ack`, {
            method: "POST",
            body: JSON.stringify({ action: verdict }),
          });
          setActionStates((s) => ({ ...s, [key]: verdict }));
          setMsg(ACK_MSG[verdict]);
          return;
        } catch {
          // 接口不存在（404）或报错 → 本会话永久降级，不再重复尝试
          setAckUnsupported(true);
        }
      }
      // 2) 降级：采纳 → 老 apply；其余只本地忽略
      if (verdict === "accepted") {
        await applyOnly(action, key);
        return;
      }
      setActionStates((s) => ({ ...s, [key]: verdict }));
      setMsg("已忽略（后端未接）");
    } finally {
      setActionBusy(null);
    }
  }

  async function sendRebut() {
    const text = rebut.trim();
    if (!main || !text) return;
    const runId = main.id;
    setBusy(true);
    try {
      const r = await reviewapi.rebut(taskId, runId, text);
      const answer = (r.run?.body ?? "").trim() || "它重新判了一遍（点上面的「看一次」看最新结论）";
      // 「你问 / 它答」两段追加到内容区顶部（本地状态即可）
      setQa((prev) => [
        { key: `${runId}:${Date.now()}`, question: text, answer, at: hhmm(new Date().toISOString()) },
        ...prev,
      ]);
      if (r.run) {
        setLatest(r.run);
        setSelectedId(r.run.id);
      }
      setRebut("");
      setMsg("它重新判了一遍");
      load();
    } catch {
      setMsg("反驳没送到（前面可能还有一个在评审）");
    } finally {
      setBusy(false);
    }
  }

  async function toggleNotes() {
    if (!showNotes && !notes) {
      try {
        const n = await reviewapi.notes(taskId);
        setNotes(n.body || "");
        if (n.version) setNotesVersion(n.version);
      } catch {
        /* ignore */
      }
    }
    setShowNotes((v) => !v);
  }

  async function toggleCite(kind: string, id: number) {
    const key = `${kind}#${id}`;
    setOpenCite((cur) => (cur === key ? null : key));
    if (citeDetail[key]) return;
    setCiteDetail((d) => ({ ...d, [key]: { loading: true, text: "" } }));
    let text = "";
    try {
      if (kind === "finding") {
        const f = await api.getFinding(String(id), taskId);
        text = [f.name || f.vulnclass, f.summary].filter(Boolean).join(" · ");
      } else if (kind === "intent") {
        const r = await api.intentsPage(taskId, 0, 300);
        const node = r.items.find((n) => n.id === String(id));
        text = intentDetailText(node?.payload);
        if (!text) text = "没找到这条方向（可能已归档或被清理）";
      } else {
        text = `这条依据（${kind}#${id}）后端还没有详情接口`;
      }
    } catch {
      text = "详情拉不到（后端未提供或已删除）";
    }
    setCiteDetail((d) => ({ ...d, [key]: { loading: false, text: text || "这条依据没有可展示的详情" } }));
  }

  const kb = kbTitleOf(main?.body ?? "");
  const citeDetailText = openCite ? citeDetail[openCite] : undefined;

  // 预期信号：没有就整行不显示
  const expectation = expectationOf(main);
  // 底部状态带：终态（effective/ineffective/voided）优先，否则看 observe_until 是否还在观察窗口内。
  const outcome = outcomeOf(main);
  const observeUntil = observeUntilOf(main);
  const observing = !outcome && observeUntil != null;
  // 观察中每分钟刷新一次；不在观察中就不开定时器。
  const nowTs = useNow(60_000, observing);
  const observeLeftMs = observeUntil != null ? observeUntil - nowTs : 0;
  const observeLeftMin = Math.ceil(observeLeftMs / 60_000);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* ① 头部（固定不滚）：审查官 + 档位三选一 + 自动看过/处理 + 关闭 */}
      <div className="shrink-0 border-b">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 px-3 pb-1 pt-2">
          <span className="flex size-5 items-center justify-center rounded bg-violet-500 text-[10px] font-bold text-white">
            复
          </span>
          <div className="text-[12.5px] font-semibold">审查官</div>
          <button
            type="button"
            className={cn(
              "flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[10px]",
              enabled ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-600" : "text-muted-foreground",
            )}
            onClick={() => setEnabled(!enabled)}
            title={enabled ? "已开启：每 10 分钟自动看一次" : "已关闭：只在你点「看一次」时评审"}
          >
            <span className={cn("size-1.5 rounded-full", enabled ? "bg-emerald-500" : "bg-slate-300")} />
            {enabled ? "已开启" : "已关闭"}
          </button>
          <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">
            自动看过 {state?.interventions ?? 0} 次 · 自动处理 {state?.auto_actions ?? 0} 次
          </span>
          <button
            type="button"
            className="shrink-0 text-[11px] text-muted-foreground hover:text-foreground"
            onClick={onClose}
            title="关闭面板（不影响它是否继续看）"
          >
            关闭
          </button>
        </div>
        <div className="flex flex-wrap items-center gap-1 px-3 pb-1.5">
          <span className="text-[10px] text-muted-foreground">档位</span>
          {(["observe", "lowrisk", "auto"] as const).map((t) => (
            <button
              type="button"
              key={t}
              className={cn(
                "rounded-full border px-2 py-0.5 text-[10.5px]",
                tier === t
                  ? "border-violet-500/50 bg-violet-500/10 text-violet-600"
                  : "text-muted-foreground hover:text-foreground",
              )}
              title={TIER_HINT[t]}
              onClick={() => {
                reviewapi
                  .setTier(taskId, t)
                  .then(() => reload())
                  .catch(() => setMsg("改不了档位（后端未接档位设置）"));
              }}
            >
              {TIER_LABEL[t]}
            </button>
          ))}
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto h-6 shrink-0 gap-1 px-1.5 text-[10.5px]"
            onClick={runOnce}
            disabled={busy}
            title="现在看一次"
          >
            {busy ? <Loader2Icon className="size-3 animate-spin" /> : <RefreshCwIcon className="size-3" />}
            看一次
          </Button>
        </div>
      </div>

      {/* ② 状态条（固定一行）：● 上次看过 HH:MM · 还有 N 条未读 · 笔记 vN */}
      <div className="flex shrink-0 items-center gap-1 overflow-hidden border-b px-3 py-1.5 text-[10.5px] text-muted-foreground">
        <span
          className={cn("size-1.5 shrink-0 rounded-full", unseen > 0 ? "bg-rose-500" : "bg-emerald-500")}
          title={unseen > 0 ? "有没看过的结论" : "没有未读结论"}
        />
        <span className="shrink-0">上次看过 {hhmm(lastSeenAt)}</span>
        <span className="shrink-0">· 还有 {unseen} 条未读</span>
        <span className="shrink-0">· 笔记 v{notesVersion || 0}</span>
        {tripped && (
          <span
            className="ml-auto shrink-0 truncate text-rose-600"
            title={`已熔断转「只看」：${state?.trip_reason || "自动动作没带来看得见的改善"}（点上面的档位可重新授权）`}
          >
            · 已熔断
          </span>
        )}
      </div>

      {/* ③ 内容区（唯一滚动区） */}
      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2.5">
        {/* 追问问答：最新一条在最上面 */}
        {qa.map((q) => (
          <div key={q.key} className="mb-2 rounded-md border bg-muted/30 px-2 py-1.5 text-[11.5px]">
            <div className="text-[10px] text-muted-foreground">你问</div>
            <div className="whitespace-pre-wrap break-words">{q.question}</div>
            <div className="mt-1 text-[10px] text-muted-foreground">它答</div>
            <div className="whitespace-pre-wrap break-words">{q.answer}</div>
          </div>
        ))}

        {!main ? (
          <div className="rounded-lg border border-dashed px-4 py-8 text-center text-[11.5px] leading-relaxed text-muted-foreground">
            它还没看过这个任务
            <br />
            点上面的「看一次」，它会读当前战果告诉你该不该继续；有结论后你也能在下面问它。
          </div>
        ) : (
          <div className="space-y-2">
            {/* 主结论卡（每次只展开这一条） */}
            <div className="rounded-md border px-2 py-2">
              <div className="flex flex-wrap items-center gap-1.5">
                <span className={cn("rounded px-1.5 py-0.5 text-[10px] font-semibold", vTone.chip)}>
                  {isRunning ? "正在看…" : vTone.label}
                </span>
                <span className="text-[10.5px] text-muted-foreground">{main.created_at}</span>
                <span className="rounded bg-slate-500/10 px-1.5 py-0.5 text-[10px] text-slate-600">
                  {main.model === "rule" ? "规则版" : "模型版"}
                </span>
                {main.level === "action" && (
                  <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-600">
                    建议动任务
                  </span>
                )}
                {main.auto_applied && (
                  <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-emerald-600">
                    它自己做了
                  </span>
                )}
                {ackStatus === "accepted" && (
                  <span className="rounded bg-emerald-500/15 px-1.5 py-0.5 text-[10px] text-emerald-600">已采纳</span>
                )}
                {ackStatus === "held" && (
                  <span className="rounded bg-sky-500/15 px-1.5 py-0.5 text-[10px] text-sky-600">你自己处理</span>
                )}
                {(ackStatus === "rejected" || ackStatus === "stale") && (
                  <span className="rounded bg-rose-500/15 px-1.5 py-0.5 text-[10px] text-rose-600">
                    {ackStatus === "stale" ? "已过期" : "已反驳"}
                  </span>
                )}
              </div>

              <div className="mt-1.5 whitespace-pre-wrap break-words text-[12px] leading-relaxed">
                {isRunning && !main.body ? "它正在读快照，结论马上出来…" : main.body || "（这次它没写正文）"}
              </div>

              {/* 依据：intent#70 · finding#31 · 经验：<KB标题>（没有就省略） */}
              <div className="mt-1.5 flex flex-wrap items-center gap-1 text-[10.5px] text-muted-foreground">
                <span>依据：</span>
                {cites.length === 0 && <span>（它没给依据）</span>}
                {cites.map((c) => {
                  const key = `${c.kind}#${c.id}`;
                  return (
                    <button
                      type="button"
                      key={key}
                      className={cn(
                        "rounded border px-1 py-0.5 text-[10px] hover:bg-accent",
                        openCite === key && "border-violet-500/50 bg-violet-500/10 text-violet-600",
                      )}
                      onClick={() => void toggleCite(c.kind, c.id)}
                      title="点开看这条依据的详情"
                    >
                      {c.kind}#{c.id}
                    </button>
                  );
                })}
                {kb && (
                  <button
                    type="button"
                    className={cn(
                      "max-w-full truncate rounded border px-1 py-0.5 text-[10px] hover:bg-accent",
                      showKb && "border-violet-500/50 bg-violet-500/10 text-violet-600",
                    )}
                    onClick={() => setShowKb((v) => !v)}
                    title={kb.title}
                  >
                    经验：{kb.title}
                  </button>
                )}
              </div>
              {showKb && kb && (
                <div className="mt-1 rounded border bg-muted/40 px-2 py-1 text-[10.5px] leading-relaxed">{kb.line}</div>
              )}
              {citeDetailText && (
                <div className="mt-1 rounded border bg-muted/40 px-2 py-1 text-[10.5px] leading-relaxed">
                  {citeDetailText.loading ? "拉取中…" : citeDetailText.text}
                </div>
              )}

              {/* 预期信号：接在「依据」下方；后端没下发 expectation 时整行省略 */}
              {expectation && (
                <div
                  className="mt-1 flex flex-wrap items-baseline gap-1 text-[10.5px] text-muted-foreground"
                  title={expectation}
                >
                  <span className="shrink-0">预期：</span>
                  <span className="min-w-0 whitespace-pre-wrap break-words text-foreground/80">{expectation}</span>
                </div>
              )}

              {/* 自动处理倒计时（有 ack_deadline 且未 ack 才显示） */}
              {deadline != null && !ackStatus && !isRunning && (
                <div
                  className={cn(
                    "mt-2 flex items-center gap-1.5 rounded border px-2 py-1 text-[10.5px]",
                    expired
                      ? "border-rose-500/40 bg-rose-500/10 text-rose-600"
                      : "border-amber-500/40 bg-amber-500/10 text-amber-700",
                  )}
                  title={
                    backendDeadline
                      ? "到点后它按当前档位自动处理；点「我在，我自己处理」可取消"
                      : "后端还没下发 ack_deadline，这里的倒计时是前端按 90s 估算的"
                  }
                >
                  {expired ? "已自动处理，刷新查看" : `自动处理 ${left}s`}
                </div>
              )}

              {/* 动作列表：每个动作一行 + 采纳 / 我在 / 不要 */}
              {panelActions.length > 0 && (
                <div className="mt-2 space-y-1">
                  <div className="text-[10.5px] font-semibold text-muted-foreground">它可以替你做的动作</div>
                  {panelActions.map((a, i) => {
                    const key = `${main.id}:${i}`;
                    const st = actionStates[key] ?? runAck;
                    // held / rejected 只在本地隐藏（后端未接 ack 时唯一的「忽略」手段）
                    if (st === "held" || st === "rejected") return null;
                    return (
                      <div
                        key={key}
                        className="flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border px-2 py-1.5"
                      >
                        <div className="min-w-0 flex-1">
                          <div className="text-[11.5px] font-medium">{actionLabel(a)}</div>
                          {a.why && (
                            <div className="truncate text-[10.5px] text-muted-foreground" title={a.why}>
                              {a.why}
                            </div>
                          )}
                        </div>
                        {st === "accepted" ? (
                          <span className="shrink-0 rounded border border-emerald-500/40 bg-emerald-500/10 px-1.5 py-0.5 text-[10px] text-emerald-700">
                            已采纳
                          </span>
                        ) : (
                          <div className="flex shrink-0 items-center gap-1">
                            <button
                              type="button"
                              className="rounded border px-1.5 py-0.5 text-[10px] hover:bg-accent disabled:opacity-50"
                              onClick={() => void ackAction(a, i, "accepted")}
                              disabled={actionBusy === key}
                            >
                              采纳
                            </button>
                            <button
                              type="button"
                              className="rounded border px-1.5 py-0.5 text-[10px] hover:bg-accent disabled:opacity-50"
                              onClick={() => void ackAction(a, i, "held")}
                              disabled={actionBusy === key}
                              title="我不在的时候才让它自己动；这次我自己处理"
                            >
                              我在，我自己处理
                            </button>
                            <button
                              type="button"
                              className="rounded border px-1.5 py-0.5 text-[10px] text-muted-foreground hover:bg-accent disabled:opacity-50"
                              onClick={() => void ackAction(a, i, "rejected")}
                              disabled={actionBusy === key}
                            >
                              不要
                            </button>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}

              {/* 底部状态带（动作行下方，三选一）：观察中 / 已生效 / 没效果 / 已撤回；都没有就不占位 */}
              {outcome ? (
                <div
                  className={cn(
                    "mt-2 flex items-center gap-1 rounded border px-2 py-1 text-[10.5px]",
                    OUTCOME_BAND[outcome].cls,
                  )}
                >
                  {OUTCOME_BAND[outcome].text}
                </div>
              ) : observing ? (
                <div
                  className={cn(
                    "mt-2 flex items-center gap-1 rounded border px-2 py-1 text-[10.5px]",
                    observeLeftMin > 0
                      ? "border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-400"
                      : "border-amber-500/40 bg-amber-500/10 text-amber-700 dark:text-amber-400",
                  )}
                  title={
                    observeLeftMin > 0 && observeUntil != null
                      ? `观察窗口到 ${new Date(observeUntil).toLocaleString()}（每分钟刷新）`
                      : "观察窗口已到期，但后端还没给出结果"
                  }
                >
                  {observeLeftMin > 0 ? `⟳ 观察中（还剩 ${observeLeftMin} 分钟）` : "⟳ 观察中（快到点了，刷新查看）"}
                </div>
              ) : null}
            </div>

            {/* 其余历史结论：折叠一行，展开是紧凑列表，点某条切换成主结论 */}
            {others.length > 0 && (
              <div>
                <button
                  type="button"
                  className="flex w-full items-center gap-1 rounded px-1 py-1 text-left text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
                  onClick={() => setShowHistory((v) => !v)}
                >
                  <ChevronRightIcon
                    className={cn("size-3 shrink-0 transition-transform", showHistory && "rotate-90")}
                  />
                  另外 {others.length} 条（{showHistory ? "点击折叠" : "点击展开"}）
                </button>
                {showHistory && (
                  <div className="mt-1 space-y-1">
                    {others.map((r) => {
                      const t = verdictMeta(r.verdict);
                      const ro = outcomeOf(r);
                      return (
                        <button
                          type="button"
                          key={r.id}
                          className="flex w-full items-center gap-1.5 rounded border px-1.5 py-1 text-left hover:bg-accent"
                          onClick={() => {
                            setSelectedId(r.id);
                            setShowHistory(false);
                            setOpenCite(null);
                            setShowKb(false);
                          }}
                          title={r.body}
                        >
                          {ro && (
                            <span
                              className={cn("shrink-0 rounded px-1 py-0.5 text-[9.5px]", OUTCOME_BADGE[ro].chip)}
                              title={`结果校验：${OUTCOME_BADGE[ro].text}`}
                            >
                              {OUTCOME_BADGE[ro].text}
                            </span>
                          )}
                          <span className="shrink-0 text-[10px] text-muted-foreground">{hhmm(r.created_at)}</span>
                          <span className={cn("shrink-0 rounded px-1 py-0.5 text-[10px]", t.chip)}>
                            {r.status === "running" ? "在看" : t.label}
                          </span>
                          <span className="min-w-0 flex-1 truncate text-[11px]">{oneLiner(r.body)}</span>
                        </button>
                      );
                    })}
                  </div>
                )}
              </div>
            )}

            {/* 它的笔记：折叠 */}
            <div>
              <button
                type="button"
                className="flex w-full items-center gap-1 rounded px-1 py-1 text-left text-[11px] text-muted-foreground hover:bg-accent hover:text-foreground"
                onClick={() => void toggleNotes()}
              >
                <ChevronRightIcon className={cn("size-3 shrink-0 transition-transform", showNotes && "rotate-90")} />
                它的笔记 v{notesVersion || 0}（{showNotes ? "点击折叠" : "点击展开"}）
              </button>
              {showNotes && (
                <pre className="mt-1 max-h-60 overflow-auto whitespace-pre-wrap break-words rounded border bg-muted/40 px-2 py-1.5 text-[10.5px] leading-relaxed">
                  {notes || "它还没有笔记（每评审一次会更新）"}
                </pre>
              )}
            </div>
          </div>
        )}
      </div>

      {/* ④ 输入框（固定贴底，永远可见） */}
      <div className="shrink-0 border-t bg-background px-2 py-2">
        <div className="flex items-end gap-1.5">
          <textarea
            ref={rebutRef}
            className="min-h-[38px] flex-1 resize-none rounded-md border bg-background px-2 py-1.5 text-[11.5px] leading-relaxed disabled:opacity-60"
            rows={2}
            placeholder={main ? "问它一个问题…（它只解释，不执行）" : "它还没看过这个任务，先点上面的「看一次」"}
            value={rebut}
            onChange={(e) => setRebut(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
                e.preventDefault();
                void sendRebut();
              }
            }}
            disabled={!main || busy}
          />
          <QuickPhrases
            size="sm"
            variant="outline"
            className="h-[38px] px-2"
            onPick={(text) => {
              setRebut((v) => (v ? `${v}\n${text}` : text));
              requestAnimationFrame(() => {
                const el = rebutRef.current;
                if (!el) return;
                el.focus();
                el.setSelectionRange(el.value.length, el.value.length);
              });
            }}
          />
          <Button
            size="sm"
            variant="outline"
            className="shrink-0"
            onClick={() => void sendRebut()}
            disabled={!main || busy || !rebut.trim()}
          >
            {busy ? <Loader2Icon className="size-3.5 animate-spin" /> : null}
            发送
          </Button>
        </div>
        {msg && <div className="mt-1 text-[10.5px] text-muted-foreground">{msg}</div>}
      </div>
    </div>
  );
}
