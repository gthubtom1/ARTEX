"use client";

import * as React from "react";

import { BookOpenIcon, FileSearchIcon, SearchIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { p2api } from "@/lib/api";
import type { KnowledgeResponse } from "@/lib/types";

// 知识库页：在 kb/ 下按文件名/正文搜索（只读）。命中后显示路径与上下文片段。
export default function KnowledgePage() {
  const [q, setQ] = React.useState("");
  const [data, setData] = React.useState<KnowledgeResponse | null>(null);
  const [err, setErr] = React.useState("");
  const [loading, setLoading] = React.useState(false);
  const [loaded, setLoaded] = React.useState(false);

  const search = React.useCallback(async (query: string, notify = false) => {
    setLoading(true);
    try {
      setErr("");
      const r = await p2api.knowledge(query);
      setData(r);
      if (notify) toast.success(`搜索完成，命中 ${r.count} 条`);
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
    void search("");
  }, [search]);

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">知识库</h1>
        <p className="text-muted-foreground text-sm">
          战术/能力清单/指纹/案例都在 kb/ 下；搜索命中直接给出文件路径与上下文。
        </p>
      </div>

      <form
        className="flex max-w-2xl gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          void search(q, true);
        }}
      >
        <div className="relative flex-1">
          <SearchIcon className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            className="pl-8"
            placeholder="搜：sql 注入 / 上传 / 提权 / kerberos…"
            value={q}
            onChange={(e) => setQ(e.target.value)}
          />
        </div>
        <Button type="submit" disabled={loading}>
          <FileSearchIcon className="size-4" />
          {loading ? "搜索中…" : "搜索"}
        </Button>
      </form>

      {err && <div className="rounded-md border border-destructive/40 bg-destructive/5 p-2 text-sm">{err}</div>}

      <div className="flex flex-wrap gap-2 text-xs">
        <Badge variant="outline">根目录 {data?.root ?? "kb"}</Badge>
        <Badge variant="outline">命中 {data?.count ?? 0}</Badge>
      </div>

      <div className="space-y-2">
        {!loaded ? (
          Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-20 w-full" />)
        ) : (data?.hits?.length ?? 0) === 0 ? (
          <Card>
            <CardContent>
              <Empty className="border border-dashed">
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <BookOpenIcon />
                  </EmptyMedia>
                  <EmptyTitle>没有命中</EmptyTitle>
                  <EmptyDescription>
                    换个关键词试试，例如文件名、漏洞类型或工具名；空搜索会列出 kb/ 下的全部条目。
                  </EmptyDescription>
                </EmptyHeader>
              </Empty>
            </CardContent>
          </Card>
        ) : (
          data!.hits.map((h) => (
            <Card key={h.path} size="sm" className="gap-1.5">
              <CardHeader className="px-3">
                <div className="flex flex-wrap items-center gap-2 text-xs">
                  <span className="font-medium">{h.title || h.path}</span>
                  <span className="text-muted-foreground">{h.path}</span>
                </div>
              </CardHeader>
              <CardContent className="px-3">
                <div className="max-h-32 overflow-auto text-xs whitespace-pre-wrap text-muted-foreground">
                  {h.snippet}
                </div>
              </CardContent>
            </Card>
          ))
        )}
      </div>
    </div>
  );
}
