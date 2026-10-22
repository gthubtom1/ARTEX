"use client";

import * as React from "react";

import { ScanSearchIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Textarea } from "@/components/ui/textarea";

// 数据定位：把数据库输出解析成「表 / 行数 / 敏感列 / 证明级别」。
// 只做展示，不执行 SQL、不导出数据。

type LocateTable = { name: string; rows: number; sensitive_columns?: string[] | null };
type LocateResult = { tables?: LocateTable[] | null; proof_level: string; notes?: string[] | null };

interface LocateResponse {
  result?: LocateResult;
  probe?: string[] | null;
  error?: string;
}

const ENGINES = [
  { value: "mysql", label: "MySQL / MariaDB" },
  { value: "postgres", label: "PostgreSQL" },
  { value: "mssql", label: "SQL Server" },
] as const;

function authHeaders(): Record<string, string> {
  const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
  return token ? { Authorization: `Bearer ${token}` } : {};
}

export default function DataPage() {
  const [engine, setEngine] = React.useState("mysql");
  const [output, setOutput] = React.useState("");
  const [result, setResult] = React.useState<LocateResult | null>(null);
  const [probe, setProbe] = React.useState<string[]>([]);
  const [busy, setBusy] = React.useState(false);

  const locate = React.useCallback(async () => {
    if (busy) return;
    const text = output.trim();
    if (!text) return;
    setBusy(true);
    try {
      const r = await fetch("/api/data/locate", {
        method: "POST",
        headers: { "Content-Type": "application/json", ...authHeaders() },
        body: JSON.stringify({ engine, output: text }),
      });
      const payload = (await r.json().catch(() => null)) as LocateResponse | null;
      if (!r.ok) {
        toast.error(payload?.error ?? `解析失败：HTTP ${r.status}`);
        return;
      }
      if (!payload?.result) {
        toast.error("解析结果为空，请检查粘贴内容。");
        return;
      }
      setResult(payload.result);
      setProbe(payload.probe ?? []);
      const count = payload.result.tables?.length ?? 0;
      toast.success(`解析完成，识别 ${count} 张表`);
    } catch (e) {
      toast.error(`解析失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }, [busy, engine, output]);

  const tables = result?.tables ?? [];
  const notes = result?.notes ?? [];

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="text-lg font-semibold">数据定位</h1>
        <p className="text-sm text-muted-foreground">
          粘贴 information_schema / SHOW TABLES / SELECT COUNT(*) 的输出，得到表、行数、敏感列与证明级别（D1–D4）。
          只记录，不导出。
        </p>
      </div>

      <Card className="gap-3">
        <CardHeader>
          <CardTitle className="text-sm">粘贴数据库输出</CardTitle>
          <CardDescription>纯本地文本解析：不连接数据库、不执行 SQL、不保存原始输出。</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <FieldGroup>
            <Field className="max-w-60">
              <FieldLabel htmlFor="data-engine">数据库类型</FieldLabel>
              <Select value={engine} onValueChange={setEngine} disabled={busy}>
                <SelectTrigger id="data-engine" className="w-full">
                  <SelectValue placeholder="选择数据库类型" />
                </SelectTrigger>
                <SelectContent>
                  {ENGINES.map((item) => (
                    <SelectItem key={item.value} value={item.value}>
                      {item.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field>
              <FieldLabel htmlFor="data-output">解析输入</FieldLabel>
              <Textarea
                id="data-output"
                className="min-h-40 font-mono text-xs"
                placeholder="例：SELECT table_name, table_rows FROM information_schema.tables; 的输出…"
                value={output}
                onChange={(e) => setOutput(e.target.value)}
                disabled={busy}
              />
              <FieldDescription>
                支持每行「表名 + 行数」的表格输出或 COUNT(*) 结果；无法识别的行会被跳过。
              </FieldDescription>
            </Field>
          </FieldGroup>
          <div className="flex items-center justify-end gap-2">
            <span className="mr-auto text-xs text-muted-foreground tabular-nums">{output.length} 字符</span>
            <Button size="sm" disabled={busy || !output.trim()} onClick={() => void locate()}>
              {busy ? <Spinner /> : <ScanSearchIcon />}
              {busy ? "解析中…" : "解析输出"}
            </Button>
          </div>
        </CardContent>
      </Card>

      {result ? (
        <Card className="gap-0 py-0">
          <CardHeader className="flex flex-row items-center justify-between gap-3 px-4 py-3">
            <div className="flex min-w-0 items-center gap-2">
              <CardTitle className="text-sm">解析结果</CardTitle>
              <Badge variant="outline">证明级别 {result.proof_level || "D0"}</Badge>
            </div>
            <span className="text-xs text-muted-foreground tabular-nums">共 {tables.length} 张表</span>
          </CardHeader>
          <CardContent className="px-0">
            {tables.length === 0 ? (
              <div className="px-4 pb-4">
                <Empty className="border border-dashed">
                  <EmptyHeader>
                    <EmptyTitle>没解析出表</EmptyTitle>
                    <EmptyDescription>确认粘贴的是表列表 / COUNT 输出，并选择正确的数据库类型。</EmptyDescription>
                  </EmptyHeader>
                </Empty>
              </div>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>表</TableHead>
                    <TableHead className="w-24 text-right">行数</TableHead>
                    <TableHead>敏感列</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {tables.map((t) => (
                    <TableRow key={t.name}>
                      <TableCell className="font-mono text-xs font-medium">{t.name}</TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{t.rows >= 0 ? t.rows : "未知"}</TableCell>
                      <TableCell>
                        {(t.sensitive_columns ?? []).length === 0 ? (
                          <span className="text-xs text-muted-foreground">—</span>
                        ) : (
                          <div className="flex flex-wrap gap-1">
                            {(t.sensitive_columns ?? []).filter(Boolean).map((column) => (
                              <Badge key={column} variant="secondary" className="font-mono text-[11px]">
                                {column}
                              </Badge>
                            ))}
                          </div>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            {notes.length > 0 ? (
              <div className="border-t px-4 py-3 text-xs text-muted-foreground">{notes.join("；")}</div>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      {probe.length > 0 ? (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">只读探测模板</CardTitle>
            <CardDescription>复制到已获得的数据库连接执行，用于补全行数与敏感列证据。</CardDescription>
          </CardHeader>
          <CardContent>
            <pre className="overflow-x-auto rounded-lg bg-muted/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap">
              {probe.join("\n")}
            </pre>
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
