"use client";

import * as React from "react";

import { NetworkIcon, RefreshCwIcon, WaypointsIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { p2api } from "@/lib/api";
import type { NetworkResponse } from "@/lib/types";
import { cn } from "@/lib/utils";

// 网络页：出口参数 + 最近出口校验/切换记录。只读，不主动发探测。
export default function NetworkPage() {
  const [data, setData] = React.useState<NetworkResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
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

  React.useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">出口与网络</h1>
          <p className="text-muted-foreground text-sm">
            所有流量走代理；出口不可用时任务 fail-closed（不会直连暴露本机 IP）。
          </p>
        </div>
        <Button size="sm" variant="outline" disabled={loading} onClick={() => load(true)}>
          <RefreshCwIcon className={cn("size-4", loading && "animate-spin")} />
          刷新
        </Button>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <div className="flex flex-wrap gap-2 text-xs">
        <Badge variant={data?.direct_allowed ? "destructive" : "outline"}>
          {data?.direct_allowed ? "直连已允许（ARTEX_ALLOW_DIRECT=1）" : "强制走代理"}
        </Badge>
        {Object.entries(data?.settings ?? {}).map(([k, v]) => (
          <Badge key={k} variant="outline">
            {k}={v || "—"}
          </Badge>
        ))}
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">出口校验记录（最近 50）</CardTitle>
          </CardHeader>
          <CardContent>
            {!loaded ? (
              <div className="space-y-2">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-8 w-full" />
                ))}
              </div>
            ) : (data?.egress?.length ?? 0) === 0 ? (
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <NetworkIcon />
                  </EmptyMedia>
                  <EmptyTitle>还没有出口校验记录</EmptyTitle>
                  <EmptyDescription>任务发起网络请求后，这里会记录出口 IP、节点与校验结果。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className="max-h-[48vh] overflow-auto rounded-md border">
                <Table className="text-xs">
                  <TableHeader className="sticky top-0 z-10 bg-card">
                    <TableRow>
                      <TableHead className="h-8">时间</TableHead>
                      <TableHead className="h-8">出口 IP</TableHead>
                      <TableHead className="h-8">节点</TableHead>
                      <TableHead className="h-8">结果</TableHead>
                      <TableHead className="h-8">细节</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data!.egress.map((e, i) => (
                      <TableRow key={i}>
                        <TableCell className="py-1 whitespace-nowrap">{e.ts}</TableCell>
                        <TableCell className="py-1">{e.egress_ip || "—"}</TableCell>
                        <TableCell className="py-1">{e.node || "—"}</TableCell>
                        <TableCell className="py-1">{e.ok ? "✅" : "❌"}</TableCell>
                        <TableCell className="max-w-[260px] truncate py-1" title={e.detail}>
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

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">网络事件（最近 50）</CardTitle>
          </CardHeader>
          <CardContent>
            {!loaded ? (
              <div className="space-y-2">
                {Array.from({ length: 5 }).map((_, i) => (
                  <Skeleton key={i} className="h-8 w-full" />
                ))}
              </div>
            ) : (data?.events?.length ?? 0) === 0 ? (
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <WaypointsIcon />
                  </EmptyMedia>
                  <EmptyTitle>还没有网络事件</EmptyTitle>
                  <EmptyDescription>出口切换、代理异常等网络事件会按时间倒序记录在这里。</EmptyDescription>
                </EmptyHeader>
              </Empty>
            ) : (
              <div className="max-h-[48vh] overflow-auto rounded-md border">
                <Table className="text-xs">
                  <TableHeader className="sticky top-0 z-10 bg-card">
                    <TableRow>
                      <TableHead className="h-8">时间</TableHead>
                      <TableHead className="h-8">类型</TableHead>
                      <TableHead className="h-8">细节</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data!.events.map((e, i) => (
                      <TableRow key={i}>
                        <TableCell className="py-1 whitespace-nowrap">{e.ts}</TableCell>
                        <TableCell className="py-1">{e.kind}</TableCell>
                        <TableCell className="max-w-[320px] truncate py-1" title={e.detail}>
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
    </div>
  );
}
