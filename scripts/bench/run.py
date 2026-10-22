#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""ARTEX 内置基准跑批器（P8-1）。

串行跑 bench/cases/*.yaml：建任务 → 轮询 → 拉 findings → 按 pass 规则判定
→ 写 bench/results/<run_key>.json → POST /api/bench/ingest 登记。

只用标准库（urllib/json/argparse）；yaml 优先用 PyYAML，装不上时退回本文件里的
极简解析器（只支持本仓库用例用到的 YAML 子集）。

示例：
  python scripts/bench/run.py --dry-run --set smoke
  python scripts/bench/run.py --set basic --profile 3 --token "$ARTEX_COOKIE"
  python scripts/bench/run.py --set smoke --limit 2 --cleanup

安全：用例 url 必须是回环地址（127.0.0.1/localhost）；要打别的地址得显式
加 --allow-remote。这个脚本是给本地靶场用的，不是扫描器。
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

try:  # PyYAML 是可选依赖；没有也能跑（走 fallback）。
    import yaml  # type: ignore
except Exception:  # pragma: no cover - 取决于运行环境
    yaml = None

CASES_DIR = os.path.join("bench", "cases")
RESULTS_DIR = os.path.join("bench", "results")
# 日志统一走 UTF-8：Windows 控制台默认 GBK，混着中文/正则会变成乱码。
for _stream in (sys.stdout, sys.stderr):
    try:
        _stream.reconfigure(encoding="utf-8", errors="replace")  # type: ignore[attr-defined]
    except Exception:
        pass

TERMINAL = {"done", "failed", "timeout"}
OUTCOMES = {"passed", "failed", "timeout", "error"}

# 预设集合。--set 只接受这些名字（外加 "all"），避免"以为跑了 10 道其实跑了 2 道"。
SETS: dict[str, list[str]] = {
    "all": [],  # 空 = 全部用例
    "basic": [],  # 默认：全部 10 道
    "smoke": ["sqli-01", "cmdi-01"],
    "dvwa": ["sqli-01", "cmdi-01", "upload-01", "xss-01"],
    "dsvw": ["traversal-01", "ssrf-01", "sqli-02"],
    "juice": ["weak-pass-01", "idor-01", "auth-bypass-01"],
}

RE_SAFE_RUN_KEY = re.compile(r"^[A-Za-z0-9._-]+\.json$")


# ---------------------------------------------------------------------------
# 极简 YAML 解析（PyYAML 不可用时的兜底）
# ---------------------------------------------------------------------------

def _strip_comment(line: str) -> str:
    out: list[str] = []
    quote = ""
    for ch in line:
        if quote:
            out.append(ch)
            if ch == quote:
                quote = ""
            continue
        if ch in ("'", '"'):
            quote = ch
            out.append(ch)
        elif ch == "#":
            break
        else:
            out.append(ch)
    return "".join(out).rstrip()


def _unescape_double(body: str) -> str:
    m = {"n": "\n", "t": "\t", "r": "\r", '"': '"', "\\": "\\", "/": "/"}
    return re.sub(r"\\(.)", lambda x: m.get(x.group(1), "\\" + x.group(1)), body)


def _scalar(v: str) -> Any:
    v = v.strip()
    if len(v) >= 2 and v[0] == v[-1] and v[0] in ("'", '"'):
        body = v[1:-1]
        return body.replace("''", "'") if v[0] == "'" else _unescape_double(body)
    low = v.lower()
    if low in ("true", "yes", "on"):
        return True
    if low in ("false", "no", "off"):
        return False
    if low in ("null", "~"):
        return None
    try:
        return int(v)
    except ValueError:
        pass
    try:
        return float(v)
    except ValueError:
        pass
    return v


def _split_inline(text: str) -> list[str]:
    parts: list[str] = []
    buf: list[str] = []
    quote = ""
    for ch in text:
        if quote:
            buf.append(ch)
            if ch == quote:
                quote = ""
            continue
        if ch in ("'", '"'):
            quote = ch
            buf.append(ch)
        elif ch == ",":
            parts.append("".join(buf))
            buf = []
        else:
            buf.append(ch)
    if buf:
        parts.append("".join(buf))
    return parts


def _value(v: str) -> Any:
    v = v.strip()
    if v == "":
        return None
    if v.startswith("[") and v.endswith("]"):
        inner = v[1:-1].strip()
        return [] if not inner else [_scalar(x) for x in _split_inline(inner)]
    return _scalar(v)


def parse_yaml_fallback(text: str) -> Any:
    """只支持：嵌套 mapping、`- item` 列表、行内 [a, b]、单双引号、# 注释。"""
    root: dict[str, Any] = {}
    stack: list[tuple[int, Any]] = [(-1, root)]
    for raw in text.splitlines():
        line = _strip_comment(raw)
        if not line.strip():
            continue
        indent = len(line) - len(line.lstrip(" "))
        body = line.strip()
        while len(stack) > 1 and indent <= stack[-1][0]:
            stack.pop()
        parent = stack[-1][1]
        if body.startswith("- "):
            if not isinstance(parent, list):
                continue
            parent.append(_value(body[2:]))
            continue
        if ":" not in body:
            continue
        key, rest = body.split(":", 1)
        key = key.strip().strip("'\"")
        val = _value(rest)
        if val is None:
            nxt = {}
            parent[key] = nxt
            stack.append((indent, nxt))
        else:
            parent[key] = val
    return root


def load_case_file(path: str) -> dict[str, Any]:
    with open(path, "r", encoding="utf-8") as f:
        text = f.read()
    if yaml is not None:
        data = yaml.safe_load(text) or {}
    else:
        data = parse_yaml_fallback(text)
    if not isinstance(data, dict):
        raise ValueError("%s: 顶层必须是 mapping" % path)
    key = str(data.get("key") or "").strip()
    if not key:
        raise ValueError("%s: 缺少 key" % path)
    base = os.path.splitext(os.path.basename(path))[0]
    if base != key:
        raise ValueError("%s: 文件名 %r 与 key %r 不一致" % (path, base, key))
    data["key"] = key
    data["name"] = str(data.get("name") or "")
    data["category"] = str(data.get("category") or "")
    data["url"] = str(data.get("url") or "")
    data["goal"] = str(data.get("goal") or "")
    try:
        data["timeout_s"] = int(data.get("timeout_s") or 300)
    except (TypeError, ValueError):
        raise ValueError("%s: timeout_s 不是整数" % path)
    rule = data.get("pass") or {}
    if not isinstance(rule, dict):
        raise ValueError("%s: pass 必须是 mapping" % path)
    vcs = rule.get("finding_vulnclass") or []
    if isinstance(vcs, str):
        vcs = [vcs]
    rule["finding_vulnclass"] = [str(x) for x in vcs]
    for k in ("finding_surface_regex", "finding_evidence_regex"):
        rule[k] = str(rule.get(k) or "")
    data["pass"] = rule
    if not (rule["finding_vulnclass"] or rule["finding_surface_regex"] or rule["finding_evidence_regex"]):
        raise ValueError("%s: pass 规则全空（会让用例永远失败，先补判据）" % path)
    return data


def load_cases(cases_dir: str, allow_remote: bool) -> list[dict[str, Any]]:
    paths = sorted(glob.glob(os.path.join(cases_dir, "*.yaml")))
    if not paths:
        raise SystemExit("没有找到用例：%s/*.yaml" % cases_dir)
    cases: list[dict[str, Any]] = []
    seen: set[str] = set()
    for p in paths:
        c = load_case_file(p)
        if c["key"] in seen:
            raise SystemExit("用例 key 重复：%s" % c["key"])
        seen.add(c["key"])
        host = urllib.parse.urlsplit(c["url"]).hostname or ""
        if host not in ("127.0.0.1", "localhost", "::1"):
            if not allow_remote:
                raise SystemExit(
                    "拒绝非回环地址的用例：%s url=%s（确实要打远端请加 --allow-remote）"
                    % (c["key"], c["url"])
                )
        cases.append(c)
    return cases


def select_cases(cases: list[dict[str, Any]], set_name: str, limit: int) -> list[dict[str, Any]]:
    if set_name not in SETS:
        raise SystemExit("未知 --set %r；可选：%s" % (set_name, ", ".join(sorted(SETS))))
    want = SETS[set_name]
    if want:
        by_key = {c["key"]: c for c in cases}
        missing = [k for k in want if k not in by_key]
        if missing:
            raise SystemExit("set %r 引用了不存在的用例：%s" % (set_name, ", ".join(missing)))
        picked = [by_key[k] for k in want]
    else:
        picked = list(cases)
    if limit and limit > 0:
        picked = picked[:limit]
    if not picked:
        raise SystemExit("没有选中任何用例（--limit 太小？）")
    return picked


# ---------------------------------------------------------------------------
# 判定（与 internal/bench/bench.go 的 Case.Match 同一语义）
# ---------------------------------------------------------------------------

@dataclass
class Finding:
    vuln_class: str = ""
    surface: str = ""
    evidence: str = ""


def rule_matches(rule: dict[str, Any], f: Finding) -> bool:
    vcs = rule.get("finding_vulnclass") or []
    sr = (rule.get("finding_surface_regex") or "").strip()
    er = (rule.get("finding_evidence_regex") or "").strip()
    if not vcs and not sr and not er:
        return False
    if vcs:
        got = (f.vuln_class or "").strip().lower()
        if not got:
            return False
        if not any((w or "").strip() and (w or "").strip().lower() in got for w in vcs):
            return False
    for pattern, value in ((sr, f.surface), (er, f.evidence)):
        if pattern:
            try:
                if not re.search(pattern, value or ""):
                    return False
            except re.error:
                return False
    return True


def match_any(rule: dict[str, Any], findings: list[Finding]) -> tuple[bool, Finding | None]:
    for f in findings:
        if rule_matches(rule, f):
            return True, f
    return False, None


def findings_from_payload(payload: Any) -> list[dict[str, Any]]:
    if isinstance(payload, list):
        return [x for x in payload if isinstance(x, dict)]
    if isinstance(payload, dict):
        for k in ("items", "findings", "data"):
            v = payload.get(k)
            if isinstance(v, list):
                return [x for x in v if isinstance(x, dict)]
    return []


def to_finding(raw: dict[str, Any]) -> Finding:
    vc = raw.get("vulnclass") or raw.get("vuln_class") or raw.get("vulnerability_class") or ""
    bits: list[str] = []
    for k in ("surface", "url", "endpoint", "location"):
        v = raw.get(k)
        if isinstance(v, str) and v:
            bits.append(v)
    for a in raw.get("assets") or []:
        if isinstance(a, dict):
            for k in ("url", "label", "value"):
                v = a.get(k)
                if isinstance(v, str) and v:
                    bits.append(v)
    ev = raw.get("evidence") or raw.get("summary") or raw.get("report") or ""
    return Finding(vuln_class=str(vc), surface=" ".join(bits), evidence=str(ev))


# ---------------------------------------------------------------------------
# run_key（与 internal/bench.BuildRunKey 同一规则）
# ---------------------------------------------------------------------------

def slug(s: str, limit: int) -> str:
    s = (s or "").strip()
    s = re.sub(r"[^A-Za-z0-9._-]", "-", s)
    s = s.replace("..", "-")
    s = re.sub(r"-+", "-", s).strip("-")
    if limit > 0 and len(s) > limit:
        s = s[:limit].strip("-")
    return s


def build_run_key(now: datetime, set_name: str, model: str) -> str:
    return "%s-%s-%s" % (
        now.strftime("%Y%m%d-%H%M"),
        slug(set_name, 24) or "default",
        slug(model, 32) or "unknown",
    )


def safe_run_key(name: str) -> str:
    if not name or ".." in name or not RE_SAFE_RUN_KEY.match(name) or "/" in name or "\\" in name:
        raise ValueError("非法 run_key：%r" % name)
    return name


# ---------------------------------------------------------------------------
# HTTP
# ---------------------------------------------------------------------------

class HTTPError(RuntimeError):
    def __init__(self, method: str, path: str, code: int, body: bytes):
        self.code = code
        self.body = body
        text = body[:300].decode("utf-8", "replace")
        super().__init__("%s %s -> HTTP %d: %s" % (method, path, code, text))


class Client:
    def __init__(self, base: str, token: str = "", timeout: float = 60.0):
        self.base = base.rstrip("/")
        self.token = (token or "").strip()
        self.timeout = timeout

    def request(self, method: str, path: str, body: Any = None, timeout: float | None = None) -> Any:
        url = path if path.startswith("http") else self.base + path
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body, ensure_ascii=False).encode("utf-8")
            headers["Content-Type"] = "application/json"
        if self.token:
            low = self.token.lower()
            if low.startswith(("bearer ", "token ")):
                headers["Authorization"] = self.token
            else:
                headers["Authorization"] = "Bearer " + self.token
                if "=" in self.token:
                    headers["Cookie"] = self.token
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=timeout or self.timeout) as resp:
                raw = resp.read()
                status = resp.status
        except urllib.error.HTTPError as e:
            raw = e.read()
            status = e.code
        except urllib.error.URLError as e:
            raise HTTPError(method, path, 0, str(e).encode("utf-8"))
        if status >= 400:
            raise HTTPError(method, path, status, raw)
        if not raw:
            return None
        try:
            return json.loads(raw.decode("utf-8"))
        except (ValueError, UnicodeDecodeError):
            return raw.decode("utf-8", "replace")


def task_id_of(payload: Any) -> str:
    if isinstance(payload, dict):
        for k in ("id", "task_id"):
            v = payload.get(k)
            if v not in (None, ""):
                return str(v)
        t = payload.get("task")
        if isinstance(t, dict) and t.get("id") is not None:
            return str(t["id"])
    if isinstance(payload, (str, int)):
        return str(payload)
    return ""


def tokens_of(payload: Any) -> int:
    if not isinstance(payload, dict):
        return 0
    t = payload.get("tokens")
    if isinstance(t, dict):
        return int(t.get("input_tokens") or 0) + int(t.get("output_tokens") or 0)
    total = 0
    for k in ("input_tokens", "output_tokens"):
        try:
            total += int(payload.get(k) or 0)
        except (TypeError, ValueError):
            pass
    return total


def parse_ts(value: Any) -> float | None:
    if not isinstance(value, str) or not value:
        return None
    s = value.strip()
    if s.endswith("Z"):
        s = s[:-1] + "+00:00"
    try:
        dt = datetime.fromisoformat(s)
    except ValueError:
        return None
    if dt.tzinfo is None:
        dt = dt.replace(tzinfo=timezone.utc)
    return dt.timestamp()


def fetch_findings(client: Client, task_id: str) -> list[dict[str, Any]]:
    """优先用任务级 findings 接口；老版本没有就退回全局 findings 的 task 过滤。"""
    try:
        return findings_from_payload(client.request("GET", "/api/tasks/%s/findings" % task_id))
    except HTTPError as e:
        if e.code not in (0, 404, 405, 501):
            warn("拉 findings 失败（尝试回退）：%s" % e)
    q = urllib.parse.urlencode({"task": task_id, "limit": 200})
    return findings_from_payload(client.request("GET", "/api/exploration/findings?" + q))


# ---------------------------------------------------------------------------
# 跑批
# ---------------------------------------------------------------------------

def warn(msg: str) -> None:
    sys.stderr.write("[warn] %s\n" % msg)
    sys.stderr.flush()


def log(msg: str) -> None:
    sys.stdout.write(msg + "\n")
    sys.stdout.flush()


def iso_now() -> str:
    return datetime.now().astimezone().isoformat(timespec="seconds")


def resolve_profile_name(client: Client, profile_id: str) -> str:
    """把 llm_profile_id 解析成可读的模型名，写进 model_profile（两次结果可比的前提）。"""
    try:
        payload = client.request("GET", "/api/llm/profiles")
    except Exception as e:  # 解析失败不致命：用 id 字符串兜底
        warn("读取 LLM profiles 失败（model_profile 用 id 兜底）：%s" % e)
        return str(profile_id)
    items = findings_from_payload(payload)
    if isinstance(payload, dict):
        maybe = payload.get("profiles")
        if isinstance(maybe, list):
            items = [x for x in maybe if isinstance(x, dict)]
    for p in items:
        if str(p.get("id")) == str(profile_id):
            return str(p.get("model") or p.get("name") or profile_id)
    return str(profile_id)


def run_case(client: Client, case: dict[str, Any], profile: str, pad: int) -> dict[str, Any]:
    key = case["key"]
    start = time.monotonic()
    started_unix = time.time()
    task_id = ""
    outcome = "error"
    note = ""
    first_finding_ms = 0
    tokens = 0
    passed = False

    body = {
        "description": "[bench] %s %s" % (key, case["name"]),
        "goal": case["goal"].replace("{url}", case["url"]),
        "timeout_seconds": case["timeout_s"],
        "seed_first_intent": True,
    }
    if profile:
        try:
            body["llm_profile_id"] = int(profile)
        except ValueError:
            body["llm_profile_id"] = profile

    try:
        created = client.request("POST", "/api/tasks", body)
        task_id = task_id_of(created)
        if not task_id:
            raise RuntimeError("建任务响应里没有 id：%r" % (created,))
    except Exception as e:
        return {
            "key": key, "category": case["category"], "passed": False,
            "duration_ms": int((time.monotonic() - start) * 1000), "first_finding_ms": 0,
            "tokens": 0, "task_id": task_id, "outcome": "error",
            "note": "创建任务失败：%s" % e,
        }

    deadline = start + case["timeout_s"] + max(0, pad)
    status = ""
    detail: Any = None
    poll_fails = 0
    while True:
        try:
            detail = client.request("GET", "/api/tasks/%s" % task_id)
            status = str(detail.get("status") or "") if isinstance(detail, dict) else ""
            poll_fails = 0
        except HTTPError as e:
            poll_fails += 1
            note = "轮询失败：%s" % e
            if e.code == 404:
                status = "error"
                note = "任务 #%s 不存在（被删除或平台重启）：%s" % (task_id, e)
                break
            if poll_fails >= 5:
                status = "error"
                note = "连续 %d 次轮询失败：%s" % (poll_fails, e)
                break
        except Exception as e:
            poll_fails += 1
            note = "轮询失败：%s" % e
            if poll_fails >= 5:
                status = "error"
                note = "连续 %d 次轮询失败：%s" % (poll_fails, e)
                break
        if status in TERMINAL:
            break
        if time.monotonic() >= deadline:
            status = "timeout"
            note = "跑批侧等待超时（%ss + pad %ss）" % (case["timeout_s"], pad)
            break
        time.sleep(5)

    duration_ms = int((time.monotonic() - start) * 1000)
    tokens = tokens_of(detail)
    try:
        raws = fetch_findings(client, task_id)
    except Exception as e:
        raws = []
        note = (note + "；" if note else "") + "拉 findings 失败：%s" % e

    findings = [to_finding(x) for x in raws]
    hit, matched = match_any(case["pass"], findings)
    if hit:
        passed = True
        outcome = "passed"
    elif status == "timeout":
        outcome = "timeout"
    elif status == "error":
        outcome = "error"
    else:
        outcome = "failed"
        if not raws and not note:
            note = "任务结束但没有 finding"
        elif not passed and not note:
            note = "%d 条 finding 都不满足 pass 规则" % len(raws)

    # 首个 finding 耗时：取最早一条 finding 的 ts（相对任务开始时间）。
    stamps = []
    for x in raws:
        ts = parse_ts(x.get("ts") or x.get("created_at") or x.get("time"))
        if ts:
            stamps.append(ts)
    if stamps:
        first_finding_ms = max(0, int((min(stamps) - started_unix) * 1000))

    if hit and matched is not None:
        note = note or "命中 vulnclass=%s" % (matched.vuln_class or "?")

    return {
        "key": key, "category": case["category"], "passed": passed,
        "duration_ms": duration_ms, "first_finding_ms": first_finding_ms,
        "tokens": tokens, "task_id": task_id, "outcome": outcome, "note": note,
    }


def summarize(run: dict[str, Any]) -> dict[str, Any]:
    cases = run.get("cases") or []
    total = run.get("total") or 0
    rate = (run["passed"] / total) if total else 0.0
    dur = (sum(c["duration_ms"] for c in cases) // len(cases)) if cases else 0
    firsts = [c["first_finding_ms"] for c in cases if c.get("first_finding_ms", 0) > 0]
    first = (sum(firsts) // len(firsts)) if firsts else 0
    return {"pass_rate": rate, "avg_duration_ms": dur, "avg_first_finding_ms": first}


def write_result(out_dir: str, run: dict[str, Any]) -> str:
    os.makedirs(out_dir, exist_ok=True)
    name = safe_run_key(run["run_key"] + ".json")
    path = os.path.join(out_dir, name)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(run, f, ensure_ascii=False, indent=2)
        f.write("\n")
    os.replace(tmp, path)
    return path


def ingest(client: Client, run: dict[str, Any]) -> None:
    body = {
        "run_key": run["run_key"],
        "set_name": run["set_name"],
        "model_profile": run["model_profile"],
        "total": run["total"],
        "passed": run["passed"],
        "duration_ms": run["duration_ms"],
        "total_tokens": run["total_tokens"],
        "notes": "via scripts/bench/run.py started_at=%s cases=%d" % (run["started_at"], run["total"]),
    }
    try:
        client.request("POST", "/api/bench/ingest", body)
        log("[ingest] %s 已登记" % run["run_key"])
    except Exception as e:
        warn("ingest 失败（结果 JSON 已落盘，可手工重放）：%s" % e)


def cleanup(client: Client, task_id: str) -> None:
    if not task_id:
        return
    try:
        client.request("DELETE", "/api/tasks/%s" % task_id)
    except Exception as e:
        warn("清理任务 #%s 失败：%s" % (task_id, e))


# ---------------------------------------------------------------------------
# main
# ---------------------------------------------------------------------------

def build_parser() -> argparse.ArgumentParser:
    p = argparse.ArgumentParser(description="ARTEX 内置基准跑批器（P8-1）")
    p.add_argument("--base", default="http://127.0.0.1:8787", help="平台地址（默认 http://127.0.0.1:8787）")
    p.add_argument("--token", default="", help="登录 cookie 或 bearer token（可选）")
    p.add_argument("--set", default="basic", help="用例集：all/basic/smoke/dvwa/dsvw/juice（默认 basic）")
    p.add_argument("--limit", type=int, default=0, help="最多跑几题（0=不限）")
    p.add_argument("--profile", default="", help="llm_profile_id（写进 model_profile，两次结果可比的前提）")
    p.add_argument("--timeout-pad", type=int, default=60, help="每题在 timeout_s 之外额外等待的秒数（默认 60）")
    p.add_argument("--cleanup", action="store_true", help="跑完删除本次创建的任务（默认保留）")
    p.add_argument("--out", default=RESULTS_DIR, help="结果目录（默认 bench/results）")
    p.add_argument("--cases", default=CASES_DIR, help="用例目录（默认 bench/cases）")
    p.add_argument("--dry-run", action="store_true", help="只校验 YAML 并打印计划，不建任务")
    p.add_argument("--allow-remote", action="store_true", help="允许用例 url 指向非回环地址（默认拒绝）")
    return p


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    try:
        all_cases = load_cases(args.cases, args.allow_remote)
        cases = select_cases(all_cases, args.set, args.limit)
    except SystemExit:
        raise
    except Exception as e:
        warn(str(e))
        return 2

    now = datetime.now().astimezone()
    # dry-run 不联网：model 名直接用 --profile（或 default）。真跑时再解析成模型名。
    model_hint = args.profile or "default"
    run_key = build_run_key(now, args.set, model_hint)
    try:
        safe_run_key(run_key + ".json")
    except ValueError as e:
        warn(str(e))
        return 2

    if args.dry_run:
        log("dry-run：不建任务、不发请求")
        log("  平台      : %s" % args.base)
        log("  用例目录  : %s" % args.cases)
        log("  结果目录  : %s" % args.out)
        log("  set       : %s（%d 题）" % (args.set, len(cases)))
        log("  profile   : %s" % (args.profile or "<默认 profile>"))
        log("  run_key   : %s（真跑时 model 段会替换成模型短名）" % run_key)
        log("  yaml 库   : %s" % ("PyYAML" if yaml is not None else "内置极简解析器"))
        log("  判定语义  : vulnclass 大小写不敏感子串 AND surface/evidence 正则（全部非空条件必须满足）")
        for i, c in enumerate(cases, 1):
            rule = c["pass"]
            log(
                "  [%2d] %-15s %-13s timeout=%3ss  %s\n        pass: vulnclass=%s surface=%s evidence=%s"
                % (
                    i, c["key"], c["category"], c["timeout_s"], c["url"],
                    rule["finding_vulnclass"] or "-",
                    rule["finding_surface_regex"] or "-",
                    rule["finding_evidence_regex"] or "-",
                )
            )
        log("计划写入：%s" % os.path.join(args.out, run_key + ".json"))
        return 0

    client = Client(args.base, args.token)
    model_profile = resolve_profile_name(client, args.profile) if args.profile else "default"
    run_key = build_run_key(datetime.now().astimezone(), args.set, model_profile)
    started_at = iso_now()
    batch_start = time.monotonic()

    log(
        "开始跑批 run_key=%s set=%s profile=%s 题数=%d"
        % (run_key, args.set, model_profile, len(cases))
    )

    results: list[dict[str, Any]] = []
    created_ids: list[str] = []
    for i, case in enumerate(cases, 1):
        log("[%d/%d] %s ..." % (i, len(cases), case["key"]))
        res = run_case(client, case, args.profile, args.timeout_pad)
        results.append(res)
        if res["task_id"]:
            created_ids.append(res["task_id"])
        log(
            "[%s] %-15s duration=%6.1fs first=%6.1fs tokens=%d%s"
            % (
                "ok" if res["passed"] else "fail",
                res["key"],
                res["duration_ms"] / 1000.0,
                res["first_finding_ms"] / 1000.0,
                res["tokens"],
                ("  note=" + res["note"]) if res["note"] else "",
            )
        )
        if args.cleanup:
            cleanup(client, res["task_id"])

    passed = sum(1 for r in results if r["passed"])
    run = {
        "run_key": run_key,
        "set_name": args.set,
        "model_profile": model_profile,
        "started_at": started_at,
        "total": len(results),
        "passed": passed,
        "duration_ms": int((time.monotonic() - batch_start) * 1000),
        "total_tokens": sum(r["tokens"] for r in results),
        "cases": results,
    }

    # 本地自检：与 internal/bench.Verify 同一套约束。
    if run["passed"] > run["total"] or len(run["cases"]) != run["total"]:
        warn("结果自检失败：passed=%d total=%d cases=%d" % (run["passed"], run["total"], len(run["cases"])))
        return 3
    for c in run["cases"]:
        if c["outcome"] not in OUTCOMES:
            warn("结果自检失败：%s outcome=%r" % (c["key"], c["outcome"]))
            return 3

    path = write_result(args.out, run)
    s = summarize(run)
    log(
        "汇总 %d/%d 通过  pass_rate=%.1f%%  avg_duration=%.1fs  首个 finding 均值=%.1fs  tokens=%d"
        % (
            run["passed"], run["total"], s["pass_rate"] * 100.0,
            s["avg_duration_ms"] / 1000.0, s["avg_first_finding_ms"] / 1000.0,
            run["total_tokens"],
        )
    )
    log("结果已写入：%s" % path)
    ingest(client, run)
    if args.cleanup:
        log("已清理本次创建的 %d 个任务" % len(created_ids))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except KeyboardInterrupt:
        warn("用户中断；已跑完的用例结果未落盘（可重跑或加 --cleanup 清理残留任务）")
        sys.exit(130)
