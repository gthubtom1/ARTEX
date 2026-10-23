"use client";

import * as React from "react";

import { CheckIcon, MessageSquarePlusIcon, PencilIcon, PlusIcon, Trash2Icon, XIcon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Popover, PopoverContent, PopoverTitle, PopoverTrigger } from "@/components/ui/popover";
import { Textarea } from "@/components/ui/textarea";
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

// 常用语（Quick Phrases）
//
// 全局共享的一份小抄：所有对话框（旁路提问 / 审查官 / 主 Agent）共用同一个按钮和
// 同一份 localStorage（key = artex.quick_phrases.v1）。点一条只把文字填进输入框，
// 绝不自动发送 —— 由父组件的 onPick 决定写进哪个 state。
const STORAGE_KEY = "artex.quick_phrases.v1";

export interface QuickPhrase {
  id: string;
  text: string;
}

// 默认预置 6 条：只在 localStorage 里完全没有这个 key 时写入一次。
const DEFAULT_PHRASES: QuickPhrase[] = [
  { id: "qp-default-1", text: "现在进展到哪一步了？用大白话说" },
  { id: "qp-default-2", text: "这条方向还要继续吗？还差什么？" },
  { id: "qp-default-3", text: "把刚才的结论整理成一段能发给客户的话" },
  { id: "qp-default-4", text: "换个方向试试，别在这条上继续花时间" },
  { id: "qp-default-5", text: "这条漏洞现在能让我拿到什么？" },
  { id: "qp-default-6", text: "先停一下，说说你现在打算怎么做" },
];

function newId(): string {
  return `qp-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function readPhrases(): QuickPhrase[] {
  if (typeof window === "undefined") return [];
  let raw: string | null = null;
  try {
    raw = window.localStorage.getItem(STORAGE_KEY);
  } catch {
    return DEFAULT_PHRASES; // 隐私模式/禁用存储：本次会话内仍能用预置条目
  }
  if (raw !== null) {
    try {
      const parsed: unknown = JSON.parse(raw);
      if (Array.isArray(parsed)) {
        const items: QuickPhrase[] = [];
        for (const entry of parsed) {
          if (!entry || typeof entry !== "object") continue;
          const rec = entry as { id?: unknown; text?: unknown };
          if (typeof rec.text !== "string" || rec.text.trim() === "") continue;
          items.push({ id: typeof rec.id === "string" && rec.id ? rec.id : newId(), text: rec.text });
        }
        return items;
      }
    } catch {
      // 数据损坏 → 当作没有，重新写入预置条目
    }
  }
  writePhrases(DEFAULT_PHRASES);
  return DEFAULT_PHRASES;
}

function writePhrases(items: QuickPhrase[]): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(items));
  } catch {
    // 存储不可用：保持内存态即可，不打断输入
  }
}

export function QuickPhrases({
  onPick,
  className,
  size = "icon-xs",
  variant = "ghost",
  disabled = false,
  title = "常用语",
}: {
  /** 点某条常用语时回调；父组件负责写进输入框并聚焦（不发送）。 */
  onPick: (text: string) => void;
  className?: string;
  size?: React.ComponentProps<typeof Button>["size"];
  variant?: React.ComponentProps<typeof Button>["variant"];
  disabled?: boolean;
  title?: string;
}) {
  const [open, setOpen] = React.useState(false);
  const [items, setItems] = React.useState<QuickPhrase[]>([]);
  // draft 非 null = 正在新建/编辑（"" 为新建，内容为编辑原文本）
  const [editing, setEditing] = React.useState<{ id: string | null; draft: string } | null>(null);
  const editorRef = React.useRef<HTMLTextAreaElement>(null);

  // 首次打开（或挂载）时读一次；只在客户端读，避免 SSR 与水合不一致。
  React.useEffect(() => {
    setItems(readPhrases());
  }, []);

  // biome-ignore lint/correctness/useExhaustiveDependencies: 编辑器出现后聚焦一次即可
  React.useEffect(() => {
    if (editing) editorRef.current?.focus();
  }, [editing?.id]);

  const persist = React.useCallback((next: QuickPhrase[]) => {
    setItems(next);
    writePhrases(next);
  }, []);

  function startCreate() {
    setEditing({ id: null, draft: "" });
  }

  function startEdit(item: QuickPhrase) {
    setEditing({ id: item.id, draft: item.text });
  }

  function commit() {
    if (!editing) return;
    const text = editing.draft.trim();
    if (!text) return;
    if (editing.id) {
      persist(items.map((it) => (it.id === editing.id ? { ...it, text } : it)));
    } else {
      persist([...items, { id: newId(), text }]);
    }
    setEditing(null);
  }

  function remove(id: string) {
    persist(items.filter((it) => it.id !== id));
    if (editing?.id === id) setEditing(null);
  }

  function pick(item: QuickPhrase) {
    onPick(item.text);
    setEditing(null);
    setOpen(false);
  }

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setEditing(null);
        else setItems(readPhrases()); // 每次打开都重新读，别的标签页改了也能看到
      }}
    >
      <TooltipProvider delayDuration={300}>
        <Tooltip>
          <TooltipTrigger asChild>
            <PopoverTrigger asChild>
              <Button
                type="button"
                variant={variant}
                size={size}
                className={cn("shrink-0", className)}
                disabled={disabled}
                title={title}
                aria-label={title}
              >
                <MessageSquarePlusIcon />
              </Button>
            </PopoverTrigger>
          </TooltipTrigger>
          <TooltipContent>{title}</TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <PopoverContent align="end" sideOffset={6} className="w-80 max-w-[calc(100vw-2rem)] gap-2 p-2">
        <div className="flex items-center justify-between gap-2 px-1">
          <PopoverTitle className="text-xs">常用语</PopoverTitle>
          {editing ? null : (
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="h-6 px-1.5 text-xs"
              onClick={startCreate}
              title="新建常用语"
            >
              <PlusIcon className="size-3" />
              新建常用语
            </Button>
          )}
        </div>

        {editing && (
          <div className="flex flex-col gap-1.5 rounded-md border bg-muted/30 p-1.5">
            <Textarea
              ref={editorRef}
              rows={2}
              value={editing.draft}
              placeholder="写一句常说的话…"
              className="min-h-[3.25rem] resize-none text-xs"
              onChange={(event) => setEditing({ ...editing, draft: event.target.value })}
              onKeyDown={(event) => {
                if (event.key === "Escape") {
                  event.preventDefault();
                  setEditing(null);
                }
                if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) {
                  event.preventDefault();
                  commit();
                }
              }}
            />
            <div className="flex items-center justify-end gap-1">
              <Button type="button" variant="ghost" size="xs" onClick={() => setEditing(null)}>
                <XIcon className="size-3" />
                取消
              </Button>
              <Button type="button" variant="secondary" size="xs" onClick={commit} disabled={!editing.draft.trim()}>
                <CheckIcon className="size-3" />
                {editing.id ? "保存修改" : "保存"}
              </Button>
            </div>
          </div>
        )}

        {items.length === 0 && !editing ? (
          <p className="px-1 py-3 text-muted-foreground text-xs leading-relaxed">
            还没有常用语。把常说的话存下来，下次点一下就填进输入框。
          </p>
        ) : (
          <ul className="flex max-h-72 list-none flex-col gap-0.5 overflow-y-auto p-0" aria-label="已保存的常用语">
            {items.map((item) => (
              <li key={item.id} className="group/qp flex items-center gap-0.5 rounded-md hover:bg-muted">
                <button
                  type="button"
                  onClick={() => pick(item)}
                  title={item.text}
                  className="min-w-0 flex-1 truncate rounded-md px-1.5 py-1.5 text-left text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                >
                  {item.text}
                </button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="size-6 opacity-0 focus-visible:opacity-100 group-hover/qp:opacity-100"
                  title="编辑"
                  aria-label={`编辑：${item.text}`}
                  onClick={() => startEdit(item)}
                >
                  <PencilIcon className="size-3" />
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-xs"
                  className="size-6 text-muted-foreground opacity-0 hover:text-destructive focus-visible:opacity-100 group-hover/qp:opacity-100"
                  title="删除"
                  aria-label={`删除：${item.text}`}
                  onClick={() => remove(item.id)}
                >
                  <Trash2Icon className="size-3" />
                </Button>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}
