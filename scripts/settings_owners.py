#!/usr/bin/env python3
"""设置归属审计（settings ownership audit）。

存在的原因：同一个 setting 键被两个页面同时读写，就是"改了 A 被 B 覆盖"的隐性 bug。
2026-10-22 实际踩过两个，靠人眼看不出：

  1. 系统设置页 和「注入层」页 都能改 `inject.mode`；后者保存时把你选的
     always/on-miss/on-refusal 静默改回 hybrid。
  2. UI 只写 `global_proxy`，而 netguard 读 `net.proxy_http` —— 用户在界面上
     配好代理，netguard 依然读到空值 → 任务一建就 paused「出口未就绪」。

所以做成机检，而不是靠"下次记得"。

检查四件事：
  1. 同一个 setting 键被 **多个页面** 写入          → FAIL（真冲突）
     （写入点不限于 setSettings({...})，也包括页面自己的 saveXxx({...}) 小助手）
  2. 前端写了、putSettings 里没有对应 setting 键     → WARN（字段名对不上/后端没处理）
  3. 前端写了、后端从没读过                        → WARN（死设置）
  4. 已知语义别名（同概念两把钥匙）                  → WARN（人工维护清单）

用法：
    python scripts/settings_owners.py        # 结论
    python scripts/settings_owners.py -v     # 附每键的读写方清单
退出码：0 = 无 FAIL；1 = 有冲突
"""
from __future__ import annotations

import argparse
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WEB = os.path.join(ROOT, "web", "src")
GO_DIRS = ["server", "agent", "guard", "internal", "cmd"]

# 页面写设置不一定是直接 setSettings({...})：也可能包在自己的小助手函数里，
# 例如 system/settings 旧版的 `saveInject({ inject_mode: v })`。
# 所以除了 setSettings，还要抓 save*/set*/update*/patch* 后面紧跟对象字面量的调用。
# 只认 setSettings 和 save*/update*/patch* 这类"写入助手"。
# 刻意**不**匹配裸 set*：React 的 setState({...}) 满天飞，会把任意对象字面量当设置写入。
RE_SET_CALL = re.compile(
    r"(?:setSettings|(?:save|update|patch)[A-Za-z0-9_]*)\s*\(\s*\{", re.S
)

# 后端读 setting：settings.String("k" / s.settings.Int("k" / pg.GetSetting("k"
RE_GO_READ = re.compile(
    r'(?:settings\.(?:String|Int|Bool|List)\(\s*"([^"]+)"|GetSetting\(\s*"([^"]+)")'
)
# 后端写 setting：SetSetting("k" / SetBool("k" / InsertSettingIfAbsent("k"
RE_GO_WRITE = re.compile(
    r'(?:SetSetting|SetBool|InsertSettingIfAbsent)\(\s*"([^"]+)"'
)

# 已知语义别名（同一件事两把钥匙）。只放**确认过**的，人工维护。
ALIASES = [
    ("global_proxy", "net.proxy_http",
     "全局出口代理：UI 写 global_proxy，netguard 读 net.proxy_http（已加回落，但键名仍不统一）"),
]


def _balanced(src: str, open_brace: int):
    """从 open_brace 处的 '{' 起，返回配对的 {...} 内文本。"""
    depth = 0
    for i in range(open_brace, len(src)):
        ch = src[i]
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return src[open_brace + 1:i]
    return None


def _top_level_keys(body: str):
    """取出对象字面量的顶层 key（跳过嵌套 {} / [] 内的）。"""
    depth = 0
    at_start = True
    key_re = re.compile(r"([A-Za-z_][A-Za-z0-9_]*)")
    keys = []
    i = 0
    while i < len(body):
        ch = body[i]
        if ch in "{[":
            depth += 1
        elif ch in "}]":
            depth -= 1
        elif depth == 0 and ch in "\n,":
            at_start = True
            i += 1
            continue
        elif depth == 0 and at_start and (ch.isalpha() or ch == "_"):
            m = key_re.match(body, i)
            if m:
                rest = body[m.end():].lstrip()
                if rest.startswith(":"):
                    keys.append(m.group(1))
                i = m.end()
                at_start = False
                continue
        if depth == 0 and ch not in " \t":
            at_start = False
        i += 1
    return keys


def scan_frontend():
    """返回 {payload 字段名: set(页面相对路径)}。"""
    owners: dict[str, set[str]] = {}
    for dirpath, _dirnames, filenames in os.walk(WEB):
        if "node_modules" in dirpath:
            continue
        for fn in filenames:
            if not fn.endswith((".tsx", ".ts")):
                continue
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, ROOT).replace("\\", "/")
            try:
                src = open(path, encoding="utf-8").read()
            except OSError:
                continue
            for m in RE_SET_CALL.finditer(src):
                body = _balanced(src, m.end() - 1)
                if body is None:
                    continue
                for key in _top_level_keys(body):
                    owners.setdefault(key, set()).add(rel)
    return owners


def scan_go():
    """收集 setting 的读写方。常量形式（GetBool(settingXxx)）也要解析。"""
    consts = setting_consts()
    reads: dict[str, set[str]] = {}
    writes: dict[str, set[str]] = {}
    for d in GO_DIRS:
        base = os.path.join(ROOT, d)
        if not os.path.isdir(base):
            continue
        for dirpath, _dirnames, filenames in os.walk(base):
            for fn in filenames:
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                path = os.path.join(dirpath, fn)
                rel = os.path.relpath(path, ROOT).replace("\\", "/")
                try:
                    src = open(path, encoding="utf-8").read()
                except OSError:
                    continue
                for m in RE_GO_READ.finditer(src):
                    key = m.group(1) or m.group(2)
                    if key:
                        reads.setdefault(key, set()).add(rel)
                # 常量形式：GetBool/GetSetting(settingXxx) —— 不解析常量会把它们误报成"死设置"
                for m in re.finditer(
                    r'Get(?:Bool|Int|String|Setting)\(\s*([Ss]etting[A-Za-z0-9_]+)', src
                ):
                    key = consts.get(m.group(1))
                    if key:
                        reads.setdefault(key, set()).add(rel)
                for m in RE_GO_WRITE.finditer(src):
                    writes.setdefault(m.group(1), set()).add(rel)
                for m in re.finditer(
                    r'Set(?:Bool|Int|Setting)\(\s*([Ss]etting[A-Za-z0-9_]+)', src
                ):
                    key = consts.get(m.group(1))
                    if key:
                        writes.setdefault(key, set()).add(rel)
    return reads, writes


def setting_consts() -> dict[str, str]:
    """收集 `settingXxx = "literal"` 常量，供 SetBool(settingXxx) 这类调用解析。"""
    out: dict[str, str] = {}
    for d in GO_DIRS:
        base = os.path.join(ROOT, d)
        if not os.path.isdir(base):
            continue
        for dirpath, _dirnames, filenames in os.walk(base):
            for fn in filenames:
                if not fn.endswith(".go") or fn.endswith("_test.go"):
                    continue
                try:
                    src = open(os.path.join(dirpath, fn), encoding="utf-8").read()
                except OSError:
                    continue
                for m in re.finditer(r'([Ss]etting[A-Za-z0-9_]+)\s*=\s*"([^"]+)"', src):
                    out[m.group(1)] = m.group(2)
    return out


def field_to_setting_map():
    """解析「前端 payload 字段 → 后端目的地」。

    目的地有三种：
      - 真实 setting 键（SetSetting("x"） / SetBool(settingX) 常量）
      - "@manager:SetXxx"（走 Manager 的方法，不是 settings 表）
      - 解析不出来 → 不进 map（上游会报 unmapped）

    做法：按出现顺序扫描 req.<Field> 与各种写入调用，把每个 req 字段配给
    **它之后最近的那个**写入。不依赖 AST，够用。
    """
    consts = setting_consts()
    out: dict[str, str] = {}
    for path in sorted(
        os.path.join(dirpath, fn)
        for d in GO_DIRS
        if os.path.isdir(os.path.join(ROOT, d))
        for dirpath, _dirnames, filenames in os.walk(os.path.join(ROOT, d))
        for fn in filenames
        if fn.endswith(".go") and not fn.endswith("_test.go")
    ):
        try:
            src = open(path, encoding="utf-8").read()
        except OSError:
            continue
        pat = re.compile(
            r'req\.([A-Za-z0-9_]+)'
            r'|(?:SetSetting|SetBool|SetInt)\(\s*(?:"([^"]+)"|([Ss]etting[A-Za-z0-9_]+))'
            r'|s\.m\.(Set[A-Za-z0-9_]+)\('
        )
        pending = None
        for m in pat.finditer(src):
            field, lit, const, mgr = m.group(1), m.group(2), m.group(3), m.group(4)
            if field:
                pending = field
                continue
            if pending is None:
                continue
            key = lit or (consts.get(const, "") if const else "")
            if not key and mgr:
                key = "@manager:" + mgr
            if key:
                out.setdefault(pending, key)
            pending = None
    out.setdefault("GlobalProxy", "@manager:SetGlobalProxy")
    return out


def snake_to_camel(s: str) -> str:
    return "".join(part.capitalize() for part in s.split("_"))


def norm_key(s: str) -> str:
    """把 payload 字段名 / Go 结构体字段名归一：去掉下划线并转小写。
    InjectMode / inject_mode / INJECT_MODE 都归一成 injectmode，避免 LLMRecord 这类缩写对不上。
    """
    return s.replace("_", "").lower()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("-v", "--verbose", action="store_true")
    args = ap.parse_args()

    fe = scan_frontend()
    go_reads, go_writes = scan_go()
    f2s = field_to_setting_map()

    # 按 **setting 键** 汇总写入页（而不是按 payload 字段名），
    # 这样「两个页面用不同字段名写同一个 setting」也能抓到。
    # payload 字段（snake_case）→ Go 结构体字段（CamelCase）；再用归一化兜一层，
    # 防止 LLMRecord 这种缩写对不上。
    by_norm = {norm_key(k): v for k, v in f2s.items()}

    def setting_of(field: str) -> str | None:
        return f2s.get(snake_to_camel(field)) or by_norm.get(norm_key(field))

    owners_by_setting: dict[str, set[str]] = {}
    for field, pages in fe.items():
        key = setting_of(field)
        if not key:
            continue
        owners_by_setting.setdefault(key, set()).update(pages)
    # @manager:* 不是 settings 键（不参与冲突判定），但要算作"已映射"
    mapped_fields = {f for f in fe if setting_of(f)}
    conflicts = {k: v for k, v in owners_by_setting.items() if len(v) > 1}

    # dead 要按 **setting 键** 判定（前面 fe 是 payload 字段名，两个命名空间不能混）
    KNOWN_MANAGED = {
        "global_proxy": "由 Manager.GlobalProxy() 读，不走 settings 包",
    }
    dead = sorted(
        k for k in owners_by_setting
        if not k.startswith("@manager:")
        and k not in go_reads
        and k not in KNOWN_MANAGED
    )
    unmapped = sorted(k for k in fe if not setting_of(k))
    back_only = sorted(k for k in go_reads if k not in fe)

    print(f"[scope] 前端写入字段 {len(fe)} 个（映射到 setting {len(owners_by_setting)} 个）"
          f" / 后端读取 setting {len(go_reads)} 个")
    print()

    if conflicts:
        print("[FAIL] 同一个 setting 键被多个页面写入（会互相覆盖）：")
        for k, pages in sorted(conflicts.items()):
            print(f"  - {k}")
            for p in sorted(pages):
                print(f"      {p}")
    else:
        print("[ok] 没有「一个 setting 键、多个写入页」的冲突")

    if unmapped:
        print()
        print("[warn] 前端写了、但 putSettings 里找不到对应 setting 键（字段名对不上或后端没处理）：")
        for k in unmapped:
            print(f"  - {k}  ← {', '.join(sorted(fe[k]))}")

    if dead:
        print()
        print("[warn] 前端写了、后端从没读过（死设置，要么删掉、要么接上）：")
        for k in dead:
            print(f"  - {k}  ← {', '.join(sorted(owners_by_setting[k]))}")

    hits = [a for a in ALIASES if a[0] in fe or a[0] in go_reads]
    if hits:
        print()
        print("[warn] 已知语义别名（同一概念两把钥匙）：")
        for a, b, why in hits:
            print(f"  - {a}  vs  {b}   {why}")

    if back_only:
        print()
        print(f"[info] 后端读取、但前端没有写入口（只能改配置文件/环境变量）：{len(back_only)} 个")
        if args.verbose:
            for k in back_only:
                print(f"  - {k}  ({', '.join(sorted(go_reads[k]))})")
        else:
            print("      加 -v 看清单")

    if args.verbose:
        print()
        print("[全部前端写入字段 → setting 键 → 写入页]")
        for k in sorted(fe):
            print(f"  {k:32s} → {setting_of(k) or '(未映射)':28s} {', '.join(sorted(fe[k]))}")

    print()
    print("AUDIT_RESULT", "FAIL" if conflicts else "PASS")
    return 1 if conflicts else 0


if __name__ == "__main__":
    sys.exit(main())
