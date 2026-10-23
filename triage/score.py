#!/usr/bin/env python3
"""
把「一堆资产」压成「值得花 AI 钱的 20 条」。

输入（两种来源都行，可混用）：
  recon.jsonl   httpx -json 的输出（一行一个资产：url/status/title/tech/favicon/cdn）
  hits.jsonl    nuclei -jsonl 的输出（一行一个命中）
输出：
  shortlist.jsonl   按分数降序的候选，字段：url,score,reasons,cluster,dup_of
  shortlist.md      人看的表（可直接挑 URL 贴进 ARTEX）

用法：
  python3 triage/score.py --recon recon.jsonl [--hits hits.jsonl] \
                          --top 20 --out-prefix triage/out/run1
  # 只看某些簇 / 排除内网：
  python3 triage/score.py --recon recon.jsonl --exclude-loopback
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from collections import defaultdict

# ---------------- 打分规则（改这里就是改你的判断标准） ----------------

# 高价值指纹：命中即 +30。按你自己的目标生态增删。
HIGH_VALUE = {
    # 国内常见 OA / ERP / 协同
    "致远oa": 30, "seeyon": 30, "泛微": 30, "weaver": 30, "ecology": 30,
    "用友": 30, "yonyou": 30, "金蝶": 30, "kingdee": 30, "蓝凌": 30, "landray": 30,
    "通达oa": 30, "tongda": 30, "帆软": 30, "finereport": 30,
    # 运维面 / 堡垒机
    "jumpserver": 30, "堡垒机": 30, "齐治": 30, "深信服": 25, "sangfor": 25,
    "citrix": 25, "fortinet": 25, "pulse": 25, "vpn": 15,
    # 数据库/中间件管理面（未授权高发）
    "phpmyadmin": 25, "adminer": 25, "druid": 25, "elasticsearch": 25,
    "kibana": 25, "grafana": 25, "jenkins": 25, "gitlab": 25, "harbor": 25,
    "consul": 25, "etcd": 25, "nacos": 30, "zookeeper": 25, "rabbitmq": 20,
    "redis": 25, "mongodb": 20, "couchdb": 20, "minio": 25,
    # 接口/调试面
    "swagger": 25, "openapi": 20, "actuator": 25, "spring boot": 20,
    "graphql": 20, "django": 10, "laravel": 10, "thinkphp": 25,
    "struts": 25, "weblogic": 25, "jboss": 25, "tomcat": 15, "shiro": 25,
    # 框架/语言（弱信号）
    "wordpress": 10, "joomla": 10, "drupal": 10, "discuz": 15,
}

SENSITIVE_PATH_HINTS = ("/.git", "/.env", "/backup", "/.svn", "/swagger-ui",
                        "/actuator", "/druid", "/phpinfo", "/.ds_store", "/config.php.bak")

# 状态码
SC_WEIGHT = {200: 10, 401: 4, 403: 2, 500: 6, 302: 6}

# 扣分项
PENALTY = {
    "honeypot": -40,
    "cdn": -30,          # CDN 后面打不到真机
    "static": -30,       # 纯静态没攻击面
}


def load_jsonl(path: str):
    if not path or not os.path.exists(path):
        return []
    out = []
    with open(path, "r", encoding="utf-8", errors="replace") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                out.append(json.loads(line))
            except json.JSONDecodeError:
                continue
    return out


def techs(rec: dict) -> list[str]:
    t = rec.get("tech") or rec.get("technologies") or []
    if isinstance(t, str):
        t = [t]
    return [str(x).lower() for x in t]


def favicon(rec: dict) -> str:
    f = rec.get("favicon")
    if isinstance(f, dict):
        h = f.get("hash") or f.get("mmh3")
        if h not in (None, "", 0):
            return str(h)
    if isinstance(f, str) and f:
        return f
    return ""


def origin(u: str) -> str:
    """把任意 URL 压成 scheme://host:port，用来把 /login 的命中算到站点头上。"""
    from urllib.parse import urlsplit
    if not u:
        return ""
    if not u.startswith(("http://", "https://")):
        u = "http://" + u
    try:
        p = urlsplit(u)
    except ValueError:
        return u
    host = p.hostname or ""
    port = f":{p.port}" if p.port and p.port not in (80, 443) else ""
    return f"{p.scheme}://{host}{port}".lower()


def normalize_url(rec: dict) -> str:
    for k in ("url", "input", "host"):
        v = rec.get(k)
        if v:
            v = str(v)
            if not v.startswith(("http://", "https://")):
                v = "https://" + v
            return v.rstrip("/")
    return ""


def score_one(rec: dict, hit_index: dict) -> tuple[int, list[str], dict]:
    url = normalize_url(rec)
    s = 0
    reasons: list[str] = []
    meta = {"url": url, "title": rec.get("title", ""), "status": rec.get("status_code") or rec.get("status")}

    # 1) 指纹
    for t in techs(rec):
        for k, v in HIGH_VALUE.items():
            if k in t:
                s += v
                reasons.append(f"指纹:{k}(+{v})")
                break

    # 2) nuclei 命中（先精确 URL，再按 origin 归并：/login 的命中算到站点头上）
    hs = (hit_index.get(url, [])
          or hit_index.get(url.replace("https://", "http://"), [])
          or hit_index.get(origin(url), [])
          or hit_index.get(origin(url).replace("https://", "http://"), []))
    if hs:
        sev = {"critical": 40, "high": 30, "medium": 12, "low": 4, "info": 0}
        best = 0
        for h in hs:
            lvl = (h.get("info", {}).get("severity") or "info").lower()
            pts = sev.get(lvl, 0)
            if pts > best:
                best = pts
            reasons.append(f"命中:{(h.get('info', {}).get('name') or h.get('template-id','?'))[:40]}({lvl})")
        s += best

    # 3) 敏感路径（从 nuclei 的 matched-at 或从 url 推）
    blob = json.dumps(rec, ensure_ascii=False).lower() + " " + " ".join(
        str(h.get("matched-at", "")).lower() for h in hs)
    if any(p in blob for p in SENSITIVE_PATH_HINTS):
        s += 20
        reasons.append("敏感路径(+20)")

    # 4) 状态码 / 标题
    sc = meta["status"]
    if isinstance(sc, int) and sc in SC_WEIGHT:
        s += SC_WEIGHT[sc]
        reasons.append(f"状态码{sc}(+{SC_WEIGHT[sc]})")
    title = (meta["title"] or "").lower()
    if any(w in title for w in ("登录", "login", "后台", "admin", "管理", "console", "dashboard", "系统")):
        s += 10
        reasons.append("登录/管理面(+10)")

    # 5) 扣分
    if rec.get("cdn") or rec.get("cdn_name"):
        s += PENALTY["cdn"]; reasons.append(f"CDN:{rec.get('cdn_name') or rec.get('cdn')}(-30)")
    if not rec.get("tech") and (meta["title"] or "") == "" and sc in (200,):
        s += PENALTY["static"]; reasons.append("无指纹无标题(-30)")
    for p in (rec.get("honeypot"), rec.get("is_honeypot")):
        if p:
            s += PENALTY["honeypot"]; reasons.append("疑似蜜罐(-40)")

    meta["cluster_key"] = favicon(rec) or ("tech:" + ",".join(sorted(techs(rec))[:3])) or ("title:" + (meta["title"] or url))
    return s, reasons, meta


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--recon", help="httpx -json 输出")
    ap.add_argument("--hits", help="nuclei -jsonl 输出")
    ap.add_argument("--top", type=int, default=20)
    ap.add_argument("--min-score", type=int, default=0)
    ap.add_argument("--out-prefix", default="triage/out/shortlist")
    ap.add_argument("--exclude-loopback", action="store_true")
    ap.add_argument("--keep-cluster-dups", action="store_true", help="同簇全部保留（默认只留最高分）")
    args = ap.parse_args()

    recon = load_jsonl(args.recon)
    hits = load_jsonl(args.hits)
    if not recon and not hits:
        print("[x] 没有输入。先跑 httpx -json -o recon.jsonl 或 nuclei -jsonl -o hits.jsonl", file=sys.stderr)
        return 1

    # nuclei 命中按 host 建立索引
    hit_index: dict[str, list] = defaultdict(list)
    for h in hits:
        for k in ("matched-at", "host", "url"):
            v = h.get(k)
            if v:
                raw = str(v).split("?")[0].rstrip("/")
                hit_index[raw].append(h)
                o = origin(raw)
                if o and o != raw.lower():
                    hit_index.setdefault(o, []).append(h)

    # 只有 hits 没有 recon 时，用 hits 反推资产行
    if not recon:
        seen = set()
        for h in hits:
            u = str(h.get("matched-at") or h.get("host") or "").split("?")[0].rstrip("/")
            if u and u not in seen:
                seen.add(u)
                recon.append({"url": u, "status_code": 200})

    scored = []
    for r in recon:
        s, reasons, meta = score_one(r, hit_index)
        if args.exclude_loopback and any(x in meta["url"] for x in ("127.0.0.1", "localhost", "::1")):
            continue
        if s < args.min_score:
            continue
        scored.append((s, reasons, meta))

    scored.sort(key=lambda x: -x[0])

    # 同簇去重：favicon/指纹相同只留最高分
    kept, clusters = [], defaultdict(list)
    for s, reasons, meta in scored:
        clusters[meta["cluster_key"]].append(meta["url"])
    for s, reasons, meta in scored:
        dup_of = ""
        if not args.keep_cluster_dups:
            members = clusters[meta["cluster_key"]]
            if members and members[0] != meta["url"]:
                dup_of = members[0]
                continue
        kept.append({"url": meta["url"], "score": s, "reasons": reasons,
                     "title": meta["title"], "status": meta["status"],
                     "cluster": meta["cluster_key"], "cluster_size": len(clusters[meta["cluster_key"]])})

    kept = kept[: args.top]

    os.makedirs(os.path.dirname(args.out_prefix) or ".", exist_ok=True)
    with open(args.out_prefix + ".jsonl", "w", encoding="utf-8") as f:
        for row in kept:
            f.write(json.dumps(row, ensure_ascii=False) + "\n")

    with open(args.out_prefix + ".md", "w", encoding="utf-8") as f:
        f.write(f"# 候选清单（{len(kept)} 条 / 全量 {len(scored)} 条打分）\n\n")
        f.write("| # | 分数 | URL | 标题 | 理由 | 同簇数 |\n|---|---|---|---|---|---|\n")
        for i, r in enumerate(kept, 1):
            rs = "; ".join(r["reasons"][:4])
            f.write(f"| {i} | {r['score']} | {r['url']} | {(r['title'] or '')[:24]} | {rs} | {r['cluster_size']} |\n")

    print(f"[+] 打分 {len(scored)} 条，输出 top {len(kept)} → {args.out_prefix}.md / .jsonl")
    for i, r in enumerate(kept[:10], 1):
        print(f"  {i:2d}. [{r['score']:>3}] {r['url']}  {('', '· ' + r['reasons'][0] if r['reasons'] else '')}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
