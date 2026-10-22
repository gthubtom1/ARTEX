"use client";

import * as React from "react";

import { useRouter } from "next/navigation";
import { AlertTriangleIcon, ArrowRightIcon, CheckCircle2Icon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { p8api } from "@/lib/api";

// P8-4：一条链入口 —— 贴一个网址就开跑。
//
// 零基础用户原本要过六道闸（PG / 密码 / 模型 / 代理 / 描述 / goal）。这里把它们压成
// 一个输入框 + 三个档位：出口自检 → 自动生成 goal → 直接开跑 → 跳到能力菜单。
//
// 两条纪律：
//   1. 出口不通**不报错**：任务照建，只在卡片上显示⚠️并给三个一键选项。用户点了"开始"
//      时甩红字等于劝退。
//   2. 档位不是权限，是"打多深"的默认值：lab=只读侦察、legacy=指纹后最小验证、
//      public=只做低风险只读面。文案里不出现"禁止/不允许"。

type Tier = "lab" | "legacy" | "public";

const TIERS: { id: Tier; label: string; hint: string }[] = [
  { id: "lab", label: "本地靶场", hint: "本机/内网，直连，先只读侦察出能力菜单" },
  { id: "legacy", label: "已知漏洞老站", hint: "有已知漏洞族，指纹后做最小验证" },
  { id: "public", label: "公网站点", hint: "只做被动侦察与低风险只读面" },
];

export function QuickStartCard() {
  const router = useRouter();
  const [url, setUrl] = React.useState("");
  const [tier, setTier] = React.useState<Tier>("lab");
  const [minutes, setMinutes] = React.useState(30);
  const [busy, setBusy] = React.useState(false);

  const [egress, setEgress] = React.useState<{ ok: boolean; detail?: string; exit_ip?: string } | null>(null);
  const [checking, setChecking] = React.useState(true);

  const checkEgress = React.useCallback(async () => {
    setChecking(true);
    try {
      const r = await p8api.egressCheck();
      setEgress(r);
    } catch (e) {
      setEgress({ ok: false, detail: (e as Error).message });
    } finally {
      setChecking(false);
    }
  }, []);

  React.useEffect(() => {
    void checkEgress();
  }, [checkEgress]);

  async function start() {
    const trimmed = url.trim();
    if (!trimmed) {
      toast.error("先贴一个网址");
      return;
    }
    setBusy(true);
    try {
      const res = await p8api.quickstart({ url: trimmed, tier, timeout_min: minutes });
      for (const w of res.warnings ?? []) toast.error(w, { duration: 8000 });
      toast.success(`任务已创建：${res.host}（${res.tier}）`);
      router.push(res.redirect || `/function/capabilities?task=${res.task_id}`);
    } catch (e) {
      toast.error(`建任务失败：${(e as Error).message}`);
      setBusy(false);
    }
  }

  return (
    <Card className="border-primary/20 bg-gradient-to-br from-primary/5 to-transparent">
      <CardContent className="flex flex-col gap-3 pt-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <div className="text-sm font-semibold">贴一个网址，直接开跑</div>
            <div className="text-[11px] text-muted-foreground">
              自动生成任务目标、自动登记测试范围、直接进侦察轮；出结果后去「能力菜单」看它能做什么。
            </div>
          </div>
          <div className="flex items-center gap-2 text-[11px]">
            {checking ? (
              <span className="flex items-center gap-1 text-muted-foreground">
                <Loader2Icon className="size-3 animate-spin" /> 出口检测中
              </span>
            ) : egress?.ok ? (
              <span className="flex items-center gap-1 text-emerald-600 dark:text-emerald-400">
                <CheckCircle2Icon className="size-3" /> 出口可用
                {egress.exit_ip ? <span className="font-mono">{egress.exit_ip}</span> : null}
              </span>
            ) : (
              <span className="flex items-center gap-1 text-amber-600 dark:text-amber-400">
                <AlertTriangleIcon className="size-3" /> 出口未就绪
              </span>
            )}
            <Button variant="ghost" size="sm" className="h-6 px-1.5" onClick={() => void checkEgress()} title="重新检测出口">
              <RefreshCwIcon className="size-3" />
            </Button>
          </div>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row">
          <Input
            value={url}
            onChange={(e) => setUrl(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !busy) void start();
            }}
            placeholder="http://127.0.0.1:8080/  或  https://example.com/"
            className="flex-1 font-mono text-xs"
            disabled={busy}
          />
          <Button onClick={() => void start()} disabled={busy || !url.trim()} className="shrink-0">
            {busy ? <Loader2Icon className="mr-1 size-3.5 animate-spin" /> : <ArrowRightIcon className="mr-1 size-3.5" />}
            开始
          </Button>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          {TIERS.map((t) => (
            <button
              key={t.id}
              type="button"
              title={t.hint}
              onClick={() => setTier(t.id)}
              disabled={busy}
              className={
                "rounded-full border px-2.5 py-1 text-[11px] transition " +
                (tier === t.id
                  ? "border-primary bg-primary/10 font-medium text-foreground"
                  : "border-border text-muted-foreground hover:bg-muted/40")
              }
            >
              {t.label}
            </button>
          ))}
          <span className="text-[11px] text-muted-foreground">超时</span>
          <select
            value={minutes}
            onChange={(e) => setMinutes(Number(e.target.value))}
            disabled={busy}
            className="h-6 rounded-md border bg-background px-1.5 text-[11px]"
          >
            {[15, 30, 60, 120].map((m) => (
              <option key={m} value={m}>
                {m} 分钟
              </option>
            ))}
          </select>
          <span className="text-[11px] text-muted-foreground">
            {TIERS.find((t) => t.id === tier)?.hint}
          </span>
        </div>

        {egress && !egress.ok && (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 px-2.5 py-2 text-[11px] text-amber-700 dark:text-amber-400">
            <AlertTriangleIcon className="size-3.5 shrink-0" />
            <span className="min-w-0 flex-1 break-all">
              出口未就绪（{egress.detail || "代理连不上"}）。任务仍可创建，但发不出包。
            </span>
            <Button variant="outline" size="sm" className="h-6 px-2 text-[11px]" onClick={() => router.push("/system/network")}>
              改代理
            </Button>
            <Button variant="outline" size="sm" className="h-6 px-2 text-[11px]" onClick={() => void checkEgress()}>
              重试
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
