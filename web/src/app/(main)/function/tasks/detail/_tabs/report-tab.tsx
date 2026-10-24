"use client";

import * as React from "react";

import { CheckIcon, CopyIcon, FileTextIcon } from "lucide-react";
import { toast } from "sonner";

import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api, briefapi } from "@/lib/api";
import { copyText } from "@/lib/utils";

export function ReportTab({ taskId }: { taskId: string }) {
  const [report, setReport] = React.useState<string>("");
  const [loading, setLoading] = React.useState(true);
  const [copied, setCopied] = React.useState(false);
  // 战况快照（docs/26 P2）：报告头一行，保证"报告结论"和"战况卡"用的是同一套判定。
  const [war, setWar] = React.useState<{
    level: string;
    basis: string;
    has: string[];
    got: number;
    oneStep: number;
    noGain: number;
    stall: string;
  } | null>(null);

  React.useEffect(() => {
    let active = true;
    briefapi
      .get(taskId)
      .then((b) => {
        if (!active) return;
        const can = b.can ?? [];
        setWar({
          level: b.level_label,
          basis: b.basis,
          has: (b.has ?? []).map((h) => h.text),
          got: can.filter((c) => c.standing === "got").length,
          oneStep: can.filter((c) => c.standing === "one_step").length,
          noGain: can.filter((c) => c.standing === "no_gain").length,
          stall: b.stall && b.stall.level !== "ok" ? b.stall.advice : "",
        });
      })
      .catch(() => void 0);
    setLoading(true);
    api
      .report(taskId)
      .then((text) => {
        if (active) setReport(text);
      })
      .catch(() => {
        if (active) setReport("");
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [taskId]);

  async function copy() {
    if (!report) return;
    const ok = await copyText(report);
    if (ok) {
      setCopied(true);
      toast.success("已复制 Markdown");
      setTimeout(() => setCopied(false), 1500);
    } else {
      toast.error("复制失败，请手动选择文本复制");
    }
  }

  let content: React.ReactNode;
  if (loading) {
    content = (
      <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed py-16 text-muted-foreground text-sm">
        <FileTextIcon className="size-8 opacity-40" />
        加载中…
      </div>
    );
  } else if (report) {
    content = (
      <div className="max-h-[60vh] overflow-auto rounded-md border bg-muted/20 p-4">
        <Markdown text={report} />
      </div>
    );
  } else {
    content = (
      <div className="flex flex-col items-center justify-center gap-2 rounded-md border border-dashed py-16 text-muted-foreground text-sm">
        <FileTextIcon className="size-8 opacity-40" />
        暂无报告
      </div>
    );
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between">
        <CardTitle className="flex items-center gap-2 text-sm">
          <FileTextIcon className="size-4" /> 渗透测试报告（Markdown）
        </CardTitle>
        <div className="flex gap-2">
          {report && (
            <Button size="sm" variant="outline" onClick={copy}>
              {copied ? <CheckIcon /> : <CopyIcon />} 复制
            </Button>
          )}
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {war && (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 rounded border border-sky-500/30 bg-sky-500/[0.05] px-3 py-1.5 text-[11.5px]">
            <span className="text-[10px] text-muted-foreground">战况快照</span>
            <b>{war.level}</b>
            <span className="text-muted-foreground">{war.basis}</span>
            <span>
              已拿到 {war.has.length > 0 ? war.has.join(" / ") : "（暂无）"}
            </span>
            <span className="text-muted-foreground">
              洞：{war.got} 个已成立 · {war.oneStep} 个差一步
              {war.noGain > 0 ? ` · ${war.noGain} 个换不到` : ""}
            </span>
            {war.stall && <span className="text-amber-700 dark:text-amber-300">{war.stall}</span>}
          </div>
        )}
        {content}
      </CardContent>
    </Card>
  );
}
