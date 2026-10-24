"use client";

import * as React from "react";

import { RefreshCwIcon, SparklesIcon } from "lucide-react";
import { toast } from "sonner";

import { phrasesapi } from "@/lib/api";
import type { TaskPhrase } from "@/lib/types";
import { cn } from "@/lib/utils";

// 动态快捷短语（P1）：按"这个站现在拿到了什么"生成 3–5 条。
//
// 与「常用语」的区别：
//   · 常用语 = 30 条全局小抄，任何时候都能用，存在 localStorage，可自己编辑；
//   · 这里 = 此刻这一句，后端按当前战果/漏洞实时算（`GET /api/tasks/{id}/phrases`）。
//
// 铁律：点一下**只填入输入框**，绝不自动发送 —— 发不发、改不改都是人的事。
export function TaskPhrases({
  taskId,
  onPick,
  disabled = false,
  className,
}: {
  taskId: string;
  onPick: (text: string) => void;
  disabled?: boolean;
  className?: string;
}) {
  const [items, setItems] = React.useState<TaskPhrase[]>([]);
  const [has, setHas] = React.useState<string[]>([]);
  const [busy, setBusy] = React.useState(false);

  // 按**当前**战果重算（随时可点）：拿到新东西后说法就变了，不能只算一次。
  const [stamp, setStamp] = React.useState("");
  // manual=true：人点的「重算」，要给出反馈（否则点了没变化，看着像没生效）
  const reload = React.useCallback(
    async (manual = false) => {
      setBusy(true);
      try {
        const r = await phrasesapi.get(taskId);
        const next = r.phrases ?? [];
        const nextHas = r.has ?? [];
        const changed = JSON.stringify(next.map((p) => p.text)) !== JSON.stringify(items.map((p) => p.text));
        setItems(next);
        setHas(nextHas);
        const n = new Date();
        const hhmm = `${String(n.getHours()).padStart(2, "0")}:${String(n.getMinutes()).padStart(2, "0")}`;
        setStamp(hhmm);
        if (manual) {
          toast.success(
            changed
              ? `已按当前战果重算：${next.length} 条（内容变了，说明战果刚更新过）`
              : `已按当前战果重算：还是这 ${next.length} 条（先拿到新东西它才会变）`,
          );
        }
      } catch {
        if (manual) toast.error("重算失败（后端不可达），稍后再点一次");
      } finally {
        setBusy(false);
      }
    },
    [taskId, items],
  );

  React.useEffect(() => {
    void reload();
    // 只在任务切换时自动拉一次；重算由人点（items 变化不重新触发，避免循环）
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskId]);

  // 「一盏灯」（docs/24 §九）：战况卡点了 ✦ 分析，短语这边免费重排一次（不再花 token）。
  React.useEffect(() => {
    const onAnalyze = () => void reload();
    window.addEventListener("artex:analyze", onAnalyze);
    return () => window.removeEventListener("artex:analyze", onAnalyze);
  }, [reload]);

  if (items.length === 0) return null;

  return (
    <div className={cn("flex flex-wrap items-center gap-1 px-1 pb-1", className)}>
      <span className="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground">
        <SparklesIcon className="size-3" />
        这个站现在能说
        {has.length > 0 && <span className="opacity-70">（已有：{has.join("/")}）</span>}
      </span>
      <button
        type="button"
        className="inline-flex items-center gap-0.5 rounded border px-1 py-0.5 text-[10px] text-muted-foreground hover:bg-accent disabled:opacity-50"
        onClick={() => void reload(true)}
        disabled={disabled || busy}
        title="按当前已拿到的战果重新算这几句话（随时可点）"
      >
        <RefreshCwIcon className={busy ? "size-3 animate-spin" : "size-3"} />
        重算
      </button>
      {stamp && <span className="text-[9.5px] text-muted-foreground">更新于 {stamp}</span>}
      {items.map((p) => (
        <button
          key={p.key}
          type="button"
          className="max-w-[46%] truncate rounded-full border bg-card px-2 py-0.5 text-[10.5px] hover:bg-accent disabled:opacity-50"
          title={p.why}
          disabled={disabled}
          onClick={() => onPick(p.text)}
        >
          {p.text}
        </button>
      ))}
    </div>
  );
}
