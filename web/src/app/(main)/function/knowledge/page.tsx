"use client";

import * as React from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { p2api } from "@/lib/api";
import type { KnowledgeResponse } from "@/lib/types";

// 知识库页：在 kb/ 下按文件名/正文搜索（只读）。命中后显示路径与上下文片段。
export default function KnowledgePage() {
  const [q, setQ] = React.useState("");
  const [data, setData] = React.useState<KnowledgeResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);

  const search = React.useCallback(async (query: string) => {
    setLoading(true);
    try {
      setErr("");
      setData(await p2api.knowledge(query));
    } catch (e) {
      setErr(String(e));
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void search("");
  }, [search]);

  return (
    <div className="mx-auto flex w-full max-w-5xl flex-col gap-4 p-4">
      <div>
        <h1 className="text-lg font-semibold">知识库</h1>
        <p className="text-sm text-muted-foreground">
          战术/能力清单/指纹/案例都在 kb/ 下；搜索命中直接给出文件路径与上下文。
        </p>
      </div>

      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void search(q);
        }}
      >
        <Input placeholder="搜：sql 注入 / 上传 / 提权 / kerberos…" value={q} onChange={(e) => setQ(e.target.value)} />
        <Button type="submit" disabled={loading}>
          {loading ? "搜索中…" : "搜索"}
        </Button>
      </form>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <div className="flex flex-wrap gap-2 text-xs">
        <Badge variant="outline">根目录 {data?.root ?? "kb"}</Badge>
        <Badge variant="outline">命中 {data?.count ?? 0}</Badge>
      </div>

      <Card>
        <CardHeader className="pb-2">
          <CardTitle className="text-sm">命中结果</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          {(data?.hits?.length ?? 0) === 0 ? (
            <div className="text-sm text-muted-foreground">没有命中。换个关键词试试。</div>
          ) : (
            data!.hits.map((h) => (
              <div key={h.path} className="rounded-md border p-2">
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-medium">{h.title || h.path}</span>
                  <span className="text-muted-foreground">{h.path}</span>
                </div>
                <div className="mt-1 text-xs text-muted-foreground">{h.snippet}</div>
              </div>
            ))
          )}
        </CardContent>
      </Card>
    </div>
  );
}
