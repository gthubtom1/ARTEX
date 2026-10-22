import {
  Activity,
  Ban,
  BookOpen,
  Cpu,
  Database,
  FileText,
  ListChecks,
  BellRing,
  CalendarClock,
  Bot,
  Brain,
  Bug,
  ClipboardList,
  FolderOpen,
  FolderSync,
  Gauge,
  History,
  LayoutDashboard,
  type LucideIcon,
  MessageSquare,
  Network,
  Plug,
  Wifi,
  Radio,
  Rocket,
  ScrollText,
  Settings2,
  ShieldAlert,
  ShieldCheck,
  Sparkles,
  Target,
  Terminal,
  Wrench,
} from "lucide-react";

export type NavBadge = "new" | "soon";

export interface NavSubItem {
  id: string;
  title: string;
  url: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

interface NavItemBase {
  id: string;
  title: string;
  icon?: LucideIcon;
  badge?: NavBadge;
  disabled?: boolean;
  newTab?: boolean;
}

export interface NavMainLinkItem extends NavItemBase {
  url: string;
  subItems?: never;
}

export interface NavMainParentItem extends NavItemBase {
  subItems: NavSubItem[];
}

export type NavMainItem = NavMainLinkItem | NavMainParentItem;

export interface NavGroup {
  id: number;
  label?: string;
  items: NavMainItem[];
}

export const sidebarItems: NavGroup[] = [
  {
    id: 1,
    label: "功能",
    items: [
      { id: "dashboard", title: "仪表盘", url: "/dashboard", icon: LayoutDashboard },
      { id: "chat", title: "对话", url: "/chat", icon: MessageSquare },
      { id: "tasks", title: "任务", url: "/function/tasks", icon: Target },
      { id: "schedules", title: "计划任务", url: "/function/schedules", icon: CalendarClock },
      { id: "assets", title: "资产", url: "/function/assets", icon: Network },
      { id: "findings", title: "发现", url: "/function/findings", icon: Bug },
      { id: "capabilities", title: "能力菜单", url: "/function/capabilities", icon: ListChecks },
      { id: "owned", title: "持有台账", url: "/function/owned", icon: Database },
      { id: "data", title: "数据定位", url: "/function/data", icon: Database },
      { id: "report", title: "报告", url: "/function/report", icon: FileText },
      { id: "knowledge", title: "知识库", url: "/function/knowledge", icon: BookOpen },
      { id: "traffic", title: "流量", url: "/function/traffic", icon: Activity },
      { id: "commands", title: "工具执行", url: "/function/commands", icon: Terminal },
      { id: "llm-records", title: "LLM 录制", url: "/function/llm-records", icon: Radio },
      { id: "sync", title: "资产同步", url: "/function/sync", icon: FolderSync },
      { id: "workspace", title: "工作空间", url: "/function/workspace", icon: FolderOpen },
    ],
  },
  {
    id: 2,
    label: "系统",
    items: [
      { id: "llm", title: "LLM", url: "/system/llm", icon: Brain },
      { id: "agents", title: "Agent", url: "/system/agents", icon: Bot },
      { id: "mcp", title: "MCP", url: "/system/mcp", icon: Plug },
      { id: "skills", title: "Skill", url: "/system/skills", icon: Sparkles },
      { id: "tools", title: "工具", url: "/system/tools", icon: Wrench },
      { id: "notify", title: "通知", url: "/system/notify", icon: BellRing },
      { id: "intercept", title: "拦截规则", url: "/system/intercept", icon: ShieldAlert },
      { id: "asset-intercept", title: "资产拦截", url: "/system/intercept/assets", icon: Ban },
      { id: "approvals", title: "审批记录", url: "/system/intercept/approvals", icon: ClipboardList },
      { id: "audit", title: "审计", url: "/system/audit", icon: ScrollText },
      { id: "snapshots", title: "快照回滚", url: "/system/snapshots", icon: History },
      { id: "environment", title: "环境", url: "/system/environment", icon: Cpu },
      { id: "network", title: "网络", url: "/system/network", icon: Wifi },
      { id: "replay", title: "回放", url: "/system/replay", icon: Activity },
      { id: "bench", title: "基准", url: "/system/bench", icon: Gauge },
      { id: "verifier", title: "复核", url: "/system/verifier", icon: ShieldCheck },
      { id: "logs", title: "日志", url: "/system/logs", icon: ScrollText },
      { id: "injection", title: "注入层", url: "/system/injection", icon: ShieldAlert },
      { id: "settings", title: "系统配置", url: "/system/settings", icon: Settings2 },
      { id: "onboard", title: "首次配置", url: "/system/onboard", icon: Rocket },
    ],
  },
];
