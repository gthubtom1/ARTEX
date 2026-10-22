#!/usr/bin/env python3
"""设置契约审计：后端 settingsPayload() 回显的键 ↔ 前端 Settings 接口。

存在的原因（2026-10-22 实测）：
后端把 cred_key_fingerprint / inject_preset / findings_verify_gate 等加进 payload，
前端 `types.ts` 的 Settings 接口没跟上，于是每个新页面都得自己写交叉类型绕过去
（/system/onboard 就写了 `Settings & { cred_key_fingerprint?: string }`）。
这类漂移靠人查不出来，做成机检。

规则：
  - 后端回显、前端未声明 → FAIL（新页面必须 hack，迟早写错）
  - 前端声明、后端不回显 → WARN（死字段，会让人以为能读到）
  - Go 常量形式的键要解析（settingXxx / SettingXxx = "literal"）

用法：python scripts/settings_contract.py [-v]
退出码：0 = 无 FAIL
"""
from __future__ import annotations

import argparse
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def go_setting_consts() -> dict[str, str]:
    out: dict[str, str] = {}
    for d in ("server", "db", "internal", "agent"):
        base = os.path.join(ROOT, d)
        if not os.path.isdir(base):
            continue
        for dirpath, _dn, fns in os.walk(base):
            for fn in fns:
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                try:
                    src = open(os.path.join(dirpath, fn), encoding="utf-8").read()
                except OSError:
                    continue
                for m in re.finditer(r'([Ss]etting[A-Za-z0-9_]+)\s*=\s*"([^"]+)"', src):
                    out[m.group(1)] = m.group(2)
    return out


def backend_keys() -> set[str]:
    consts = go_setting_consts()
    src = open(os.path.join(ROOT, "server", "server.go"), encoding="utf-8").read()
    i = src.find("func (s *Server) settingsPayload() map[string]any {")
    if i < 0:
        raise SystemExit("settingsPayload not found")
    j = src.find("\n}", i)
    body = src[i:j]
    keys: set[str] = set()
    # "literal": value
    keys.update(re.findall(r'"([A-Za-z0-9_.]+)"\s*:', body))
    # <const>: value  （形如 db.SettingCredentialAgent:）
    for m in re.finditer(r'(?:[A-Za-z0-9_]+\.)?([Ss]etting[A-Za-z0-9_]+)\s*:', body):
        if m.group(1) in consts:
            keys.add(consts[m.group(1)])
    return keys


def frontend_keys() -> set[str]:
    ts = open(os.path.join(ROOT, "web", "src", "lib", "types.ts"), encoding="utf-8").read()
    m = re.search(r"export interface Settings \{([\s\S]*?)\n\}", ts)
    if not m:
        raise SystemExit("Settings interface not found in web/src/lib/types.ts")
    return set(re.findall(r"^\s*([a-zA-Z_][a-zA-Z0-9_]*)\??:", m.group(1), re.M))


def frontend_write_only() -> set[str]:
    """收集前端 setSettings({...}) 里出现过的键 —— 这些是**只写不读**的请求字段
    （例如 brave_search_api_key：后端只回 brave_key_set 布尔）。它们不该被当成死字段。"""
    keys: set[str] = set()
    web = os.path.join(ROOT, "web", "src")
    for dirpath, _dn, fns in os.walk(web):
        if "node_modules" in dirpath:
            continue
        for fn in fns:
            if not fn.endswith((".ts", ".tsx")):
                continue
            try:
                src = open(os.path.join(dirpath, fn), encoding="utf-8").read()
            except OSError:
                continue
            for m in re.finditer(r"setSettings\s*\(\s*\{", src):
                # 取到配对的 }
                i, depth = m.end() - 1, 0
                for j in range(i, len(src)):
                    if src[j] == "{":
                        depth += 1
                    elif src[j] == "}":
                        depth -= 1
                        if depth == 0:
                            body = src[i + 1:j]
                            keys.update(re.findall(r"([A-Za-z_][A-Za-z0-9_]*)\s*:", body))
                            break
    return keys


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args()

    be = backend_keys()
    fe = frontend_keys()
    missing = sorted(be - fe)
    write_only = frontend_write_only()
    dead = sorted(k for k in (fe - be) if k not in write_only)
    extra = dead  # 只把"既不回显、也不写入"的算死字段

    print(f"[scope] 后端回显 {len(be)} 键 / 前端 Settings 声明 {len(fe)} 键")

    if missing:
        print()
        print("[FAIL] 后端回显、前端 Settings 未声明（新页面只能自己写交叉类型绕过）：")
        for k in missing:
            print("  -", k)
    else:
        print()
        print("[ok] 后端回显的键都在前端 Settings 里声明了")

    if extra:
        print()
        print("[warn] 前端声明了、后端不回显（死字段，读出来永远是 undefined）：")
        for k in extra:
            print("  -", k)

    if args.verbose:
        print()
        print("[后端全部键]", sorted(be))

    rc2 = check_second_round()
    if rc2:
        missing = missing + ["(见上方第二轮 FAIL)"]

    print()
    print("AUDIT_RESULT", "FAIL" if missing else "PASS")
    return 1 if missing else 0


# ─────────────────────────────────────────────────────────────
# 第二轮检查（R2 契约审查指出的两类盲区）
#
# 盲区 1：只查"键在不在"，不查"能不能写"。
#   实例：adviser_enabled 在前端 types.ts 里有、后端 payload 不返回、putSettings 也不认
#   -> 界面上的勾选框点了等于没点。
# 盲区 2：只查"前端声明/后端回显"，不查"有默认值但没人读"的假旋钮。
#   实例：work.workers 有默认值、环境页会展示，但运行时读的是另一个键 workers。
# ─────────────────────────────────────────────────────────────

# 只读/派生键：由后端算出来回显，不通过 PUT 写入（有明确理由，不是遗漏）
READONLY_KEYS = {
    "cred_key_fingerprint": "由 creds.LoadOrCreateKey 算出，不是用户输入",
    "inject_default_persona": "内置人设正文，只读回显",
    "inject_default_persona_name": "内置人设展示名，只读",
    "brave_key_set": "只回显'是否已存 key'的布尔",
    "tavily_key_set": "同上",
    "web_search_enabled": "由 web_search_backend 等组合推导",
    "web_search_backend": "由搜索配置推导",
    "traffic_capture": "走专用开关接口/Manager，不经 putSettings",
    "agent_traffic_binding": "走 Manager",
    "task_intelligence_enabled": "走 db.SettingTaskIntelligence + Manager",
    "credential_agent_enabled": "走 db 常量 + Manager",
    "credential_traffic_binding": "同上",
    "llm_record": "走 Manager",
    "workers": "走 Manager.SetWorkers",
    "task_concurrency_enabled": "走 Manager.SetConcurrencyLimit",
    "task_concurrency_limit": "同上",
    "llm_pool_enabled": "走 Manager",
    "llm_pool_bind_fallback": "同上",
    "noa_compaction": "走 Manager",
    "python_interpreter": "由 seedPythonInterpreter 自动检测",
    "notify_enabled": "走 notify 专用接口",
    "notify_public_base_url": "走 notify 专用接口",
    "notify_digest_interval_min": "走 notify 专用接口",
    "web_search_proxy": "走 web search 专用接口",
    "intercept_auto_decide": "有 PUT 分支（req.InterceptAutoDecide），见下",
}


def norm(s: str) -> str:
    """把 setting 键与 payload 字段名归一：去掉点和下划线、转小写。"""
    return s.replace(".", "").replace("_", "").lower()


def writable_setting_keys() -> set[str]:
    """putSettings 里真正能写的 setting 键（含 req 字段 -> SetSetting 配对）。"""
    src = open(os.path.join(ROOT, "server", "server.go"), encoding="utf-8").read()
    i = src.find("func (s *Server) putSettings(")
    if i < 0:
        return set()
    j = src.find("\nfunc ", i + 10)
    body = src[i:j if j > 0 else len(src)]
    consts = go_setting_consts()
    keys: set[str] = set()
    for m in re.finditer(r'SetSetting\(\s*"([^"]+)"', body):
        keys.add(m.group(1))
    for m in re.finditer(r'Set(?:Bool|Int)\(\s*"([^"]+)"', body):
        keys.add(m.group(1))
    for m in re.finditer(r'Set(?:Bool|Int|Setting)\(\s*([Ss]etting[A-Za-z0-9_]+)', body):
        if consts.get(m.group(1)):
            keys.add(consts[m.group(1)])
    return keys


def default_keys() -> set[str]:
    """internal/settings/settings.go 的 Defaults 里的键。"""
    src = open(os.path.join(ROOT, "internal", "settings", "settings.go"), encoding="utf-8").read()
    i = src.find("Defaults = map[string]string{")
    if i < 0:
        return set()
    j = src.find("\n}", i)
    body = src[i:j]
    return set(re.findall(r'"([A-Za-z0-9_.]+)"\s*:', body))


def check_second_round() -> int:
    be = backend_keys()
    writable = writable_setting_keys()
    consts = go_setting_consts()

    # 规则 A：回显了但写不进去 -> 界面上的控件必然是死键
    dead_controls = []
    for k in sorted(be):
        if k in READONLY_KEYS:
            continue
        # payload 键 -> setting 键：多数同名，少数 payload 用下划线、setting 用点
        # 归一化比较：payload 用下划线（inject_max_chars），setting 用点（inject.max_chars），
        # 直接集合相减会全部误报 —— 这正是"审计脚本自己的盲区"。
        if norm(k) not in {norm(w) for w in writable}:
            dead_controls.append(k)

    # 规则 B：有默认值但全仓无人读 -> 假旋钮
    readers = set()
    # 复用 scan_go 的思路：直接扫
    for d in ("server", "agent", "guard", "internal", "cmd"):
        base = os.path.join(ROOT, d)
        if not os.path.isdir(base):
            continue
        for dirpath, _dn, fns in os.walk(base):
            for fn in fns:
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                try:
                    src = open(os.path.join(dirpath, fn), encoding="utf-8").read()
                except OSError:
                    continue
                for m in re.finditer(
                    r'(?:settings\.(?:Bool|Int|String|List)|Get(?:Bool|Int|String|Setting))\(\s*'
                    r'(?:"([^"]+)"|([Ss]etting[A-Za-z0-9_]+))', src):
                    key = m.group(1) or consts.get(m.group(2), "")
                    if key:
                        readers.add(key)
    KNOWN_FAKE_KEYS: dict[str, str] = {}  # 2026-10-23: 已清空，10 个假旋钮全部从 Defaults 删除
    fake_knobs = sorted(k for k in default_keys() if k not in readers and k not in KNOWN_FAKE_KEYS)

    fail = False
    if dead_controls:
        fail = True
        print()
        print("[FAIL] 后端回显了、但 putSettings 写不进去（界面控件是死键）：")
        for k in dead_controls:
            print("  -", k)
        print("      要么补 PUT 分支，要么加进 READONLY_KEYS 并写清为什么只读")
    else:
        print()
        print("[ok] 回显的每个可写键都有 PUT 入口")

    if fake_knobs:
        print()
        print("[warn] Defaults 里有默认值、但全仓无人读（假旋钮，会在环境页被当生效参数展示）：")
        for k in fake_knobs:
            print("  -", k)

    return 0 if not fail else 1



if __name__ == "__main__":
    sys.exit(main())


