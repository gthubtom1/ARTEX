"use client";

import * as React from "react";

import { RefreshCwIcon, SparklesIcon } from "lucide-react";

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
  const reload = React.useCallback(async () => {
    setBusy(true);
    try {
      const r = await phrasesapi.get(taskId);
      setItems(r.phrases ?? []);
      setHas(r.has ?? []);
    } catch {
      void 0;
    } finally {
      setBusy(false);
    }
  }, [taskId]);

  React.useEffect(() => {
    void reload();
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
        onClick={() => void reload()}
        disabled={disabled || busy}
        title="按当前已拿到的战果重新算这几句话（随时可点）"
      >
        <RefreshCwIcon className={busy ? "size-3 animate-spin" : "size-3"} />
        重算
      </button>
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
