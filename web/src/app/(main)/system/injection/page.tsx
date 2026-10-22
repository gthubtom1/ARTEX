"use client";

import * as React from "react";

import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { api } from "@/lib/api";

// 注入层 / 人设 页。
//
// 三件事：
//   1) 开关（inject.mode）+ 注入方式（cached / every-turn）；
//   2) 人设正文（inject.custom）——原样注入系统提示，不截断，但后端有 8000 字符上限；
//   3) 内置人设（inject.preset）——Custom 为空时用随二进制发布的默认人设，开箱即用。
//
// 注意：本页不再强制把 mode 写成 hybrid|off。之前那样会让系统设置页选的
// always/on-miss/on-refusal 被静默改回 hybrid。

const MAX_CUSTOM = 200000;

function estimateTokens(text: string): number {
  let cjk = 0;
  let other = 0;
  for (const ch of text) {
    if (ch.codePointAt(0)! >= 0x2e80) cjk++;
    else other++;
  }
  return cjk + Math.ceil(other / 4);
}

type Payload = Record<string, unknown>;

export default function InjectionPage() {
  const [mode, setMode] = React.useState("hybrid");
  const [custom, setCustom] = React.useState("");
  const [personaMode, setPersonaMode] = React.useState<"cached" | "every-turn">("cached");
  const [preset, setPreset] = React.useState<"on" | "off">("on");
  const [maxChars, setMaxChars] = React.useState("900");
  const [maxRetries, setMaxRetries] = React.useState("2");
  const [presetText, setPresetText] = React.useState("");
  const [presetName, setPresetName] = React.useState("内置人设");
  const [loaded, setLoaded] = React.useState(false);
  const [saving, setSaving] = React.useState(false);

  const apply = React.useCallback((s: Payload) => {
    setMode(String(s.inject_mode ?? "hybrid"));
    setCustom(String(s.inject_custom ?? ""));
    setPersonaMode((s.inject_persona_mode as "cached" | "every-turn") ?? "cached");
    setPreset((s.inject_preset as "on" | "off") ?? "on");
    setMaxChars(String(s.inject_max_chars ?? 900));
    setMaxRetries(String(s.inject_max_retries ?? 2));
    setPresetText(String(s.inject_default_persona ?? ""));
    setPresetName(String(s.inject_default_persona_name ?? "内置人设"));
  }, []);

  React.useEffect(() => {
    api
      .settings()
      .then((s) => {
        apply(s as unknown as Payload);
        setLoaded(true);
      })
      .catch((e) => toast.error(`读取配置失败：${(e as Error).message}`));
  }, [apply]);

  const enabled = mode !== "off";

  async function save() {
    setSaving(true);
    try {
      const s = await api.setSettings({
        inject_mode: enabled ? (mode === "off" ? "hybrid" : mode) : "off",
        inject_custom: custom,
        inject_persona_mode: personaMode,
        inject_preset: preset,
        inject_max_chars: Math.min(4000, Math.max(200, Number(maxChars) || 900)),
        inject_max_retries: Math.min(5, Math.max(0, Number(maxRetries) || 0)),
      } as never);
      apply(s as unknown as Payload);
      toast.success("已保存（下一轮模型请求即生效，不用重启任务）");
    } catch (e) {
      toast.error(`保存失败：${(e as Error).message}`);
    } finally {
      setSaving(false);
    }
  }

  const tooLong = custom.length > MAX_CUSTOM;
  const tokens = estimateTokens(custom);

  return (
    <div className="mx-auto flex w-full max-w-3xl flex-col gap-4 p-4 md:gap-6">
      <div>
        <h1 className="text-lg font-semibold">注入层 / 人设</h1>
        <p className="text-sm text-muted-foreground">
          这里是「破甲词 / 人设」的输入处，也是拒答重定向的开关。人设正文原样拼进系统提示；
          模型出现拒答时自动补一条技术重定向（目标 → 方法 → 下一步）。
        </p>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">启用</CardTitle>
          <CardDescription>关闭后既不注入人设，也不做拒答重定向。</CardDescription>
        </CardHeader>
        <CardContent>
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={enabled}
              disabled={!loaded}
              onChange={(e) => setMode(e.target.checked ? (mode === "off" ? "hybrid" : mode) : "off")}
            />
            <span>{enabled ? `已启用（${mode}）` : "已关闭（off）"}</span>
          </label>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">注入方式</CardTitle>
          <CardDescription>
            默认「缺失时补」，上下文里没有这份人设时才重新注入。
            <br />
            注意：planner / worker 的系统提示每轮是重拼的，所以在这两个角色上「缺失时补」与「每轮注入」实际等价；
            真正省 token 的办法是缩短人设，而不是切换方式。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <label className="flex cursor-pointer items-start gap-2 text-sm">
            <input
              type="radio"
              className="mt-1"
              checked={personaMode === "cached"}
              disabled={!loaded}
              onChange={() => setPersonaMode("cached")}
            />
            <span>
              <b>缺失时补（推荐）</b>
              <span className="ml-2 text-muted-foreground">上下文里还有人设就不重复发；被压缩/丢了才补一次。</span>
            </span>
          </label>
          <label className="flex cursor-pointer items-start gap-2 text-sm">
            <input
              type="radio"
              className="mt-1"
              checked={personaMode === "every-turn"}
              disabled={!loaded}
              onChange={() => setPersonaMode("every-turn")}
            />
            <span>
              <b>每轮注入</b>
              <span className="ml-2 text-muted-foreground">每轮都发一次完整人设，最贵，只在模型确实会忘时用。</span>
            </span>
          </label>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">内置人设（开箱即用）</CardTitle>
          <CardDescription>
            下面这段是随程序发布的默认人设。开启后，只要人设正文留空就用它 —— 新装用户不用去别处粘贴。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-2">
          <label className="flex cursor-pointer items-center gap-2 text-sm">
            <input
              type="checkbox"
              checked={preset === "on"}
              disabled={!loaded}
              onChange={(e) => setPreset(e.target.checked ? "on" : "off")}
            />
            <span>
              {preset === "on" ? `已启用（${presetName}）` : "已关闭（人设正文为空时不注入任何人设）"}
            </span>
          </label>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={!loaded || !presetText}
              onClick={() => {
                setCustom(presetText);
                toast.success("已填入下方人设正文，记得点保存");
              }}
            >
              复制内置人设到正文
            </Button>
            <span className="text-xs text-muted-foreground">
              {presetText ? `≈ ${estimateTokens(presetText).toLocaleString()} tokens / 次注入` : ""}
            </span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">破甲词 / 人设（原文）</CardTitle>
          <CardDescription>
            粘贴完整文本即可。留空且「内置人设」开启时使用内置版本。人设是<b>原样注入、不截断</b>的；只有当长度超过 {MAX_CUSTOM.toLocaleString()} 字符（约 200KB）才会被拦，用来防"贴错文件"。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <textarea
            className="min-h-[360px] w-full rounded-md border bg-background p-2 font-mono text-xs leading-relaxed"
            placeholder="把完整的破甲词 / 人设 / 执行约定粘到这里……（留空则使用内置人设）"
            value={custom}
            disabled={!loaded}
            onChange={(e) => setCustom(e.target.value)}
          />
          <div className="mt-1 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
            <span className={tooLong ? "text-red-600 dark:text-red-400" : ""}>
              已输入 {custom.length} / {MAX_CUSTOM} 字符
            </span>
            <span>≈ {tokens.toLocaleString()} tokens / 次请求</span>
            {custom.trim() === "" && preset === "on" && (
              <span>当前生效的是内置人设（{presetName}）</span>
            )}
          </div>
          {tooLong && (
            <div className="mt-2 rounded-md border border-red-500/40 bg-red-500/5 p-2 text-xs text-red-700 dark:text-red-400">
              超过后端上限，保存会被拒绝。请精简到 {MAX_CUSTOM} 字符以内。
            </div>
          )}
          {!tooLong && tokens > 12000 && (
            <div className="mt-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-2 text-xs text-amber-700 dark:text-amber-400">
              人设偏长：它会随系统提示一起发送。建议精简到 1 万 tokens 以内，
              或保持内容稳定以命中提供方的提示缓存（缓存命中时按缓存价计费）。
            </div>
          )}
        </CardContent>
      </Card>

      <div className="flex items-center gap-2">
        <Button onClick={save} disabled={!loaded || saving || tooLong}>
          {saving ? "保存中…" : "保存"}
        </Button>
        <span className="text-xs text-muted-foreground">
          {enabled ? mode : "off"} · {personaMode} · {custom.trim() === "" && preset === "on" ? "内置人设" : `${custom.length} 字符`}
        </span>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">高级（一般不用动）</CardTitle>
          <CardDescription>
            这两个只影响「执行上下文块」和「拒答重定向」，不影响上面的人设正文。
            <b>注：本页是注入层的唯一入口</b>——系统设置页里已经不再重复放这些项，避免两处互相覆盖。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap items-center gap-3">
          <label className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">自动上下文块上限（人设不受此限）</span>
            <input
              type="number"
              min={200}
              max={4000}
              className="h-8 w-24 rounded-md border bg-background px-2 text-sm"
              value={maxChars}
              disabled={!loaded}
              onChange={(e) => setMaxChars(e.target.value)}
            />
          </label>
          <label className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">单次 run 最多重定向</span>
            <input
              type="number"
              min={0}
              max={5}
              className="h-8 w-20 rounded-md border bg-background px-2 text-sm"
              value={maxRetries}
              disabled={!loaded}
              onChange={(e) => setMaxRetries(e.target.value)}
            />
          </label>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">它怎么工作</CardTitle>
        </CardHeader>
        <CardContent className="space-y-1 text-sm text-muted-foreground">
          <p>1. 请求模型前，把这段原文作为额外的一段系统提示追加进去（planner / worker / 对话页都覆盖）。</p>
          <p>2. 模型出现拒答时，额外注入一条技术重定向（目标→方法→下一步），次数受「拒答重定向次数」限制。</p>
          <p>3. 不写「禁止」类文案。改完保存后对正在跑的任务下一轮就生效，不需要重启。</p>
        </CardContent>
      </Card>
    </div>
  );
}
