#!/usr/bin/env bash
#
# ARTEX VPS 一键部署（Debian/Ubuntu 系 与 RHEL/CentOS 系）
#
# 用法（在仓库根目录，或任意目录里用绝对路径）：
#   bash scripts/deploy_vps.sh              # 真部署
#   bash scripts/deploy_vps.sh --dry-run    # 只打印每步要做什么，不动系统
#   bash scripts/deploy_vps.sh --help
#
# 设计约束（刻意为之，别"优化"掉）：
#   * 幂等：重复执行不会重装 docker、不会覆盖已有 .env、不会删任何数据卷。
#   * 不碰防火墙、不装/不改 LLM key、不改系统时区/内核参数。
#   * 每一步都打印"正在做什么"，失败时打印"为什么 + 下一步做什么"。
#   * 退出码：0 成功；2 需要人工介入（不支持的系统/缺仓库文件）；
#             3 docker 安装失败；4 启动失败；5 健康检查超时。
#
set -euo pipefail

# 有人用 `sh scripts/deploy_vps.sh` 跑：SECONDS/BASH_SOURCE 都是 bash 专有，切回来。
if [ -z "${BASH_VERSION:-}" ]; then exec bash "$0" "$@"; fi

# ── 输出小工具 ──────────────────────────────────────────────
if [ -t 1 ] && [ "${NO_COLOR:-}" = "" ]; then
  C_R=$'\033[31m'; C_G=$'\033[32m'; C_Y=$'\033[33m'; C_C=$'\033[36m'; C_B=$'\033[1m'; C_0=$'\033[0m'
else
  C_R=''; C_G=''; C_Y=''; C_C=''; C_B=''; C_0=''
fi
step(){ printf '\n%s%s▸ %s%s\n' "$C_B" "$C_C" "$*" "$C_0"; }
info(){ printf '  %s·%s %s\n' "$C_C" "$C_0" "$*"; }
ok(){   printf '  %s✓%s %s\n' "$C_G" "$C_0" "$*"; }
warn(){ printf '  %s!%s %s\n' "$C_Y" "$C_0" "$*"; }
err(){  printf '  %s✗%s %s\n' "$C_R" "$C_0" "$*" >&2; }
die(){ err "$*"; exit "${2:-1}"; }

# ── 参数 ────────────────────────────────────────────────────
DRY_RUN=0
for a in "$@"; do
  case "$a" in
    --dry-run|-n) DRY_RUN=1 ;;
    --help|-h)
      if [ -f "$0" ]; then
        sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'
      else
        printf '用法：bash deploy_vps.sh [--dry-run|--help]
'
      fi
      exit 0 ;;
    *) die "未知参数：$a（可用：--dry-run / --help）" 2 ;;
  esac
done
dry(){ [ "$DRY_RUN" = 1 ]; }
run(){ if dry; then info "[dry-run] 会执行：$*"; else "$@"; fi; }

# 仓库根：脚本在 <repo>/scripts/ 下；被 curl|bash 管道执行时退回当前目录。
SELF="${BASH_SOURCE[0]:-}"
if [ -n "$SELF" ] && [ -f "$SELF" ]; then
  REPO="$(cd "$(dirname "$SELF")/.." && pwd)"
else
  REPO="$PWD"
fi
cd "$REPO"

PORT="${ARTEX_PORT:-8787}"
HEALTH_URL="http://127.0.0.1:${PORT}/api/health"
WAIT_SECS=90

printf '%sARTEX VPS 部署%s\n' "$C_B" "$C_0"
info "仓库目录：$REPO"
[ "$DRY_RUN" = 1 ] && warn "dry-run 模式：只打印计划，不做任何修改"

# root 判断：装 docker / 改包需要提权，有 sudo 就用 sudo。
# 用字符串而非数组：bash 4.2（CentOS 7）在 set -u 下展开空数组会报 unbound variable。
SUDO=""
if [ "$(id -u)" != "0" ] && command -v sudo >/dev/null 2>&1; then SUDO="sudo"; fi

# ════════════════════════════════════════════════════════════
# ① 检测系统
# ════════════════════════════════════════════════════════════
step "① 检测系统"
OS_ID=""; OS_LIKE=""; OS_NAME="unknown"
if [ -r /etc/os-release ]; then
  # os-release 里可能有未定义变量引用，临时关掉 nounset 再 source。
  set +u
  # shellcheck disable=SC1091
  . /etc/os-release
  set -u
  OS_ID="${ID:-}"; OS_LIKE="${ID_LIKE:-}"; OS_NAME="${PRETTY_NAME:-${ID:-unknown}}"
fi
info "系统：$OS_NAME"

PKG=""
case " $OS_ID $OS_LIKE " in
  *" debian "*|*" ubuntu "*|*" linuxmint "*|*" raspbian "*|*" pop "*) PKG=apt ;;
  *" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" fedora "*|*" ol "*)    PKG=dnf ;;
esac
# 兜底：按包管理器存在与否判断（容器镜像里 ID 往往很怪）
if [ -z "$PKG" ]; then
  if command -v apt-get >/dev/null 2>&1; then PKG=apt
  elif command -v dnf >/dev/null 2>&1; then PKG=dnf
  elif command -v yum >/dev/null 2>&1; then PKG=yum
  fi
fi
if [ -z "$PKG" ]; then
  if dry; then
    # dry-run 只输出计划：本机不是 VPS（例如 Windows/Git Bash 上试跑）也不该中断，
    # 但要说明白"真实执行会在这里退出 2"，否则容易误以为本机可部署。
    warn "dry-run：本机（$OS_NAME）不在支持列表，按 Debian/Ubuntu + apt 假设继续推演"
    warn "        真实执行时会在这里 exit 2 并打印下面的指引"
    PKG=apt
  else
  err "不支持的系统：$OS_NAME"
  cat <<'EOF'

  本脚本只覆盖两类系统：
    · Debian / Ubuntu 系（apt）—— 推荐 Debian 12 / Ubuntu 22.04+
    · RHEL / CentOS / Rocky / AlmaLinux / Fedora 系（dnf/yum）

  其它系统（Alpine、Arch、openSUSE、BSD、macOS、Windows…）请二选一：
    A. 换一台 Debian 12 或 Ubuntu 22.04 的 VPS（最省事）
    B. 手工部署：
       1) 按官方文档装 docker + docker compose plugin
          https://docs.docker.com/engine/install/
       2) 在本仓库根目录：cp .env.example .env
          把 POSTGRES_PASSWORD 改成随机值（openssl rand -hex 16）
       3) docker compose up -d
       4) 浏览器打开 http://<本机IP>:8787
EOF
  exit 2
  fi
fi
ok "包管理器：$PKG"

# ════════════════════════════════════════════════════════════
# ② docker / docker compose plugin
# ════════════════════════════════════════════════════════════
step "② 检测 docker 与 docker compose plugin"
have_compose(){ command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; }

if have_compose; then
  ok "已安装：$(docker --version 2>/dev/null | tr -d '\n')；compose $(docker compose version --short 2>/dev/null || echo ok)"
else
  if command -v docker >/dev/null 2>&1; then
    warn "有 docker，但没有 compose plugin —— 只补装插件，不动已有 docker"
    case "$PKG" in
      apt)     run $SUDO apt-get update -y && run $SUDO apt-get install -y docker-compose-plugin ;;
      dnf|yum) run $SUDO "$PKG" install -y docker-compose-plugin ;;
    esac
  else
    info "未检测到 docker，使用官方脚本安装（curl -fsSL https://get.docker.com | sh）"
    if dry; then
      info "[dry-run] 会执行：curl -fsSL https://get.docker.com | ${SUDO:+$SUDO }sh"
      info "[dry-run] 会执行：${SUDO:+$SUDO }systemctl enable --now docker"
    else
      if ! command -v curl >/dev/null 2>&1; then
        err "缺少 curl，先装：${SUDO:+$SUDO }$PKG install -y curl"
        exit 3
      fi
      if ! curl -fsSL https://get.docker.com | $SUDO sh; then
        err "Docker 官方安装脚本执行失败"
        cat <<'EOF'

  下一步做什么（任选其一）：
    1. 重跑本脚本（官方脚本可重复执行，网络抖动重试往往就好了）
    2. 手工装：https://docs.docker.com/engine/install/  （Debian/Ubuntu 选对应发行版）
    3. 国内 VPS 拉不动 get.docker.com：改用镜像源，例如
         curl -fsSL https://get.docker.com | sh -s -- --mirror Aliyun
       （或直接用阿里云/腾讯云的"容器镜像服务"文档里的安装命令）
EOF
        exit 3
      fi
      run $SUDO systemctl enable --now docker
      ok "Docker 安装完成"
    fi
  fi
  # 再验一次
  if ! dry; then
    have_compose || die "docker compose plugin 仍不可用。先执行 'docker compose version' 看报错，或手工装 docker-compose-plugin 后重跑本脚本。" 3
  fi
  if [ "$(id -u)" != "0" ]; then
    warn "刚安装完 docker：当前用户还不在 docker 组，脚本会用 sudo；日常免 sudo 需重新登录（usermod -aG docker \$USER）"
  fi
fi

# ════════════════════════════════════════════════════════════
# ③ .env 与随机 Postgres 密码
# ════════════════════════════════════════════════════════════
step "③ 准备 .env（Postgres 密码）"
ENV_FILE="$REPO/.env"
PASS_LINE=""
if [ -f "$ENV_FILE" ]; then
  ok "沿用现有 .env（不覆盖，不重新生成密码）"
  PASS_LINE="$(grep -n '^POSTGRES_PASSWORD=' "$ENV_FILE" 2>/dev/null | head -1 | cut -d: -f1 || true)"
  if [ -z "$PASS_LINE" ]; then
    warn ".env 里没有 POSTGRES_PASSWORD —— docker compose 会直接报错。请在 $ENV_FILE 补一行：POSTGRES_PASSWORD=<随机值>"
  elif grep -q '^POSTGRES_PASSWORD=change-me-please' "$ENV_FILE"; then
    warn ".env 用的还是示例里的 change-me-please，建议改成随机值（改完执行 docker compose up -d 生效，数据卷不丢）"
  fi
else
  if [ ! -f "$REPO/.env.example" ]; then
    die "找不到 .env.example —— 请确认这是完整的 ARTEX 仓库（不是只有部分文件）" 2
  fi
  if dry; then
    info "[dry-run] 会执行：cp .env.example .env（权限 600）"
    info "[dry-run] 会执行：openssl rand -hex 16 → 写入 .env 的 POSTGRES_PASSWORD"
    PASS_LINE="$(grep -n '^POSTGRES_PASSWORD=' "$REPO/.env.example" | head -1 | cut -d: -f1)"
  else
    PW="$(openssl rand -hex 16 2>/dev/null || head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n' | cut -c1-32)"
    TMP="$(mktemp)"
    # 逐行替换，避免 sed -i 的平台差异；.env 里其它键（ARTEX_IMAGE 等）原样保留。
    if grep -q '^POSTGRES_PASSWORD=' "$REPO/.env.example"; then
      sed "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${PW}|" "$REPO/.env.example" > "$TMP"
    else
      cp "$REPO/.env.example" "$TMP"
      printf 'POSTGRES_PASSWORD=%s\n' "$PW" >> "$TMP"
    fi
    umask 077
    mv "$TMP" "$ENV_FILE"
    chmod 600 "$ENV_FILE"
    PASS_LINE="$(grep -n '^POSTGRES_PASSWORD=' "$ENV_FILE" | head -1 | cut -d: -f1)"
    ok "已生成 .env，随机密码写入第 ${PASS_LINE} 行（权限 600）"
  fi
fi

# ════════════════════════════════════════════════════════════
# ④ 仓库完整性
# ════════════════════════════════════════════════════════════
step "④ 
# 镜像需要 dist/<arch>/artex（Dockerfile COPY 的路径）。缺了就给出可执行的下一步，
# 而不是等 docker build 报 "COPY failed"。
if [ ! -f "dist/amd64/artex" ]; then
  warn "未找到 dist/amd64/artex（Dockerfile 需要的 Linux 二进制）"
  if command -v go >/dev/null 2>&1 && command -v node >/dev/null 2>&1; then
    ok "检测到 Go 与 Node，自动构建：bash scripts/package_linux.sh"
    bash scripts/package_linux.sh || die "构建失败：看上面的报错"
  else
    die "缺 Go/Node 无法本地构建。二选一：①装 go+node 后跑 bash scripts/package_linux.sh；②下载 Release 里的 Linux 包，把 artex 放到 dist/amd64/artex"
  fi
fi
确认部署文件齐全"
missing=()
for f in Dockerfile docker-compose.yml; do
  [ -f "$REPO/$f" ] || missing+=("$f")
done
if [ "${#missing[@]}" -gt 0 ]; then
  err "缺少：${missing[*]}"
  cat <<EOF

  当前目录是：$REPO

  原因通常有两种：
    1. 用 'curl … | bash' 直接跑了脚本，但没有仓库文件。
       → 先克隆再执行：
           git clone https://github.com/gthubtom1/ARTEX.git
           cd ARTEX && bash scripts/deploy_vps.sh
    2. 在错误的目录里执行（例如在 \$HOME 下）。
       → cd 到仓库根目录（能看到 Dockerfile 的那一层）再执行。
  脚本不会替你下载仓库：缺文件时盲目往下走只会在 build 阶段报更晦涩的错。
EOF
  exit 2
fi
ok "Dockerfile / docker-compose.yml 均在"
if grep -qE '^[[:space:]]+build:' docker-compose.yml; then
  info "compose 里有 build: 段 → 会就地构建镜像（要求 dist/<arch>/artex 已存在）"
else
  info "compose 引用预构建镜像（无 build: 段）→ --build 不会真的编译，按 .env 里的 ARTEX_IMAGE/ARTEX_TAG 拉取"
fi

# ════════════════════════════════════════════════════════════
# ⑤ 启动
# ════════════════════════════════════════════════════════════
step "⑤ 启动容器（docker compose up -d --build）"
if dry; then
  info "[dry-run] 会执行：${SUDO:+$SUDO }docker compose up -d --build"
else
  if ! $SUDO docker compose up -d --build; then
    err "docker compose up 失败"
    printf '\n── docker compose logs --tail=50 ──────────────────\n'
    $SUDO docker compose logs --tail=50 || true
    printf '───────────────────────────────────────────────────\n'
    cat <<'EOF'

  按报错对号入座（三条最常见）：
    1. 端口占用：'Bind for 0.0.0.0:8787 failed: port is already allocated'
       → 查谁占着：ss -ltnp | grep :8787   （或 sudo lsof -i:8787）
         停掉它，或改 .env 之外的方式换端口（compose 里写死 8787，需改 docker-compose.yml）。
    2. 数据库没起来：日志里 postgres 反复 'database system is starting up' / 认证失败
       → docker compose ps  看 postgres 是否 healthy；
         docker compose logs postgres --tail=50
         若 .env 里的 POSTGRES_PASSWORD 与数据卷里已初始化的密码不一致，
         容器会认证失败（数据卷密码只在首次初始化时生效）。
         改 .env 回旧密码，或确认可以丢弃旧数据后再 'docker compose down -v'（会删数据！）。
    3. 构建/拉镜像失败：'pull access denied' / 'no such image' / 网络超时
       → docker compose pull  单独重试；国内机器给 docker 配镜像加速后重跑本脚本。
         若报 dist/<arch>/artex not found，说明本地构建缺预编译二进制：
         先 bash build.sh（或改用预发布镜像，不要加 --build）。
EOF
    exit 4
  fi
  ok "容器已启动"
fi

# ════════════════════════════════════════════════════════════
# ⑥ 健康检查（最多 90 秒）
# ════════════════════════════════════════════════════════════
step "⑥ 等待服务就绪（$HEALTH_URL，最多 ${WAIT_SECS}s）"
if dry; then
  info "[dry-run] 会执行：轮询 $HEALTH_URL，2xx 即认为就绪，最多 ${WAIT_SECS} 秒"
else
  body=""
  ready=0
  deadline=$((SECONDS + WAIT_SECS))
  i=0
  while [ "$SECONDS" -lt "$deadline" ]; do
    i=$((i + 1))
    code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 3 "$HEALTH_URL" 2>/dev/null || true)"
    case "$code" in
      2*) ready=1; break ;;
    esac
    [ $((i % 5)) -eq 1 ] && info "第 ${i} 次探测：HTTP ${code:-无响应}（服务可能还在建表）"
    sleep 3
  done
  if [ "$ready" != 1 ]; then
    err "${WAIT_SECS} 秒内 $HEALTH_URL 没有返回 2xx"
    printf '\n── docker compose ps ──────────────────────────────\n'
    $SUDO docker compose ps || true
    printf '\n── docker compose logs --tail=50 ──────────────────\n'
    $SUDO docker compose logs --tail=50 || true
    printf '───────────────────────────────────────────────────\n'
    cat <<'EOF'

  三条排查建议（按出现频率排序）：
    1. 端口/进程：docker compose ps 看 artex 是否在跑；不在就跑
       docker compose logs artex --tail=100
       若日志有 'address already in use' → ss -ltnp | grep :8787 找到占用者并停掉。
    2. 数据库没起来：postgres 不 healthy 时 artex 会被 compose 卡住不启动。
       docker compose logs postgres --tail=50 看是密码不对、还是磁盘满（df -h）。
       .env 里的 POSTGRES_PASSWORD 只在数据卷**首次初始化**时生效，
       改密码不会改已有数据卷 —— 要么改回旧值，要么确认可丢数据后 down -v。
    3. 构建失败：docker compose pull 单独试；看是不是镜像 tag 不存在，
       或（本地构建时）dist/<arch>/artex 缺失。构建失败不会破坏已有数据卷，重试即可。
  （若只是健康检查慢：VPS 首次建表 + 拉镜像可能超过 90 秒，
    再等一会儿直接看浏览器 http://<VPS_IP>:8787，容器一般还在启动中。）
EOF
    exit 5
  fi
  body="$(curl -s --max-time 3 "$HEALTH_URL" 2>/dev/null || true)"
  ok "服务已就绪：$HEALTH_URL → ${body:-（空响应，2xx 视为正常）}"
fi

# ════════════════════════════════════════════════════════════
# ⑦ 收尾信息
# ════════════════════════════════════════════════════════════
step "⑦ 完成"
IP="<VPS_IP>"
if ! dry; then
  IP="$(curl -4 -s --max-time 4 https://api.ipify.org 2>/dev/null || true)"
  if [ -z "$IP" ]; then
    IP="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
  fi
  [ -z "$IP" ] && IP="<VPS_IP>"
fi
printf '\n'
printf '  %s✓%s 访问地址：%shttp://%s:%s%s
' "$C_G" "$C_0" "$C_B" "$IP" "$PORT" "$C_0"
ok "用户名：ARTEX"
if [ -n "$PASS_LINE" ]; then
  ok "Postgres 密码：.env 第 ${PASS_LINE} 行（POSTGRES_PASSWORD）；登录密码在首次打开页面时设置"
else
  ok "Postgres 密码：见 $ENV_FILE 的 POSTGRES_PASSWORD 行"
fi
info "注意：8787 直接对外；8788（代理端口）只绑了 127.0.0.1，公网不可达（符合预期）"
info "下一步：浏览器登录 → 系统 → 首次配置 → 填一个模型（DeepSeek / OpenAI 兼容都行）→ 出口自检 → 完成"
info "常用命令："
printf '    cd %s\n' "$REPO"
printf '    %sdocker compose logs -f artex%s     # 看日志\n' "$C_C" "$C_0"
printf '    %sdocker compose ps%s                # 看状态\n' "$C_C" "$C_0"
printf '    %sdocker compose restart artex%s     # 只重启应用（不动数据库）\n' "$C_C" "$C_0"
info "升级：git pull && bash scripts/deploy_vps.sh（.env 与 pgdata 数据卷都会保留）"
exit 0
