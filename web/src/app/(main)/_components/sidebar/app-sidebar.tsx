"use client";

import * as React from "react";

import Link from "next/link";

import { useShallow } from "zustand/react/shallow";

import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar";
import { APP_CONFIG } from "@/config/app-config";
import { useCurrentUser } from "@/hooks/use-current-user";
import { filterNavForBeginner, sidebarItems } from "@/navigation/sidebar/sidebar-items";
import { usePreferencesStore } from "@/stores/preferences/preferences-provider";

import { NavMain } from "./nav-main";
import { NavUser } from "./nav-user";
import { SidebarSupportCard } from "./sidebar-support-card";


// 零基础默认只露少数入口（38 → 8），其余进「高级」；选择记在 localStorage，不动路由与权限。
const NAV_ADV_KEY = "artex_nav_advanced";

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const [showAdvanced, setShowAdvanced] = React.useState(false);
  React.useEffect(() => {
    try {
      setShowAdvanced(localStorage.getItem(NAV_ADV_KEY) === "1");
    } catch {
      /* localStorage 不可用（隐私模式）：保持默认精简 */
    }
  }, []);
  const toggleAdvanced = React.useCallback(() => {
    setShowAdvanced((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(NAV_ADV_KEY, next ? "1" : "0");
      } catch {
        /* 忽略 */
      }
      return next;
    });
  }, []);
  const items = React.useMemo(() => filterNavForBeginner(sidebarItems, showAdvanced), [showAdvanced]);
  const currentUser = useCurrentUser();
  const { sidebarVariant, sidebarCollapsible, isSynced } = usePreferencesStore(
    useShallow((s) => ({
      sidebarVariant: s.sidebarVariant,
      sidebarCollapsible: s.sidebarCollapsible,
      isSynced: s.isSynced,
    })),
  );

  const variant = isSynced ? sidebarVariant : props.variant;
  const collapsible = isSynced ? sidebarCollapsible : props.collapsible;

  return (
    <Sidebar {...props} variant={variant} collapsible={collapsible}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton asChild>
              <Link prefetch={false} href="/function/tasks">
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img src="/logo.png" alt="ARTEX" width={40} height={40} className="shrink-0" />
                <span className="font-semibold text-base">{APP_CONFIG.name}</span>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        <NavMain items={items} />
        <button
          type="button"
          onClick={toggleAdvanced}
          className="mx-2 mt-2 rounded px-2 py-1 text-left text-[11.5px] text-muted-foreground hover:bg-accent hover:text-foreground"
          title="还有 30 个进阶入口：资产同步 / 流量 / 工具执行 / 拦截规则 / 审计 …"
        >
          {showAdvanced ? "收起高级入口" : `高级入口（还有 ${sidebarItems.reduce((n, g) => n + g.items.length, 0) - 8} 项）`}
        </button>
        {/* <NavDocuments items={data.documents} /> */}
        {/* <NavSecondary items={data.navSecondary} className="mt-auto" /> */}
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={currentUser} />
      </SidebarFooter>
    </Sidebar>
  );
}
