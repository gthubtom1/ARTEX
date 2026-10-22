"use client";

import * as React from "react";

import { HistoryIcon, NetworkIcon, RefreshCwIcon, ShieldCheckIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { p2api, p8api } from "@/lib/api";
import type { NetworkResponse } from "@/lib/types";
import { cn } from "@/lib/utils";

// 网络页（D1 之后）：**只做出口 IP 自检**。
//
// 应用内不再有代理 / 节点 / 延迟 / 订阅 / 切节点 —— 部署目标是 VPS 或 TUN 全局的 VM，
// 网络在宿主层解决；应用内再配一遍 = 两个真相源，已经实际出过一次 bug。
// 这里只剩三块：当前出口 IP / 基准 IP / 最近变更记录，外加一个「重新检测」。
export default function NetworkPage() {
  const [data, setData] = React.useState<NetworkResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [checking, setChecking] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);

  const load = React.useCallback(async (notify = false) => {
    setLoading(true);
    try {
      setErr("");
      const r = await p2api.network();
      setData(r);
      if (notify) toast.success("已刷新网络状态");
    } catch (e) {
      const message = String(e);
      setErr(message);
      toast.error(message);
    } finally {
      setLoading(false);
      setLoaded(true);
    }
  }, []);

  // 重新检测：绕过 60s 缓存，真打一次两个 IP 源。
  const recheck = React.useCallback(async () => {
    setChecking(true);
    try {
      const r = await p8api.egressCheck(true);
      if (r.ok) toast.success("出口自检通过：" + (r.exit_ip || "—"));
      else toast.warning("出口自检未通过：" + (r.detail || "未知原因"));
      await load();
    } catch (e) {
      toast.error("检测失败：" + (e as Error).message);
    } finally {
      setChecking(false);
    }
  }, [load]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const current = data?.current ?? {};
  const baseline = current.baseline_ip || data?.settings?.["net.baseline_ip"] || "";
  const exitIP = current.exit_ip || "";
  // 最近变更记录：egress_log 里「没通过」的那些行（出口变了 / 查不到 / 内网地址）。
  const changes = (data?.egress ?? []).filter((e) => !e.ok);

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">出口与网络</h1>
          <p className="text-muted-foreground text-sm">
            只做出口 IP 自检：出网路径由宿主层（VPS 路由 / TUN 全局）负责，应用内不再配置代理。
            出口变了会在这里显形，但<b>不会拦截任务</b>。
          </p>
        </div>
        <Button size="sm" variant="outline" disabled={loading || checking} onClick={() => void recheck()}>
          <RefreshCwIcon className={cn("size-4", (loading || checking) && "animate-spin")} />
          重新检测
        </Button>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <NetworkIcon className="size-4" />
              当前出口 IP
            </CardTitle>
            <CardDescription>最近一次自检的结果；点「重新检测」立即真打一次（跳缓存）。</CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {!loaded ? (
              <Skeleton className="h-12 w-48" />
            ) : exitIP ? (
              <div className="flex flex-wrap items-center gap-3">
                <span className="font-mono text-2xl font-semibold tracking-tight">{exitIP}</span>
                <Badge variant={current.ok ? "outline" : "destructive"}>{current.ok ? "✅ 正常" : "❌ 异常"}</Badge>
              </div>
            ) : (
              <p className="text-muted-foreground text-sm">还没有出口记录 —— 点右上角「重新检测」。</p>
            )}
            <p className="text-muted-foreground text-xs break-all">
              {current.at ? `检测时间 ${current.at} · ` : ""}
              {current.detail || "—"}
            </p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-2 text-sm">
              <ShieldCheckIcon className="size-4" />
              基准 IP
            </CardTitle>
            <CardDescription>
              首次自检成功时自动记下（<code>net.baseline_ip</code>）；之后每次比对，不一致会写进下面的变更记录。
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {!loaded ? (
              <Skeleton className="h-8 w-40" />
            ) : baseline ? (
              <span className="font-mono text-lg font-medium">{baseline}</span>
            ) : (
              <span className="text-muted-foreground text-sm">尚未建立基准（第一次自检成功后自动记录）</span>
            )}
            {baseline && exitIP && baseline !== exitIP && (
              <p className="text-xs text-amber-600 dark:text-amber-400">
                当前出口与基准不同 —— 可能换了机房、TUN 掉线或路由泄漏，请人工确认。
              </p>
            )}
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="flex items-center gap-2 text-sm">
            <HistoryIcon className="size-4" />
            最近变更记录（最近 50 条里未通过的自检）
          </CardTitle>
        </CardHeader>
        <CardContent>
          {!loaded ? (
            <div className="space-y-2">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : changes.length === 0 ? (
            <Empty className="border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <HistoryIcon />
                </EmptyMedia>
                <EmptyTitle>还没有出口变更</EmptyTitle>
                <EmptyDescription>出口 IP 与基准不一致、或自检失败时，会按时间倒序记在这里。</EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="max-h-[48vh] overflow-auto rounded-md border">
              <Table className="text-xs">
                <TableHeader className="sticky top-0 z-10 bg-card">
                  <TableRow>
                    <TableHead className="h-8">时间</TableHead>
                    <TableHead className="h-8">出口 IP</TableHead>
                    <TableHead className="h-8">结果</TableHead>
                    <TableHead className="h-8">细节</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {changes.map((e, i) => (
                    <TableRow key={i}>
                      <TableCell className="py-1 whitespace-nowrap">{e.ts}</TableCell>
                      <TableCell className="py-1 font-mono">{e.egress_ip || "—"}</TableCell>
                      <TableCell className="py-1">❌</TableCell>
                      <TableCell className="max-w-[420px] truncate py-1" title={e.detail}>
                        {e.detail}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
