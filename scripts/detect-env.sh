#!/usr/bin/env bash
# 环境自检：给 AI Agent 用的机器可读输出（KEY=VALUE，一行一条）。
# 用法：bash scripts/detect-env.sh
# 目的：让 AI 不用猜平台/依赖，跑一次就知道该走哪条安装路线、缺什么。
set +e
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT" || exit 1

kv() { printf '%s=%s\n' "$1" "$2"; }
has() { command -v "$1" >/dev/null 2>&1 && echo yes || echo no; }
port_open() { # $1=port
  if command -v nc >/dev/null 2>&1; then nc -z 127.0.0.1 "$1" >/dev/null 2>&1 && echo open || echo closed
  elif command -v ss >/dev/null 2>&1; then ss -ltn 2>/dev/null | grep -q ":$1 " && echo open || echo closed
  else (exec 3<>"/dev/tcp/127.0.0.1/$1") >/dev/null 2>&1 && echo open || echo closed; fi
}

case "$(uname -s)" in
  Linux) OS=linux ;;
  Darwin) OS=darwin ;;
  MINGW*|MSYS*|CYGWIN*) OS=windows-gitbash ;;
  *) OS=unknown ;;
esac
kv OS "$OS"
kv ARCH "$(uname -m)"

DISTRO=unknown; PKG=none
if [ -r /etc/os-release ]; then
  . /etc/os-release 2>/dev/null
  DISTRO="${ID:-unknown}"
fi
if command -v apt >/dev/null 2>&1; then PKG=apt
elif command -v dnf >/dev/null 2>&1; then PKG=dnf
elif command -v yum >/dev/null 2>&1; then PKG=yum
elif command -v pacman >/dev/null 2>&1; then PKG=pacman
elif command -v brew >/dev/null 2>&1; then PKG=brew
fi
kv DISTRO "$DISTRO"
kv PKG_MANAGER "$PKG"

kv DOCKER "$(has docker)"
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then kv DOCKER_COMPOSE yes; else kv DOCKER_COMPOSE no; fi
kv GO "$(has go)"
[ "$(has go)" = yes ] && kv GO_VERSION "$(go version 2>/dev/null | awk '{print $3}')"
kv NODE "$(has node)"
[ "$(has node)" = yes ] && kv NODE_VERSION "$(node -v 2>/dev/null)"
kv NPM "$(has npm)"
kv GIT "$(has git)"
kv PSQL "$(has psql)"

kv PORT_5432 "$(port_open 5432)"
kv PORT_55432 "$(port_open 55432)"   # 本项目历史上用过嵌入式 PG
kv PORT_8787 "$(port_open 8787)"     # ARTEX 默认端口
kv PORT_8788 "$(port_open 8788)"     # 记录代理端口

[ -f artex ] && kv ARTEX_BINARY artex
[ -f dist/artex.exe ] && kv ARTEX_BINARY dist/artex.exe
[ -f artex.exe ] && kv ARTEX_BINARY artex.exe
kv HAS_CONFIG "$([ -f config.json ] && echo yes || echo no)"
kv HAS_ENV_FILE "$([ -f .env ] && echo yes || echo no)"
kv HAS_SKILLS "$([ -d skills ] && echo yes || echo no)"
kv HAS_KB "$([ -d kb ] && echo yes || echo no)"
kv HAS_WEB_NODE_MODULES "$([ -d web/node_modules ] && echo yes || echo no)"
kv REPO_ROOT "$ROOT"
echo "---"
echo "NEXT: read INSTALL-AI.md and pick a route from the decision table"
