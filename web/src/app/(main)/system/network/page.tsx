"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { p2api } from "@/lib/api";
import type { NetworkResponse } from "@/lib/types";

// 网络页：出口参数 + 最近出口校验/切换记录。只读，不主动发探测。
export default function NetworkPage() {
  const [data, setData] = React.useState<NetworkResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      setErr("");
      setData(await p2api.network());
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load();
  }, [load]);

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4">
      <div className="flex items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">出口与网络</h1>
          <p className="text-sm text-muted-foreground">
            所有流量走代理；出口不可用时任务 fail-closed（不会直连暴露本机 IP）。
          </p>
        </div>
        <Button size="sm" variant="outline" disabled={loading} onClick={load}>
          {loading ? "刷新中…" : "刷新"}
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
            {(data?.egress?.length ?? 0) === 0 ? (
              <div className="text-sm text-muted-foreground">还没有出口校验记录。</div>
            ) : (
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">时间</th>
                    <th className="py-1 pr-2">出口 IP</th>
                    <th className="py-1 pr-2">节点</th>
                    <th className="py-1 pr-2">结果</th>
                    <th className="py-1">细节</th>
                  </tr>
                </thead>
                <tbody>
                  {data!.egress.map((e, i) => (
                    <tr key={i} className="border-b last:border-0">
                      <td className="py-1 pr-2">{e.ts}</td>
                      <td className="py-1 pr-2">{e.egress_ip || "—"}</td>
                      <td className="py-1 pr-2">{e.node || "—"}</td>
                      <td className="py-1 pr-2">{e.ok ? "✅" : "❌"}</td>
                      <td className="max-w-[260px] truncate py-1" title={e.detail}>
                        {e.detail}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">网络事件（最近 50）</CardTitle>
          </CardHeader>
          <CardContent>
            {(data?.events?.length ?? 0) === 0 ? (
              <div className="text-sm text-muted-foreground">还没有网络事件。</div>
            ) : (
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">时间</th>
                    <th className="py-1 pr-2">类型</th>
                    <th className="py-1">细节</th>
                  </tr>
                </thead>
                <tbody>
                  {data!.events.map((e, i) => (
                    <tr key={i} className="border-b last:border-0">
                      <td className="py-1 pr-2">{e.ts}</td>
                      <td className="py-1 pr-2">{e.kind}</td>
                      <td className="max-w-[320px] truncate py-1" title={e.detail}>
                        {e.detail}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
