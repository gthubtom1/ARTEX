"use client";

import * as React from "react";

import { DownloadIcon, RefreshCwIcon, SearchIcon } from "lucide-react";
import { toast } from "sonner";

import { TablePagination } from "@/components/table-pagination";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { SortableHead } from "@/components/ui/sortable-head";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { p2api } from "@/lib/api";
import { useStoredSortPreference } from "@/lib/sort-preference";
import type { OwnedAsset, OwnedResponse } from "@/lib/types";

// 持有台账页：打下来的东西还在不在。支持手动复查与 JSON 导出（报告/交接用）。

const PAGE_SIZE = 20;

const statusTone: Record<string, string> = {
  owned: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  verified: "bg-emerald-500/10 text-emerald-600 dark:text-emerald-400",
  stale: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  lost: "bg-red-500/10 text-red-600 dark:text-red-400",
  cleaned: "bg-muted text-foreground/60",
};

const STATUS_LABELS: Record<string, string> = {
  owned: "持有中",
  verified: "已验证",
  stale: "待复查",
  lost: "已失效",
  cleaned: "已清理",
};

const STATUS_OPTIONS = ["owned", "verified", "stale", "lost", "cleaned"] as const;

// 统计卡片的固定顺序：总计在前，其余按台账状态生命周期排列。
const STAT_ORDER = ["total", ...STATUS_OPTIONS];

const SORT_FIELDS = ["asset", "privilege", "status", "last_verified", "next_check"] as const;

// 加载骨架屏的固定行键，避免用数组下标作 key。
const SKELETON_ROWS = ["one", "two", "three", "four", "five", "six"];

type OwnedSortField = (typeof SORT_FIELDS)[number];

function isDateField(field: OwnedSortField): field is "last_verified" | "next_check" {
  return field === "last_verified" || field === "next_check";
}

function dateOf(o: OwnedAsset, field: "last_verified" | "next_check") {
  return parseTime(field === "last_verified" ? o.last_verified_at : o.next_check_at);
}

function statusLabel(status: string) {
  return STATUS_LABELS[status] ?? status;
}

function assetLabel(o: OwnedAsset) {
  const host = [o.host, o.ip].find((value) => value) ?? "";
  return o.port ? `${host}:${o.port}` : host;
}

function parseTime(ts?: string) {
  if (!ts) return null;
  const d = new Date(ts);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return null;
  return d.getTime();
}

function fmt(ts?: string) {
  const t = parseTime(ts);
  if (t === null) return "—";
  return new Date(t).toLocaleString();
}

function textOf(o: OwnedAsset, field: OwnedSortField) {
  switch (field) {
    case "asset":
      return assetLabel(o);
    case "privilege":
      return o.privilege || "";
    case "status":
      return statusLabel(o.status);
    default:
      return "";
  }
}

export default function OwnedPage() {
  const [data, setData] = React.useState<OwnedResponse | null>(null);
  const [status, setStatus] = React.useState("all");
  const [search, setSearch] = React.useState("");
  const [page, setPage] = React.useState(1);
  const [pageSize, setPageSize] = React.useState(PAGE_SIZE);
  const [loading, setLoading] = React.useState(false);
  const [busy, setBusy] = React.useState(false);
  const [sort, setSort] = useStoredSortPreference<OwnedSortField>(
    "artex_owned_sort",
    SORT_FIELDS,
    "last_verified",
    "desc",
  );
  const requestRef = React.useRef(0);

  const load = React.useCallback(async (s: string) => {
    const request = ++requestRef.current;
    setLoading(true);
    try {
      const next = await p2api.owned(s === "all" ? undefined : s);
      if (request !== requestRef.current) return;
      setData(next);
    } catch (e) {
      if (request !== requestRef.current) return;
      toast.error(`加载持有台账失败：${(e as Error).message}`);
    } finally {
      if (request === requestRef.current) setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void load(status);
  }, [status, load]);

  // 筛选/排序变化都会换一份结果集，回到第一页。
  // biome-ignore lint/correctness/useExhaustiveDependencies: search/status/sort 只作为重置分页的触发源。
  React.useEffect(() => {
    setPage(1);
  }, [search, status, sort]);

  async function recheck() {
    setBusy(true);
    try {
      const r = await p2api.recheckOwned();
      toast.success(
        `复查完成：检查 ${r.result.checked ?? 0} 台，存活 ${r.result.ok ?? 0}，失效 ${r.result.failed ?? 0}，跳过 ${r.result.skipped ?? 0}`,
      );
      await load(status);
    } catch (e) {
      toast.error(`复查失败：${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }

  async function download() {
    try {
      const token = typeof window !== "undefined" ? localStorage.getItem("artex_token") : null;
      const r = await fetch("/api/owned/export", {
        headers: token ? { Authorization: `Bearer ${token}` } : {},
      });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      const blob = await r.blob();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "artex-ledger.json";
      a.click();
      URL.revokeObjectURL(url);
      toast.success("已开始下载台账 JSON");
    } catch (e) {
      toast.error(`导出失败：${(e as Error).message}`);
    }
  }

  const stats = data?.stats ?? {};

  const rows = React.useMemo(() => {
    const q = search.trim().toLowerCase();
    const list = (data?.assets ?? []).filter((o) => {
      // 后端已按状态过滤；这里再卡一道，切换状态时不会闪回旧状态的行。
      if (status !== "all" && o.status !== status) return false;
      if (!q) return true;
      const haystack = [assetLabel(o), o.access_type, o.privilege, statusLabel(o.status), o.evidence, o.notes]
        .filter(Boolean)
        .join(" ")
        .toLowerCase();
      return haystack.includes(q);
    });
    const sortField = sort.field;
    list.sort((a, b) => {
      if (isDateField(sortField)) {
        const ta = dateOf(a, sortField);
        const tb = dateOf(b, sortField);
        // 缺失时间永远排最后，不随排序方向翻转。
        if (ta === null || tb === null) {
          if (ta === tb) return 0;
          return ta === null ? 1 : -1;
        }
        return sort.direction === "asc" ? ta - tb : tb - ta;
      }
      const cmp = textOf(a, sortField).localeCompare(textOf(b, sortField), "zh-Hans-CN");
      return sort.direction === "asc" ? cmp : -cmp;
    });
    return list;
  }, [data, search, status, sort]);

  React.useEffect(() => {
    const lastPage = Math.max(1, Math.ceil(rows.length / pageSize));
    if (page > lastPage) setPage(lastPage);
  }, [rows.length, page, pageSize]);

  const pageRows = React.useMemo(() => rows.slice((page - 1) * pageSize, page * pageSize), [rows, page, pageSize]);

  function toggleSort(field: OwnedSortField) {
    setSort((current) =>
      current.field === field
        ? { field, direction: current.direction === "asc" ? "desc" : "asc" }
        : { field, direction: isDateField(field) ? "desc" : "asc" },
    );
  }

  const statEntries = React.useMemo(() => {
    const rank = (key: string) => {
      const index = STAT_ORDER.indexOf(key);
      return index === -1 ? STAT_ORDER.length : index;
    };
    return Object.entries(stats).sort((a, b) => rank(a[0]) - rank(b[0]) || a[0].localeCompare(b[0]));
  }, [stats]);

  const filtering = search.trim() !== "" || status !== "all";

  return (
    <div className="flex flex-1 flex-col gap-4 md:gap-6">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-lg font-semibold">持有台账</h1>
          <p className="text-sm text-muted-foreground">打下来的资产跨任务保留；到期自动复查，改密/关站会标 stale。</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" disabled={busy} onClick={() => void recheck()}>
            <RefreshCwIcon className={busy ? "animate-spin" : undefined} />
            {busy ? "复查中…" : "现在复查一遍"}
          </Button>
          <Button size="sm" variant="outline" onClick={() => void download()}>
            <DownloadIcon /> 导出 JSON
          </Button>
        </div>
      </div>

      {statEntries.length > 0 ? (
        <div className="grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-6">
          {statEntries.map(([key, value]) => (
            <Card key={key} className="gap-1 py-4">
              <CardHeader className="px-4">
                <CardDescription>{key === "total" ? "总计" : statusLabel(key)}</CardDescription>
                <CardTitle className="text-2xl tabular-nums">{value}</CardTitle>
              </CardHeader>
            </Card>
          ))}
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        <div className="relative w-full sm:w-72">
          <SearchIcon
            className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground"
            aria-hidden="true"
          />
          <Input
            type="search"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="搜索资产 / 权限 / 证据"
            aria-label="搜索持有台账"
            className="pl-8"
          />
        </div>
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger size="sm" className="w-32">
            <SelectValue placeholder="状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            {STATUS_OPTIONS.map((s) => (
              <SelectItem key={s} value={s}>
                {statusLabel(s)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <span className="ml-auto text-xs text-muted-foreground tabular-nums">共 {rows.length} 条</span>
      </div>

      <Card className="gap-0 py-0">
        {/* 首次加载、或切筛选后新结果尚未到达（旧数据被筛空）时都显示骨架屏。 */}
        {loading && (data === null || rows.length === 0) ? (
          <CardContent className="flex flex-col gap-2 px-4 py-4">
            {SKELETON_ROWS.map((key) => (
              <Skeleton key={key} className="h-6 w-full" />
            ))}
          </CardContent>
        ) : (
          <>
            {rows.length === 0 ? (
              <CardContent className="px-4 py-4">
                <Empty className="border border-dashed">
                  <EmptyHeader>
                    <EmptyTitle>{filtering ? "没有匹配的持有记录" : "还没有持有记录"}</EmptyTitle>
                    <EmptyDescription>
                      {filtering
                        ? "调整搜索关键词或状态筛选后再试。"
                        : "worker 打下 shell/后台后会用 owned_register 自动登记。"}
                    </EmptyDescription>
                  </EmptyHeader>
                </Empty>
              </CardContent>
            ) : null}
            {rows.length > 0 ? (
              <CardContent className="px-0">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <SortableHead
                        field="asset"
                        label="资产"
                        activeField={sort.field}
                        direction={sort.direction}
                        onSort={toggleSort}
                      />
                      <TableHead>类型</TableHead>
                      <SortableHead
                        field="privilege"
                        label="权限"
                        activeField={sort.field}
                        direction={sort.direction}
                        onSort={toggleSort}
                      />
                      <TableHead>跳数</TableHead>
                      <SortableHead
                        field="status"
                        label="状态"
                        activeField={sort.field}
                        direction={sort.direction}
                        onSort={toggleSort}
                        className="w-24"
                      />
                      <SortableHead
                        field="last_verified"
                        label="最近验证"
                        activeField={sort.field}
                        direction={sort.direction}
                        onSort={toggleSort}
                        className="w-44"
                      />
                      <SortableHead
                        field="next_check"
                        label="下次复查"
                        activeField={sort.field}
                        direction={sort.direction}
                        onSort={toggleSort}
                        className="w-44"
                      />
                      <TableHead>证据/备注</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {pageRows.map((o) => (
                      <TableRow key={o.id}>
                        <TableCell className="font-mono text-xs font-medium">{assetLabel(o)}</TableCell>
                        <TableCell className="text-xs">{o.access_type || "—"}</TableCell>
                        <TableCell className="text-xs">{o.privilege || "—"}</TableCell>
                        <TableCell className="text-xs tabular-nums">{o.depth}</TableCell>
                        <TableCell>
                          <Badge variant="outline" className={statusTone[o.status] ?? ""}>
                            {statusLabel(o.status)}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground tabular-nums">
                          {fmt(o.last_verified_at)}
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground tabular-nums">
                          {fmt(o.next_check_at)}
                        </TableCell>
                        <TableCell className="max-w-[280px] truncate text-xs" title={o.evidence || o.notes || ""}>
                          {o.evidence || o.notes || "—"}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <TablePagination
                  page={page}
                  pageSize={pageSize}
                  total={rows.length}
                  onPageChange={setPage}
                  onPageSizeChange={(nextSize) => {
                    setPageSize(nextSize);
                    setPage(1);
                  }}
                  pageSizeOptions={[10, 20, 50, 100]}
                />
              </CardContent>
            ) : null}
          </>
        )}
      </Card>
    </div>
  );
}
