"use client";

import * as React from "react";

import Link from "next/link";
import { useRouter } from "next/navigation";

import {
  ArrowRightIcon,
  CpuIcon,
  DatabaseIcon,
  KeyRoundIcon,
  RadioTowerIcon,
  RefreshCwIcon,
  ShieldAlertIcon,
  ShieldCheckIcon,
} from "lucide-react";
import { toast } from "sonner";

import { OnboardStep } from "@/components/onboard-step";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { api, p2api, p8api, type EgressCheck, type TierSuggestResponse } from "@/lib/api";
import type { Settings } from "@/lib/types";
import { cn } from "@/lib/utils";

// 后端 /api/settings 会回显凭据主密钥指纹，但 Settings 类型里还没声明它
// （本页不改 lib 文件，只在本地扩展）。
type SettingsWithFingerprint = Settings & { cred_key_fingerprint?: string };

type TierRole = "planner" | "worker" | "verifier";

const STEPS: { key: string; label: string }[] = [
  { key: "db", label: "数据库" },
  { key: "model", label: "模型" },
  { key: "egress", label: "出口" },
  { key: "cred", label: "凭据" },
];

const TIER_ROLES: { key: TierRole; label: string }[] = [
  { key: "planner", label: "规划者（planner）" },
  { key: "worker", label: "执行者（worker）" },
  { key: "verifier", label: "校验者（verifier）" },
];

// tier 徽章配色：high=绿 / mid=蓝 / low=灰。
function tierBadgeClass(tier: string): string {
  switch (tier) {
    case "high":
      return "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400";
    case "mid":
      return "border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-400";
    default:
      return "border-border bg-muted text-muted-foreground";
  }
}

function errText(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export default function SystemOnboardPage() {
  const router = useRouter();

  // 步骤状态只放本地：0=数据库 1=模型 2=出口 3=凭据。
  const [step, setStep] = React.useState(0);
  const [done, setDone] = React.useState<boolean[]>(() => STEPS.map(() => false));

  // 步 1 · 数据库
  const [dbChecking, setDbChecking] = React.useState(false);

  // 步 2 · 模型
  const [tier, setTier] = React.useState<TierSuggestResponse | null>(null);
  const [tierLoading, setTierLoading] = React.useState(false);
  const [tierErr, setTierErr] = React.useState("");
  const [adopting, setAdopting] = React.useState(false);
  const [savingRole, setSavingRole] = React.useState<TierRole | null>(null);

  // 步 3 · 出口
  const [egress, setEgress] = React.useState<EgressCheck | null>(null);
  const [egressLoading, setEgressLoading] = React.useState(false);
  const [egressErr, setEgressErr] = React.useState("");
  const [showDirectHint, setShowDirectHint] = React.useState(false);

  // 步 4 · 凭据
  const [settings, setSettings] = React.useState<SettingsWithFingerprint | null>(null);
  const [settingsLoading, setSettingsLoading] = React.useState(false);
  const [settingsErr, setSettingsErr] = React.useState("");

  const markDone = React.useCallback((index: number) => {
    setDone((prev) => prev.map((v, i) => (i === index ? true : v)));
  }, []);

  const next = () => {
    markDone(step);
    setStep((s) => Math.min(s + 1, STEPS.length - 1));
  };

  // ---- 步 1：数据库可用性 ----
  const checkDb = React.useCallback(async () => {
    setDbChecking(true);
    try {
      await p2api.owned();
      toast.success("数据库连接正常");
    } catch (e) {
      toast.error("数据库连接失败：" + errText(e));
    } finally {
      setDbChecking(false);
    }
  }, []);

  // ---- 步 2：模型分层 ----
  const loadTier = React.useCallback(async (notify = false) => {
    setTierLoading(true);
    setTierErr("");
    try {
      const r = await p8api.tierSuggest();
      setTier(r);
      if (notify) toast.success("模型建议已刷新");
    } catch (e) {
      const msg = errText(e);
      setTierErr(msg);
      toast.error("读取模型建议失败：" + msg);
    } finally {
      setTierLoading(false);
    }
  }, []);

  const adoptSuggestion = async () => {
    if (!tier) return;
    const s = tier.suggest;
    setAdopting(true);
    try {
      await p8api.putTier({
        planner: s.planner ?? 0,
        worker: s.worker ?? 0,
        verifier: s.verifier ?? 0,
        adviser: s.adviser ?? 0,
      });
      toast.success("已采用建议的分层配置");
      await loadTier();
    } catch (e) {
      toast.error("保存分层失败：" + errText(e));
    } finally {
      setAdopting(false);
    }
  };

  const saveTier = async (role: TierRole, id: number) => {
    setSavingRole(role);
    try {
      const body = role === "planner" ? { planner: id } : role === "worker" ? { worker: id } : { verifier: id };
      const r = await p8api.putTier(body);
      setTier((prev) => (prev ? { ...prev, current: r.current } : prev));
      toast.success("已更新分层配置");
    } catch (e) {
      toast.error("保存分层失败：" + errText(e));
    } finally {
      setSavingRole(null);
    }
  };

  // ---- 步 3：出口预检 ----
  const runEgress = React.useCallback(async (notify = false) => {
    setEgressLoading(true);
    setEgressErr("");
    try {
      const r = await p8api.egressCheck();
      setEgress(r);
      if (notify) {
        if (r.ok) toast.success("出口可用");
        else toast.error("出口未就绪：" + (r.detail || "未知原因"));
      }
    } catch (e) {
      const msg = errText(e);
      setEgressErr(msg);
      toast.error("出口检查失败：" + msg);
    } finally {
      setEgressLoading(false);
    }
  }, []);

  // 进入本步时自动预检一次。
  React.useEffect(() => {
    if (step === 2) void runEgress();
  }, [step, runEgress]);

  // ---- 步 4：凭据 ----
  const loadSettings = React.useCallback(async () => {
    setSettingsLoading(true);
    setSettingsErr("");
    try {
      const s = await api.settings();
      setSettings(s);
    } catch (e) {
      const msg = errText(e);
      setSettingsErr(msg);
      toast.error("读取配置失败：" + msg);
    } finally {
      setSettingsLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void loadTier();
    void loadSettings();
  }, [loadTier, loadSettings]);

  const fingerprint = settings?.cred_key_fingerprint?.trim() ?? "";

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">首次配置</h1>
        <p className="text-muted-foreground text-sm">四步跑起来。每步都能跳过，跳过会写清后果。</p>
      </div>

      {/* 横向步骤条：当前步高亮，已完成的打勾。 */}
      <ol className="flex flex-wrap items-center gap-x-2 gap-y-2 text-sm">
        {STEPS.map((s, i) => (
          <li key={s.key} className="flex items-center gap-2">
            <button
              type="button"
              onClick={() => setStep(i)}
              aria-current={step === i ? "step" : undefined}
              className={cn(
                "flex items-center gap-2 rounded-full border px-3 py-1 transition-colors",
                step === i
                  ? "border-primary bg-primary/10 text-foreground"
                  : "border-border text-muted-foreground hover:bg-muted/60",
              )}
            >
              <span
                className={cn(
                  "flex size-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold",
                  done[i]
                    ? "border-emerald-500/50 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                    : step === i
                      ? "border-primary bg-primary text-primary-foreground"
                      : "border-border text-muted-foreground",
                )}
              >
                {done[i] ? <ShieldCheckIcon className="size-3" /> : i + 1}
              </span>
              {s.label}
            </button>
            {i < STEPS.length - 1 ? <span className="text-muted-foreground/40">—</span> : null}
          </li>
        ))}
      </ol>

      {/* ── 步 1 · 数据库 ── */}
      {step === 0 ? (
        <OnboardStep
          index={1}
          title="数据库"
          hint="后端不回报 DSN，这里只确认连通性。"
          active={step === 0}
          done={done[0]}
          footer={
            <>
              <Button variant="outline" onClick={() => void checkDb()} disabled={dbChecking}>
                <RefreshCwIcon className={cn("size-4", dbChecking && "animate-spin")} />
                重试
              </Button>
              <Button onClick={next}>
                下一步
                <ArrowRightIcon className="size-4" />
              </Button>
            </>
          }
        >
          <div className="flex items-start gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/5 p-3">
            <DatabaseIcon className="mt-0.5 size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
            <p className="text-sm">能进到这个页面，说明数据库已连通。</p>
          </div>
          <p className="text-muted-foreground text-xs">
            后端不向前端回显数据库连接串（DSN），所以这里不提供填写入口。点「重试」会调用一个只读接口
            （<code className="font-mono">/api/owned</code>）做一次实际查询；失败时会提示具体错误。
          </p>
        </OnboardStep>
      ) : null}

      {/* ── 步 2 · 模型 ── */}
      {step === 1 ? (
        <OnboardStep
          index={2}
          title="模型"
          hint="给不同角色分配不同档位的模型；跳过则全部跟随全局激活配置。"
          active={step === 1}
          done={done[1]}
          footer={
            <>
              <Button variant="outline" onClick={() => setStep(0)}>
                上一步
              </Button>
              <Button onClick={next}>
                下一步
                <ArrowRightIcon className="size-4" />
              </Button>
            </>
          }
        >
          {tierLoading && !tier ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-5 w-2/3" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-9 w-40" />
            </div>
          ) : tierErr && !tier ? (
            <div className="flex items-center justify-between gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3">
              <p className="text-sm">读取模型建议失败：{tierErr}</p>
              <Button variant="outline" size="sm" onClick={() => void loadTier()}>
                重试
              </Button>
            </div>
          ) : tier ? (
            <>
              <div className="flex flex-wrap items-center justify-between gap-2">
                <p className="text-sm font-medium">建议分层</p>
                <div className="flex items-center gap-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => void loadTier(true)}
                    disabled={tierLoading}
                  >
                    <RefreshCwIcon className={cn("size-4", tierLoading && "animate-spin")} />
                    刷新
                  </Button>
                  <Button size="sm" onClick={() => void adoptSuggestion()} disabled={adopting || tier.profiles.length === 0}>
                    {adopting ? "采用中…" : "一键采用建议"}
                  </Button>
                </div>
              </div>

              <p className="text-muted-foreground rounded-md border bg-muted/30 p-3 text-xs leading-relaxed">
                {tier.reason || "后端没有给出理由。"}
              </p>

              {tier.profiles.length === 0 ? (
                <div className="flex flex-col items-start gap-2 rounded-md border border-dashed p-4">
                  <p className="text-sm font-medium">还没有模型配置</p>
                  <p className="text-muted-foreground text-xs">
                    没有可用的模型配置，分层建议为空；跳过本步后，所有角色都跟随全局激活配置（此时也没有可用配置）。
                  </p>
                  <Button variant="outline" size="sm" asChild>
                    <Link href="/system/llm">
                      <CpuIcon className="size-4" />
                      先去配一个模型配置
                    </Link>
                  </Button>
                </div>
              ) : (
                <>
                  <ul className="divide-y rounded-md border">
                    {tier.profiles.map((p) => (
                      <li key={p.id} className="flex flex-wrap items-center gap-2 px-3 py-2">
                        <span className="text-sm font-medium">{p.name}</span>
                        <span className="text-muted-foreground font-mono text-xs">{p.model}</span>
                        <Badge variant="outline" className={cn("ml-auto", tierBadgeClass(p.tier))}>
                          {p.tier}
                        </Badge>
                        {p.note ? <span className="text-muted-foreground w-full text-xs">{p.note}</span> : null}
                      </li>
                    ))}
                  </ul>

                  <div className="flex flex-col gap-2">
                    {TIER_ROLES.map((role) => (
                      <div key={role.key} className="flex flex-wrap items-center justify-between gap-2">
                        <span className="text-sm">{role.label}</span>
                        <Select
                          value={String(tier.current[role.key] ?? 0)}
                          disabled={savingRole !== null || adopting}
                          onValueChange={(v) => void saveTier(role.key, Number(v))}
                        >
                          <SelectTrigger className="w-64">
                            <SelectValue />
                          </SelectTrigger>
                          <SelectContent>
                            <SelectItem value="0">跟随全局</SelectItem>
                            {tier.profiles.map((p) => (
                              <SelectItem key={p.id} value={String(p.id)}>
                                {p.name} · {p.model}
                              </SelectItem>
                            ))}
                          </SelectContent>
                        </Select>
                      </div>
                    ))}
                    <p className="text-muted-foreground text-xs">
                      选「跟随全局」= 清除该角色的档位绑定（后端记为 0）。adviser（顾问）只在「一键采用建议」时一起写入。
                    </p>
                  </div>
                </>
              )}
            </>
          ) : null}
        </OnboardStep>
      ) : null}

      {/* ── 步 3 · 出口 ── */}
      {step === 2 ? (
        <OnboardStep
          index={3}
          title="出口"
          hint="任务出网前先验证出口；跳过则首次任务可能因出口未就绪而中止。"
          active={step === 2}
          done={done[2]}
          footer={
            <>
              <Button variant="outline" onClick={() => setStep(1)}>
                上一步
              </Button>
              <Button onClick={next}>
                下一步
                <ArrowRightIcon className="size-4" />
              </Button>
            </>
          }
        >
          {egressLoading && !egress ? (
            <div className="flex flex-col gap-2">
              <Skeleton className="h-20 w-full" />
              <Skeleton className="h-9 w-56" />
            </div>
          ) : egressErr && !egress ? (
            <div className="flex items-center justify-between gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3">
              <p className="text-sm">出口检查失败：{egressErr}</p>
              <Button variant="outline" size="sm" onClick={() => void runEgress(true)} disabled={egressLoading}>
                重试
              </Button>
            </div>
          ) : egress?.ok ? (
            <div className="flex flex-wrap items-center gap-3 rounded-md border border-emerald-500/40 bg-emerald-500/5 p-3">
              <RadioTowerIcon className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
              <p className="text-sm font-medium text-emerald-600 dark:text-emerald-400">出口可用</p>
              <Badge variant="outline">exit_ip={egress.exit_ip || "—"}</Badge>
              <Badge variant="outline">node={egress.node || "—"}</Badge>
              <Button
                variant="outline"
                size="sm"
                className="ml-auto"
                onClick={() => void runEgress(true)}
                disabled={egressLoading}
              >
                <RefreshCwIcon className={cn("size-4", egressLoading && "animate-spin")} />
                重试
              </Button>
            </div>
          ) : egress ? (
            <div className="flex flex-col gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3">
              <div className="flex items-start gap-2">
                <ShieldAlertIcon className="mt-0.5 size-4 shrink-0 text-destructive" />
                <div>
                  <p className="text-sm font-medium text-destructive">出口未就绪</p>
                  <p className="text-muted-foreground text-xs">{egress.detail || "后端没有给出详情。"}</p>
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" onClick={() => router.push("/system/network")}>
                  我去改代理
                </Button>
                <Button variant="outline" size="sm" onClick={() => setShowDirectHint((v) => !v)}>
                  只打本地靶场
                </Button>
                <Button variant="outline" size="sm" onClick={() => void runEgress(true)} disabled={egressLoading}>
                  <RefreshCwIcon className={cn("size-4", egressLoading && "animate-spin")} />
                  重试
                </Button>
              </div>
              {showDirectHint ? (
                <p className="text-muted-foreground rounded-md border bg-background/60 p-2 text-xs leading-relaxed">
                  启动时设 <code className="font-mono">ARTEX_ALLOW_DIRECT=1</code>
                  ，或在首页快速任务选「本地靶场」档位。
                </p>
              ) : null}
            </div>
          ) : null}
        </OnboardStep>
      ) : null}

      {/* ── 步 4 · 凭据 ── */}
      {step === 3 ? (
        <OnboardStep
          index={4}
          title="凭据"
          hint="已存凭据用主密钥加密；用临时密钥时重启后旧凭据无法解密。"
          active={step === 3}
          done={done[3]}
          footer={
            <>
              <Button variant="outline" onClick={() => setStep(2)}>
                上一步
              </Button>
              <Button
                onClick={() => {
                  markDone(3);
                  router.push("/");
                }}
                disabled={settingsLoading && !settings}
              >
                完成
              </Button>
            </>
          }
        >
          {settingsLoading && !settings ? (
            <Skeleton className="h-20 w-full" />
          ) : settingsErr && !settings ? (
            <div className="flex items-center justify-between gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3">
              <p className="text-sm">读取配置失败：{settingsErr}</p>
              <Button variant="outline" size="sm" onClick={() => void loadSettings()}>
                重试
              </Button>
            </div>
          ) : fingerprint ? (
            <div className="flex items-start gap-2 rounded-md border border-emerald-500/40 bg-emerald-500/5 p-3">
              <ShieldCheckIcon className="mt-0.5 size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
              <p className="text-sm">
                凭据加密已启用（指纹 <span className="font-mono">{fingerprint}</span>）
              </p>
            </div>
          ) : (
            <div className="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/5 p-3">
              <KeyRoundIcon className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
              <p className="text-sm leading-relaxed">
                正在使用临时密钥：重启后已存凭据无法解密。建议在启动目录保留{" "}
                <code className="font-mono">data/cred.key</code>。
              </p>
            </div>
          )}
          <p className="text-muted-foreground text-xs">
            跳过本步不影响使用，只是临时密钥每次启动都会变化。点「完成」直接回首页。
          </p>
        </OnboardStep>
      ) : null}
    </div>
  );
}
