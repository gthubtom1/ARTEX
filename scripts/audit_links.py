#!/usr/bin/env python3
"""全链路静态审计：导航↔页面↔路由↔前端调用的一致性。"""
import glob
import re
import sys
from collections import Counter


def norm(p: str) -> str:
    return p.replace("\\", "/")


def page_set() -> set:
    out = set()
    for f in glob.glob("web/src/app/**/page.tsx", recursive=True):
        rel = norm(f[len("web/src/app"): -len("/page.tsx")])
        rel = re.sub(r"^/\((main|auth|external)\)", "", rel)
        out.add(rel or "/")
    return out


def route_set() -> set:
    out = set()
    for f in glob.glob("server/*.go"):
        t = open(f, encoding="utf-8", errors="ignore").read()
        out |= set(re.findall(r'HandleFunc\("[A-Z]+ ([^"]+)"', t))
    return out


def route_re(route: str) -> str:
    parts = re.split(r"(\{[^}]+\})", route)
    return "^" + "".join("[^/]+" if p.startswith("{") else re.escape(p) for p in parts) + "$"


def matches(path: str, routes: set) -> bool:
    for r in routes:
        if re.match(route_re(r), path):
            return True
    return False


def main() -> int:
    bad = 0
    pages = page_set()
    nav = open("web/src/navigation/sidebar/sidebar-items.ts", encoding="utf-8").read()
    urls = [norm(u) for u in re.findall(r'url:\s*"([^"]+)"', nav)]
    missing = [u for u in urls if u not in pages]
    dup_nav = [k for k, v in Counter(urls).items() if v > 1]
    print(f"[nav] {len(urls)} urls / {len(pages)} pages; missing={missing}; dup={dup_nav}")
    bad |= bool(missing or dup_nav)

    routes = route_set()
    all_routes = []
    for f in glob.glob("server/*.go"):
        t = open(f, encoding="utf-8", errors="ignore").read()
        all_routes += re.findall(r'HandleFunc\("([A-Z]+ [^"]+)"', t)
    dup_routes = [k for k, v in Counter(all_routes).items() if v > 1]
    print(f"[route] {len(routes)} unique; duplicate registrations={dup_routes}")
    bad |= bool(dup_routes)

    api = open("web/src/lib/api.ts", encoding="utf-8").read()
    i = api.find("export const p2api")
    block = api[i:] if i >= 0 else ""
    p2 = sorted({u for u in re.findall(r'["`](/[^"`]*)["`]', block) if not u.startswith("//")})
    print(f"[p2api] {len(p2)} paths")
    for p in p2:
        base = "/api" + p.split("?")[0]
        base = re.sub(r"\$\{[^}]*$", "", base)
        base = re.sub(r"\$\{[^}]+\}", "{x}", base)
        ok = matches(base, routes)
        print(f"  {'OK ' if ok else 'MISS'} {p}")
        bad |= (not ok)

    new_pages = ["capabilities", "owned", "data", "knowledge", "report"]
    new_pages += ["audit", "environment", "network", "replay", "verifier"]
    print("[fetches] 新页面裸 fetch")
    for name in new_pages:
        cands = glob.glob(f"web/src/app/(main)/**/{name}/page.tsx", recursive=True)
        if not cands:
            print(f"  MISS PAGE {name}")
            bad = 1
            continue
        t = open(cands[0], encoding="utf-8").read()
        calls = re.findall(r'fetch\(`?"?([^"`?$]+)', t)
        for c in calls:
            if not c.startswith("/api"):
                continue
            path = re.sub(r"\$\{[^}]+\}", "{x}", c)
            if not matches(path, routes):
                print(f"  MISS {name}: {c}")
                bad = 1

    print("\nAUDIT_RESULT", "PASS" if bad == 0 else "FAIL")
    return 1 if bad else 0


if __name__ == "__main__":
    sys.exit(main())
