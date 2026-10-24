"use client";

import * as React from "react";

import { CheckIcon, CopyIcon, FileTextIcon } from "lucide-react";
import { toast } from "sonner";

import { Markdown } from "@/components/markdown";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { api, briefapi, onepagerapi } from "@/lib/api";
import { copyText } from "@/lib/utils";

export function ReportTab({ taskId }: { taskId: string }) {
  const [report, setReport] = React.useState<string>("");
  const [loading, setLoading] = React.useState(true);
  const [copied, setCopied] = React.useState(false);
  // 委托方一页纸：拿到东西之后唯一能交出去的那一页（能进哪/拿到什么/影响多大/还差什么/怎么修）
  const [onePager, setOnePager] = React.useState<import("@/lib/types").OnePager | null>(null);
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
    onepagerapi
      .get(taskId)
      .then((p) => {
        if (active) setOnePager(p);
      })
      .catch(() => void 0);
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
        {/* 委托方一页纸（放在技术报告上面：给零基础的人/给客户看的就是这一块） */}
        {onePager && (
          <div className="rounded-md border border-emerald-500/40 bg-emerald-500/[0.04]">
            <div className="flex flex-wrap items-center gap-2 border-b border-emerald-500/30 px-3 py-1.5">
              <b className="text-[12px]">委托方一页纸</b>
              <span className="text-[10.5px] text-muted-foreground">
                能进哪 / 拿到什么 / 影响多大 / 还差什么 / 怎么修 —— 可直接发给客户，无术语
              </span>
              <Button
                size="sm"
                variant="outline"
                className="ml-auto"
                onClick={async () => {
                  const ok = await copyText(onePager.text);
                  toast[ok ? "success" : "error"](ok ? "已复制一页纸纯文本" : "复制失败，请手动选择");
                }}
              >
                <CopyIcon /> 复制纯文本
              </Button>
            </div>
            <pre className="max-h-[40vh] overflow-auto px-3 py-2 text-[11.5px] leading-relaxed whitespace-pre-wrap">
              {onePager.text}
            </pre>
          </div>
        )}
        {content}
      </CardContent>
    </Card>
  );
}
