"use client";

import * as React from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";

type Mode = "off" | "always" | "on-miss" | "on-refusal" | "hybrid";

const MODES: { value: Mode; label: string; desc: string }[] = [
  { value: "hybrid", label: "hybrid（默认）", desc: "每轮带执行上下文 + 拒答时自动重定向" },
  { value: "on-miss", label: "on-miss", desc: "每次请求前检查上下文指纹，丢了就补" },
  { value: "on-refusal", label: "on-refusal", desc: "只在模型拒答时重定向" },
  { value: "always", label: "always", desc: "每轮强制带执行上下文" },
  { value: "off", label: "off", desc: "关闭注入层" },
];

export default function InjectionPage() {
  const [mode, setMode] = React.useState<Mode>("hybrid");
  const [maxChars, setMaxChars] = React.useState("900");
  const [maxRetries, setMaxRetries] = React.useState("2");
  const [loaded, setLoaded] = React.useState(false);
  const [saving, setSaving] = React.useState(false);

  const apply = React.useCallback((s: Record<string, unknown>) => {
    setMode(((s.inject_mode as Mode) ?? "hybrid") as Mode);
    setMaxChars(String(s.inject_max_chars ?? 900));
    setMaxRetries(String(s.inject_max_retries ?? 2));
  }, []);

  React.useEffect(() => {
    api
      .settings()
      .then((s) => {
        apply(s as unknown as Record<string, unknown>);
        setLoaded(true);
      })
      .catch((e) => toast.error(`读取配置失败：${(e as Error).message}`));
  }, [apply]);

  async function save() {
    const chars = Number(maxChars);
    const retries = Number(maxRetries);
    if (!Number.isInteger(chars) || chars < 200 || chars > 4000) {
      toast.error("最大字符需要在 200–4000 之间");
      return;
    }
    if (!Number.isInteger(retries) || retries < 0 || retries > 5) {
      toast.error("最多重定向需要在 0–5 之间");
      return;
    }
    setSaving(true);
    try {
      const s = await api.setSettings({
        inject_mode: mode,
        inject_max_chars: chars,
        inject_max_retries: retries,
      } as never);
      apply(s as unknown as Record<string, unknown>);
      toast.success("注入层配置已保存（下一次 run 生效）");
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4 md:gap-6">
      <div>
        <h1 className="text-lg font-semibold">注入层（防拒答）</h1>
        <p className="text-sm text-muted-foreground">
          控制执行上下文与拒答重定向。它是后台自动运行的；这里决定它何时注入、注入多长、最多重定向几次。
        </p>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">工作模式</CardTitle>
          <CardDescription>保存后对之后启动的 run 生效；正在跑的任务不受影响。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          {MODES.map((m) => (
            <label
              key={m.value}
              className="flex cursor-pointer items-start gap-2 rounded-md border p-2 text-sm hover:bg-muted/40"
            >
              <input
                type="radio"
                name="inject-mode"
                className="mt-1"
                checked={mode === m.value}
                disabled={!loaded}
                onChange={() => setMode(m.value)}
              />
              <span>
                <span className="font-medium">{m.label}</span>
                <span className="ml-2 text-muted-foreground">{m.desc}</span>
              </span>
            </label>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">预算</CardTitle>
          <CardDescription>防止注入把上下文撑爆或陷入“拒答→重试”循环。</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 sm:grid-cols-2">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">单次最大字符（200–4000）</span>
            <input
              className="h-8 rounded-md border bg-background px-2"
              value={maxChars}
              disabled={!loaded}
              onChange={(e) => setMaxChars(e.target.value)}
            />
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-muted-foreground">单次 run 最多重定向（0–5）</span>
            <input
              className="h-8 rounded-md border bg-background px-2"
              value={maxRetries}
              disabled={!loaded}
              onChange={(e) => setMaxRetries(e.target.value)}
            />
          </label>
        </CardContent>
      </Card>

      <div className="flex items-center gap-2">
        <Button onClick={save} disabled={!loaded || saving}>
          {saving ? "保存中…" : "保存配置"}
        </Button>
        <span className="text-xs text-muted-foreground">
          当前：{mode} · {maxChars} 字符 · 最多 {maxRetries} 次
        </span>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">它实际做了什么</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm text-muted-foreground">
          <p>1. 每轮请求前检查上下文里有没有注入指纹；缺失就补一段“执行上下文”（hybrid/on-miss/always）。</p>
          <p>2. 模型输出像拒答（“我不能/无法/抱歉…”）且没有工具调用时，注入一条技术重定向：目标→方法→下一步。</p>
          <p>3. 最多重定向 N 次（这里配置），避免死循环；不写“禁止”文案，只做正向重述。</p>
        </CardContent>
      </Card>
    </div>
  );
}
