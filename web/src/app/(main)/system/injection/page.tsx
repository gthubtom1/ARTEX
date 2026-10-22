"use client";

import * as React from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";

// 与 pi-web 的「人设」一致：一整段文本原样注入系统提示，每轮生效，不截断、不设字数上限。
// 不暴露“重定向预算/最大字符”这类内部参数——那是自动上下文块的事，用户不需要管。
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
          <div className="mt-1 text-xs text-muted-foreground">
            已输入 {custom.length} 字符（不设上限）
          </div>
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
