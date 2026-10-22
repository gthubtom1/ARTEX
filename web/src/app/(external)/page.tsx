"use client";

import * as React from "react";

// 静态导出模式下 next/navigation 的 redirect() 会生成错误页；
// 根路径改为客户端跳转：已登录进任务页，未登录由主布局守卫跳到 /login。
export default function Home() {
  React.useEffect(() => {
    window.location.replace("/function/tasks");
  }, []);
  return <div className="flex min-h-screen items-center justify-center text-sm text-muted-foreground">正在进入控制台…</div>;
}
