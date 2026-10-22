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

    print()
    print("AUDIT_RESULT", "FAIL" if missing else "PASS")
    return 1 if missing else 0


if __name__ == "__main__":
    sys.exit(main())
