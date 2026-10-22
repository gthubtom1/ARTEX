"use client";

import * as React from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";

// 与 pi-web 的「人设」一致：一整段文本原样注入系统提示，每轮生效，不截断、不设字数上限。
// 不暴露“重定向预算/最大字符”这类内部参数——那是自动上下文块的事，用户不需要管。
function estimateTokens(text: string): number {
  let cjk = 0;
  let other = 0;
  for (const ch of text) {
    if (ch.codePointAt(0)! >= 0x2e80) cjk++;
    else other++;
  }
  return cjk + Math.ceil(other / 4);
}

export default function InjectionPage() {
  const [enabled, setEnabled] = React.useState(true);
  const [custom, setCustom] = React.useState("");
  const [loaded, setLoaded] = React.useState(false);
  const [saving, setSaving] = React.useState(false);

  const apply = React.useCallback((s: Record<string, unknown>) => {
    setEnabled(((s.inject_mode as string) ?? "hybrid") !== "off");
    setCustom(String(s.inject_custom ?? ""));
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
    setSaving(true);
    try {
      const s = await api.setSettings({
        inject_mode: enabled ? "hybrid" : "off",
        inject_custom: custom,
      } as never);
      apply(s as unknown as Record<string, unknown>);
      toast.success("已保存（对之后启动的 run 生效）");
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4 md:gap-6">
      <div>
        <h1 className="text-lg font-semibold">注入层 / 人设</h1>
        <p className="text-sm text-muted-foreground">
          这里就是“破甲词 / 人设”的输入处。和 pi-web 的人设一样：整段原文注入系统提示，每轮生效，不截断、不限制字数。
        </p>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">启用</CardTitle>
          <CardDescription>关闭后不注入人设，也不做拒答重定向。</CardDescription>
        </CardHeader>
        <CardContent>
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              disabled={!loaded}
              onChange={(e) => setEnabled(e.target.checked)}
            />
            <span>{enabled ? "已启用（hybrid）" : "已关闭（off）"}</span>
          </label>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">破甲词 / 人设（原文）</CardTitle>
          <CardDescription>
            粘贴完整文本即可，几十 KB 也可以。它会被原样拼进每轮系统提示；保存后对之后启动的 run 生效。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <textarea
            className="min-h-[360px] w-full rounded-md border bg-background p-2 font-mono text-xs leading-relaxed"
            placeholder="把完整的破甲词 / 人设 / 执行约定粘到这里……"
            value={custom}
            disabled={!loaded}
            onChange={(e) => setCustom(e.target.value)}
          />
          <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
            <span>已输入 {custom.length} 字符（不设上限）</span>
            <span>≈ {estimateTokens(custom).toLocaleString()} tokens / 次请求</span>
          </div>
          {estimateTokens(custom) > 12000 && (
            <div className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-2 text-xs text-amber-700 dark:text-amber-400">
              人设偏长：它会在每次模型请求里随系统提示一起发送。建议精简到 1 万 tokens 以内，
              或保持内容不变以命中提供方的提示缓存（缓存命中时按缓存价计费）。
            </div>
          )}
        </CardContent>
      </Card>

      <div className="flex items-center gap-2">
        <Button onClick={save} disabled={!loaded || saving}>
          {saving ? "保存中…" : "保存"}
        </Button>
        <span className="text-xs text-muted-foreground">
          {enabled ? "每轮注入" : "已关闭"} · {custom.length} 字符
        </span>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">省 token 的方式（和 pi-web 一致）</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm text-muted-foreground">
          <p>1. 人设不是每轮额外复制一遍：它只在系统提示里出现一次，同一任务内保持稳定。</p>
          <p>2. 稳定的系统提示会被模型提供方缓存（Anthropic / DeepSeek 等），缓存命中的部分按更低价格计费。</p>
          <p>3. 不要在任务进行中反复改人设——每次修改都会让缓存失效，下一轮按全价重算。</p>
          <p>4. 越长越贵：建议控制在 1 万 tokens 以内；需要超长设定时，把“每轮都要生效的规则”留在人设里，参考资料放知识库按需检索。</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">它怎么工作</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm text-muted-foreground">
          <p>1. 每轮请求前把这段原文拼进系统提示（与 pi-web 人设相同的方式）。</p>
          <p>2. 模型出现拒答时，额外注入一条技术重定向（目标→方法→下一步），最多 2 次，不需要你配置。</p>
          <p>3. 不写“禁止”文案，不做字数截断；你写多少就注入多少。</p>
        </CardContent>
      </Card>
    </div>
  );
}
