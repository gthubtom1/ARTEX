"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

// 数据定位：把数据库输出解析成「表 / 行数 / 敏感列 / 证明级别」。
// 只做展示，不执行 SQL、不导出数据。

type Table = { name: string; rows: number; sensitive_columns?: string[] | null };
type LocateResult = { tables?: Table[] | null; proof_level: string; notes?: string[] | null };

function authHeaders(): Record<string, string> {
  const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export default function DataPage() {
  const [engine, setEngine] = React.useState("mysql");
  const [output, setOutput] = React.useState("");
  const [result, setResult] = React.useState<LocateResult | null>(null);
  const [probe, setProbe] = React.useState<string[]>([]);
  const [err, setErr] = React.useState("");
  const [busy, setBusy] = React.useState(false);

  const locate = React.useCallback(async () => {
    setBusy(true);
    setErr("");
    try {
      const r = await fetch("/api/data/locate", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: JSON.stringify({ engine, output }),
      });
      const d = await r.json();
      if (!r.ok) {
        setErr(d.error ?? `解析失败：HTTP ${r.status}`);
        return;
      }
      setResult(d.result);
      setProbe(d.probe ?? []);
    } catch (e) {
      setErr(String(e));
    } finally {
      setBusy(false);
    }
  }, [engine, output]);

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4">
      <div>
        <h1 className="text-lg font-semibold">数据定位</h1>
        <p className="text-sm text-muted-foreground">
          粘贴 information_schema / SHOW TABLES / SELECT COUNT(*) 的输出，得到表、行数、敏感列与证明级别（D1–D4）。
          只记录，不导出。
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <select
          className="h-8 rounded-md border bg-background px-2 text-sm"
          value={engine}
          onChange={(e) => setEngine(e.target.value)}
        >
          <option value="mysql">MySQL / MariaDB</option>
          <option value="postgres">PostgreSQL</option>
          <option value="mssql">SQL Server</option>
        </select>
        <Button size="sm" disabled={busy || !output.trim()} onClick={locate}>
          {busy ? "解析中…" : "解析输出"}
        </Button>
      </div>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <textarea
        className="min-h-[160px] w-full rounded-md border bg-background p-2 font-mono text-xs"
        placeholder="例：SELECT table_name, table_rows FROM information_schema.tables; 的输出…"
        value={output}
        onChange={(e) => setOutput(e.target.value)}
      />

      {result && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">
              解析结果（证明级别 <Badge variant="outline">{result.proof_level || "D0"}</Badge>）
            </CardTitle>
          </CardHeader>
          <CardContent className="space-y-3">
            {(result.tables?.length ?? 0) === 0 ? (
              <div className="text-sm text-muted-foreground">没解析出表；确认粘贴的是表列表 / COUNT 输出。</div>
            ) : (
              <table className="w-full text-xs">
                <thead className="text-left text-muted-foreground">
                  <tr className="border-b">
                    <th className="py-1 pr-2">表</th>
                    <th className="py-1 pr-2">行数</th>
                    <th className="py-1">敏感列</th>
                  </tr>
                </thead>
                <tbody>
                  {result.tables!.map((t) => (
                    <tr key={t.name} className="border-b last:border-0">
                      <td className="py-1 pr-2 font-medium">{t.name}</td>
                      <td className="py-1 pr-2">{t.rows >= 0 ? t.rows : "未知"}</td>
                      <td className="py-1">{(t.sensitive_columns ?? []).join("、") || "—"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
            {(result.notes?.length ?? 0) > 0 && (
              <div className="text-xs text-muted-foreground">{(result.notes ?? []).join("；")}</div>
            )}
          </CardContent>
        </Card>
      )}

      {probe.length > 0 && (
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="text-sm">只读探测模板（复制到已获得的数据库连接执行）</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="whitespace-pre-wrap text-xs leading-relaxed">{probe.join("\n")}</pre>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
