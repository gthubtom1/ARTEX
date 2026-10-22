"use client";

import * as React from "react";

import { RefreshCwIcon, SearchIcon, WrenchIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { p2api } from "@/lib/api";
import type { EnvironmentResponse } from "@/lib/types";
import { cn } from "@/lib/utils";

// 环境页：本机工具面（自动发现）+ 关键运行参数。缺工具时这里能直接看到缺哪些。
export default function EnvironmentPage() {
  const [data, setData] = React.useState<EnvironmentResponse | null>(null);
  const [q, setQ] = React.useState("");
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);

  const load = React.useCallback(async (notify = false) => {
    setLoading(true);
    try {
      setErr("");
      const r = await p2api.environment();
      setData(r);
      if (notify) toast.success(`已刷新，发现 ${r.count} 个工具`);
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

  const tools = (data?.tools ?? []).filter((t) => {
    const n = q.trim().toLowerCase();
    return !n || t.name.toLowerCase().includes(n) || (t.path ?? "").toLowerCase().includes(n);
  });

  const settings = Object.entries(data?.settings ?? {});

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">环境与工具</h1>
          <p className="text-muted-foreground text-sm">
            五层扫描结果（PATH/常见目录/包管理/桌面/全盘）+ 运行参数。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <SearchIcon className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="h-8 w-48 pl-8"
              placeholder="过滤工具名…"
              value={q}
              onChange={(e) => setQ(e.target.value)}
            />
          </div>
          <Button size="sm" variant="outline" disabled={loading} onClick={() => load(true)}>
            <RefreshCwIcon className={cn("size-4", loading && "animate-spin")} />
            刷新
          </Button>
        </div>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <div className="flex flex-wrap gap-2 text-xs">
        <Badge variant="outline">发现 {data?.count ?? 0} 个工具</Badge>
        {(data?.missing ?? []).slice(0, 20).map((m) => (
          <Badge key={m} className="bg-amber-500/10 text-amber-600 dark:text-amber-400">
            缺 {m}
          </Badge>
        ))}
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">关键参数</CardTitle>
        </CardHeader>
        <CardContent>
          {!loaded ? (
            <div className="grid grid-cols-2 gap-1 sm:grid-cols-3 md:grid-cols-4">
              {Array.from({ length: 8 }).map((_, i) => (
                <Skeleton key={i} className="h-7 w-full" />
              ))}
            </div>
          ) : settings.length === 0 ? (
            <p className="text-sm text-muted-foreground">还没有读取到运行参数。</p>
          ) : (
            <div className="grid grid-cols-2 gap-1 text-xs sm:grid-cols-3 md:grid-cols-4">
              {settings.map(([k, v]) => (
                <div key={k} className="flex justify-between gap-2 rounded border px-2 py-1">
                  <span className="text-muted-foreground">{k}</span>
                  <span className="truncate" title={v}>
                    {v || "—"}
                  </span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">工具清单（{tools.length}）</CardTitle>
        </CardHeader>
        <CardContent>
          {!loaded ? (
            <div className="space-y-2">
              {Array.from({ length: 6 }).map((_, i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : tools.length === 0 ? (
            <Empty className="border border-dashed">
              <EmptyHeader>
                <EmptyMedia variant="icon">
                  <WrenchIcon />
                </EmptyMedia>
                <EmptyTitle>还没扫到工具</EmptyTitle>
                <EmptyDescription>
                  后台扫描可能仍在进行；也可以清空过滤条件后重试。缺失的工具会列在上方徽标里。
                </EmptyDescription>
              </EmptyHeader>
            </Empty>
          ) : (
            <div className="max-h-[56vh] overflow-auto rounded-md border">
              <Table className="text-xs">
                <TableHeader className="sticky top-0 z-10 bg-card">
                  <TableRow>
                    <TableHead className="h-8">名称</TableHead>
                    <TableHead className="h-8">版本</TableHead>
                    <TableHead className="h-8">健康</TableHead>
                    <TableHead className="h-8">来源</TableHead>
                    <TableHead className="h-8">路径</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tools.map((t) => (
                    <TableRow key={`${t.name}-${t.path}`}>
                      <TableCell className="py-1 font-medium">{t.name}</TableCell>
                      <TableCell className="py-1">{t.version || "—"}</TableCell>
                      <TableCell className="py-1">{t.healthy ? "✅" : "⚠️"}</TableCell>
                      <TableCell className="py-1">{t.source || t.kind}</TableCell>
                      <TableCell className="max-w-[420px] truncate py-1" title={t.path}>
                        {t.path}
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
