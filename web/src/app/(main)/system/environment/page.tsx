"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { p2api } from "@/lib/api";
import type { EnvironmentResponse } from "@/lib/types";

// 环境页：本机工具面（自动发现）+ 关键运行参数。缺工具时这里能直接看到缺哪些。
export default function EnvironmentPage() {
  const [data, setData] = React.useState<EnvironmentResponse | null>(null);
  const [q, setQ] = React.useState("");
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      setErr("");
      setData(await p2api.environment());
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  const tools = (data?.tools ?? []).filter((t) => {
    const n = q.trim().toLowerCase();
    return !n || t.name.toLowerCase().includes(n) || (t.path ?? "").toLowerCase().includes(n);
  });

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">环境与工具</h1>
          <p className="text-sm text-muted-foreground">五层扫描结果（PATH/常见目录/包管理/桌面/全盘）+ 运行参数。</p>
        </div>
        <div className="flex items-center gap-2">
          <Input className="h-8 w-48" placeholder="过滤工具名…" value={q} onChange={(e) => setQ(e.target.value)} />
          <Button size="sm" variant="outline" disabled={loading} onClick={load}>
            {loading ? "刷新中…" : "刷新"}
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
        <CardContent className="grid grid-cols-2 gap-1 text-xs sm:grid-cols-3 md:grid-cols-4">
          {Object.entries(data?.settings ?? {}).map(([k, v]) => (
            <div key={k} className="flex justify-between gap-2 rounded border px-2 py-1">
              <span className="text-muted-foreground">{k}</span>
              <span className="truncate" title={v}>
                {v || "—"}
              </span>
            </div>
          ))}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">工具清单（{tools.length}）</CardTitle>
        </CardHeader>
        <CardContent>
          {tools.length === 0 ? (
            <div className="text-sm text-muted-foreground">还没扫到工具（后台扫描可能仍在进行）。</div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">名称</th>
                    <th className="py-1 pr-2">版本</th>
                    <th className="py-1 pr-2">健康</th>
                    <th className="py-1 pr-2">来源</th>
                    <th className="py-1">路径</th>
                  </tr>
                </thead>
                <tbody>
                  {tools.map((t) => (
                    <tr key={`${t.name}-${t.path}`} className="border-b last:border-0">
                      <td className="py-1 pr-2 font-medium">{t.name}</td>
                      <td className="py-1 pr-2">{t.version || "—"}</td>
                      <td className="py-1 pr-2">{t.healthy ? "✅" : "⚠️"}</td>
                      <td className="py-1 pr-2">{t.source || t.kind}</td>
                      <td className="max-w-[420px] truncate py-1" title={t.path}>
                        {t.path}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
