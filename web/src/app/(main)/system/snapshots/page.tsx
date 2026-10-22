"use client";

import * as React from "react";
import { HistoryIcon, Loader2Icon, RotateCcwIcon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { api, type SnapshotRow } from "@/lib/api";

function fmtTime(ts: unknown) {
  if (!ts) return "-";
  const d = new Date(ts as string);
  if (Number.isNaN(d.getTime())) return "-";
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function fmtBytes(n: number) {
  if (!n) return "0 B";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

// 快照回滚页 —— 对应 internal/snapshot。
//
// 为什么需要这一页：写文件类工具在写之前会留一份档（Create 一直在跑），但 Restore
// 此前全仓零调用 —— 「可回滚」只是句口号。现在接出来了：这里能列出每一次"写前留档"，
// 并把某个历史状态写回原路径。
//
// 纪律：回滚是破坏性动作（覆盖当前文件），所以按钮要二次确认，且后端还要求
// body 带 {"confirm": true}；回滚前系统会自动再留一份档，误操作也能退回来。
export default function SnapshotsPage() {
  const [rows, setRows] = React.useState<SnapshotRow[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [busy, setBusy] = React.useState<string | null>(null);
  const [taskFilter, setTaskFilter] = React.useState("");

  const load = React.useCallback(async () => {
    setLoading(true);
    try {
      const tid = taskFilter.trim() ? Number(taskFilter.trim()) : undefined;
      const res = await api.snapshots(Number.isFinite(tid as number) ? tid : undefined, 200);
      setRows(res.snapshots);
    } catch (e) {
      toast.error("加载快照失败：" + (e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [taskFilter]);

  React.useEffect(() => {
    void load();
  }, [load]);

  const restore = async (row: SnapshotRow) => {
    const ok = window.confirm(
      `把「${row.reason || row.id}」这份快照写回原路径？\n\n` +
        `涉及 ${row.files} 个文件，共 ${fmtBytes(row.bytes)}。\n` +
        `当前文件内容会被覆盖（系统会先为当前状态再留一份档，可以退回来）。`,
    );
    if (!ok) return;
    setBusy(row.id);
    try {
      const res = await api.restoreSnapshot(row.id);
      toast.success(`已回滚 ${res.restored} 个文件`);
      await load();
    } catch (e) {
      toast.error("回滚失败：" + (e as Error).message);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div className="flex flex-col gap-4 p-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="flex items-center gap-2 text-xl font-semibold">
            <HistoryIcon className="size-5" /> 快照回滚
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">
            写文件类工具在动你的文件之前会自动留一份档。这里能看到每一次留档，并把某个历史状态写回原路径。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Input
            className="w-36"
            placeholder="任务 ID（可空）"
            value={taskFilter}
            onChange={(e) => setTaskFilter(e.target.value)}
          />
          <Button variant="outline" size="sm" onClick={() => void load()} disabled={loading}>
            <RefreshCwIcon className={loading ? "size-4 animate-spin" : "size-4"} /> 刷新
          </Button>
        </div>
      </div>

      <Card className="overflow-hidden">
        {loading ? (
          <div className="flex items-center gap-2 p-6 text-sm text-muted-foreground">
            <Loader2Icon className="size-4 animate-spin" /> 正在加载…
          </div>
        ) : rows.length === 0 ? (
          <div className="p-6 text-sm text-muted-foreground">
            还没有快照。等任务里出现一次「写文件 / 改配置」动作，这里就会出现一条记录。
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-[150px]">时间</TableHead>
                <TableHead className="w-[80px]">任务</TableHead>
                <TableHead>原因 / 涉及文件</TableHead>
                <TableHead className="w-[110px]">大小</TableHead>
                <TableHead className="w-[110px]">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.id}>
                  <TableCell className="whitespace-nowrap text-xs">{fmtTime(row.created_at)}</TableCell>
                  <TableCell className="text-xs">{row.task_id > 0 ? `#${row.task_id}` : "-"}</TableCell>
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Badge variant="secondary">{row.files} 个文件</Badge>
                      <span className="text-sm">{row.reason || "(未标注原因)"}</span>
                    </div>
                    {row.sample_paths.length > 0 && (
                      <div className="mt-1 font-mono text-[11px] text-muted-foreground">
                        {row.sample_paths.map((p) => (
                          <div key={p} className="truncate">
                            {p}
                          </div>
                        ))}
                        {row.files > row.sample_paths.length && <div>…还有 {row.files - row.sample_paths.length} 个</div>}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="text-xs">{fmtBytes(row.bytes)}</TableCell>
                  <TableCell>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={busy === row.id}
                      onClick={() => void restore(row)}
                    >
                      {busy === row.id ? (
                        <Loader2Icon className="size-4 animate-spin" />
                      ) : (
                        <RotateCcwIcon className="size-4" />
                      )}
                      回滚
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Card>

      <p className="text-xs text-muted-foreground">
        {rows.length} 条记录（最多显示 200 条）。启动时会自动清理，只保留最近 500 份。
      </p>
    </div>
  );
}
