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
const STORAGE_KEY = "artex.quick_phrases.v2"; // v2：预置从 6 条扩到 30 条（九类场景）

export interface QuickPhrase {
  id: string;
  text: string;
}

// 默认预置 30 条（覆盖判断/入口/取证/推进/授权/纠偏/收口/排障/学习九类）：
// 只在 localStorage 里完全没有这个 key 时写入一次；用户改过就不再覆盖。
const DEFAULT_PHRASES: QuickPhrase[] = [
  // ① 判断类（先决定值不值得打）
  { id: "qp-1", text: "先别乱打。用只读手法摸 20 分钟，然后告诉我：这个站值不值得继续，硬在哪" },
  { id: "qp-2", text: "这个站最可能的入口是哪几个？给出 URL 和理由，不要泛泛而谈" },
  { id: "qp-3", text: "它前面有没有 WAF 或 CDN？如果有，先把绕过思路列出来再动手" },
  // ② 入口/攻击面
  { id: "qp-4", text: "把所有入口列成清单：URL、方法、参数、是否需要登录、响应特征" },
  { id: "qp-5", text: "把接口面单独拉一遍：/api、/rest、swagger、actuator，找出未授权的" },
  { id: "qp-6", text: "找一下上传点、模板编辑、导入导出这类能落文件的功能" },
  // ③ 验证与取证（要能复现）
  { id: "qp-7", text: "这条漏洞给我完整可复现的请求和响应片段，不要只写结论" },
  { id: "qp-8", text: "做一次对照：去掉 payload 再发一遍，证明差异是 payload 造成的" },
  { id: "qp-9", text: "把这条漏洞的影响写清楚：能读到什么数据、能做什么操作、能不能扩大" },
  // ④ 往上推（漏洞 → 权限）
  { id: "qp-10", text: "别停在报漏洞。把这条洞往上推一步：能不能进后台、能不能执行命令、能不能读到数据" },
  { id: "qp-11", text: "用这条注入把数据库里的表名列出来，只取 3 条样本证明能读到，不要整表导出" },
  { id: "qp-12", text: "从后台找上传或模板入口，落一个最小验证文件证明能执行命令，验证完立刻删掉" },
  { id: "qp-13", text: "拿到 shell 后用 owned_register 登记这台机器的权限级别，然后查 sudo -l 和 SUID" },
  { id: "qp-14", text: "现在是什么权限级别？要再往上一步需要什么条件，直接说" },
  // ⑤ 授权与边界（它会来问你）
  { id: "qp-15", text: "允许上传最小验证文件，落地后立即删除并留证" },
  { id: "qp-16", text: "允许读取数据样本，每类最多 3 条，不要整表导出" },
  { id: "qp-17", text: "这一步先不要做，把风险和收益讲清楚，我再决定" },
  { id: "qp-18", text: "不要碰数据，只证明存在，停下来写结论" },
  // ⑥ 纠偏（跑偏/原地打转）
  { id: "qp-19", text: "你已经在同一个点上重复了，换个手法或换条路，20 分钟内没进展就收" },
  { id: "qp-20", text: "别再扫目录了，换到接口面和登录面" },
  { id: "qp-21", text: "先把已有证据写完再继续，不要一边挖一边丢" },
  { id: "qp-22", text: "现在进展到哪一步了？用大白话说，不要贴日志" },
  { id: "qp-23", text: "这条方向还要继续吗？还差什么？给我一个明确的判断" },
  // ⑦ 收口与交付
  { id: "qp-24", text: "停手，按现有证据写一份能直接交给客户的报告" },
  { id: "qp-25", text: "把结论压成三句话：值不值得打、拿到了什么、下一步建议" },
  { id: "qp-26", text: "把这次的成功手法和死路都写成经验，下次同类目标直接复用" },
  // ⑧ 排障（它卡住时）
  { id: "qp-27", text: "你为什么停下来了？是模型/网络/工具哪一环的问题，说清楚并给替代方案" },
  { id: "qp-28", text: "目标连不上就先分清是它挂了还是我们出网有问题，别急着重试" },
  { id: "qp-29", text: "缺什么工具直接告诉我安装命令，别绕" },
  // ⑨ 学习（零基础用）
  { id: "qp-30", text: "用零基础能懂的话解释：这条洞为什么成立，别人一般怎么利用" },
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
