"use client";

import * as React from "react";

import { CheckIcon } from "lucide-react";

import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

// 首次配置向导的单步卡片：序号圈 / 标题 / 状态徽章 / 说明 / 内容 / 底部操作区。
// 只负责展示，步骤推进与数据请求都由页面自己管理。
export interface OnboardStepProps {
  index: number;
  title: string;
  hint: string;
  active: boolean;
  done: boolean;
  children: React.ReactNode;
  footer?: React.ReactNode;
}

export function OnboardStep({ index, title, hint, active, done, children, footer }: OnboardStepProps) {
  return (
    <Card size="sm" className={cn("transition-shadow", active ? "ring-2 ring-primary/40" : undefined)}>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <span
            className={cn(
              "flex size-6 shrink-0 items-center justify-center rounded-full border text-xs font-semibold",
              done
                ? "border-emerald-500/50 bg-emerald-500/15 text-emerald-600 dark:text-emerald-400"
                : active
                  ? "border-primary bg-primary text-primary-foreground"
                  : "border-border text-muted-foreground",
            )}
          >
            {done ? <CheckIcon className="size-3.5" /> : index}
          </span>
          <span>{title}</span>
          {done ? (
            <Badge
              variant="outline"
              className="ml-auto border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
            >
              已完成
            </Badge>
          ) : active ? (
            <Badge variant="secondary" className="ml-auto">
              进行中
            </Badge>
          ) : null}
        </CardTitle>
        <CardDescription className="pl-8 text-xs">{hint}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">{children}</CardContent>
      {footer ? <CardFooter className="justify-end gap-2">{footer}</CardFooter> : null}
    </Card>
  );
}
