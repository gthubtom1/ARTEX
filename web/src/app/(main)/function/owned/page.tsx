"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { p2api } from "@/lib/api";
import type { OwnedResponse } from "@/lib/types";

// 持有台账页：打下来的东西还在不在。支持手动复查与 JSON 导出（报告/交接用）。

const statusTone: Record<string, string> = {
  owned: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  verified: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  stale: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  lost: "bg-red-500/10 text-red-600 dark:text-red-400",
  cleaned: "bg-muted text-foreground/60",
};

function fmt(ts?: string) {
  if (!ts) return "—";
  const d = new Date(ts);
  if (isNaN(d.getTime()) || d.getFullYear() < 2000) return "—";
  return d.toLocaleString();
}

export default function OwnedPage() {
  const [data, setData] = React.useState<OwnedResponse | null>(null);
  const [status, setStatus] = React.useState("");
  const [busy, setBusy] = React.useState(false);
  const [msg, setMsg] = React.useState("");
  const [err, setErr] = React.useState("");

  const load = React.useCallback(async (s: string) => {
    try {
      setErr("");
      setData(await p2api.owned(s || undefined));
    } catch (e) {
      setErr(String(e));
    }
  }, []);

  React.useEffect(() => {
    void load(status);
  }, [status, load]);

  async function recheck() {
    setBusy(true);
    setMsg("");
    try {
      const r = await p2api.recheckOwned();
      setMsg(
        `复查完成：检查 ${r.result.checked ?? 0} 台，存活 ${r.result.ok ?? 0}，失效 ${r.result.failed ?? 0}，跳过 ${r.result.skipped ?? 0}`,
      );
      await load(status);
    } catch (e) {
      setMsg(String(e));
    } finally {
      setBusy(false);
    }
  }

  async function download() {
    const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
    const r = await fetch("/api/owned/export", {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!r.ok) {
      setMsg(`导出失败：HTTP ${r.status}`);
      return;
    }
    const blob = await r.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = "artex-ledger.json";
    a.click();
    URL.revokeObjectURL(url);
  }

  const assets = data?.assets ?? [];
  const stats = data?.stats ?? {};

  return (
    <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h1 className="text-lg font-semibold">持有台账</h1>
          <p className="text-sm text-muted-foreground">
            打下来的资产跨任务保留；到期自动复查，改密/关站会标 stale。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <select
            className="h-8 rounded-md border bg-background px-2 text-sm"
            value={status}
            onChange={(e) => setStatus(e.target.value)}
          >
            <option value="">全部状态</option>
            {["owned", "verified", "stale", "lost", "cleaned"].map((s) => (
              <option key={s} value={s}>
                {s}
              </option>
            ))}
          </select>
          <Button size="sm" disabled={busy} onClick={recheck}>
            {busy ? "复查中…" : "现在复查一遍"}
          </Button>
          <Button size="sm" variant="outline" onClick={download}>
            导出 JSON
          </Button>
        </div>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}
      {msg && <div className="rounded-md border bg-muted/40 p-2 text-sm">{msg}</div>}

      <div className="flex flex-wrap gap-2 text-xs">
        {Object.entries(stats).map(([k, v]) => (
          <Badge key={k} variant="outline">
            {k} {v}
          </Badge>
        ))}
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">台账（{assets.length} 条）</CardTitle>
        </CardHeader>
        <CardContent>
          {assets.length === 0 ? (
            <div className="text-sm text-muted-foreground">
              还没有持有记录。worker 打下 shell/后台后会用 owned_register 自动登记。
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">资产</th>
                    <th className="py-1 pr-2">类型</th>
                    <th className="py-1 pr-2">权限</th>
                    <th className="py-1 pr-2">跳数</th>
                    <th className="py-1 pr-2">状态</th>
                    <th className="py-1 pr-2">最近验证</th>
                    <th className="py-1 pr-2">下次复查</th>
                    <th className="py-1">证据/备注</th>
                  </tr>
                </thead>
                <tbody>
                  {assets.map((o) => (
                    <tr key={o.id} className="border-b last:border-0">
                      <td className="py-1 pr-2">
                        {o.host || o.ip}
                        {o.port ? `:${o.port}` : ""}
                      </td>
                      <td className="py-1 pr-2">{o.access_type}</td>
                      <td className="py-1 pr-2">{o.privilege}</td>
                      <td className="py-1 pr-2">{o.depth}</td>
                      <td className="py-1 pr-2">
                        <Badge className={statusTone[o.status] ?? ""}>{o.status}</Badge>
                      </td>
                      <td className="py-1 pr-2">{fmt(o.last_verified_at)}</td>
                      <td className="py-1 pr-2">{fmt(o.next_check_at)}</td>
                      <td className="max-w-[280px] truncate py-1" title={o.evidence || o.notes || ""}>
                        {o.evidence || o.notes || "—"}
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
