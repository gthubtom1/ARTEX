#!/usr/bin/env bash
# stub-bootstrap.sh —— 在没有 docker 的开发机上把 deploy/bootstrap.sh 的**流程**跑一遍。
#
# 它不是真机验收，也不冒充：docker / curl / jq / uname / id 全部被 stub 掉，
# 只证明「脚本分支、.env 幂等、start→wire→status 调用链、参数传递」没写错。
# 真机验收：干净机器上 `sudo bash deploy/bootstrap.sh` 后跑 `bash deploy/status.sh`，
# 要看到 STATUS PASS 且「MCP 数据源 enabled，N 个工具」N>0。
#
# 用它能抓到的典型问题（都已真实发生过）：
#   · 本地构建路线却执行 `docker compose pull` → set -e 中断（B-05）
#   · status.sh 把迁移号写死成 0010，加了 0011 之后体检永远 FAIL
#   · --with-asm / --no-asm 的 ASM_ENABLED 没落盘，第二次跑又拉起测绘
#
# 用法：bash scripts/stub-bootstrap.sh [-v]
# 输出：STUB_RESULT PASS / FAIL（退出码 0/1），全过程日志在临时目录，-v 时保留路径。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERBOSE=0
[ "${1:-}" = "-v" ] && VERBOSE=1

TMP="$(mktemp -d)"
BIN="$TMP/bin"
REPO="$TMP/repo"
LOG="$TMP/docker.log"
CURL_LOG="$TMP/curl.log"
OUT1="$TMP/run-with-asm.log"
OUT2="$TMP/run-no-asm.log"

cleanup() { [ "$VERBOSE" = "1" ] && echo "  临时目录：$TMP" || rm -rf "$TMP"; }
trap cleanup EXIT

FAILS=0
pass() { printf '\033[1;32m  [+] %s\033[0m\n' "$*"; }
fail() { printf '\033[1;31m  [x] %s\033[0m\n' "$*"; FAILS=$((FAILS+1)); }
inf()  { printf '      %s\n' "$*"; }

echo "== 准备临时仓库（只拷部署需要的文件）=="
mkdir -p "$REPO" "$BIN"
cp -r "$ROOT/deploy" "$REPO/deploy"
cp "$ROOT/docker-compose.yml" "$ROOT/.env.example" "$REPO/"
for f in Dockerfile Dockerfile.build; do [ -f "$ROOT/$f" ] && cp "$ROOT/$f" "$REPO/"; done
inf "repo=$REPO"

# ---------------------------------------------------------------- stubs
cat > "$BIN/uname" <<'EOF'
#!/usr/bin/env bash
case "$*" in *-m*) echo x86_64 ;; *-s*) echo Linux ;; *) echo Linux ;; esac
EOF

cat > "$BIN/id" <<'EOF'
#!/usr/bin/env bash
case "$1" in
  -u) echo 0 ;;
  -nG) echo root ;;
  *) echo "uid=0(root) gid=0(root) groups=0(root)" ;;
esac
EOF

cat > "$BIN/systemctl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cp "$BIN/systemctl" "$BIN/service"
cp "$BIN/systemctl" "$BIN/usermod"
cp "$BIN/systemctl" "$BIN/chown"

# stat：Git Bash 的 chmod 不产生 POSIX 权限位，.env 永远显示 644。
# 真机（Linux）是 600，所以这里按 Linux 语义模拟，否则体检会误报。
cat > "$BIN/stat" <<'EOF'
#!/usr/bin/env bash
case "$*" in
  *-c*%a*)
    case "${!#}" in *.env) echo 600 ;; *) echo 644 ;; esac ;;
  *) printf '%s
' "${!#}" ;;
esac
EOF

# docker：记录每条命令，给 compose ps / exec psql 编造输出
cat > "$BIN/docker" <<'EOF'
#!/usr/bin/env bash
echo "docker $*" >> "$STUB_DOCKER_LOG"
case "$1 $2" in
  "--version "*) echo "Docker version 27.0.0, build stub"; exit 0 ;;
  "info "*) exit 0 ;;
  "image inspect") echo "Error: No such image (stub)"; exit 1 ;;
  "compose version") echo "Docker Compose version v2.29.0-stub"; exit 0 ;;
esac
if [ "$1" = "compose" ]; then
  shift
  case "$1" in
    version) echo "Docker Compose version v2.29.0-stub"; exit 0 ;;
    ps)
      case "$*" in
        *"{{.State}}"*) for i in 1 2 3 4 5 6; do echo running; done ;;
        *) echo "      postgres  running"; echo "      artex  running" ;;
      esac
      exit 0 ;;
    exec)
      # dc exec -T postgres psql -U .. -d .. -tAc "SQL"
      case "$*" in
        *schema_migrations*version*) echo "0011_loot_row_state.sql" ;;
        *count\(*\)) echo 1 ;;
        *) echo "" ;;
      esac
      exit 0 ;;
    logs) echo "(stub) no container logs"; exit 0 ;;
    up|build|pull|down|stop|start) exit 0 ;;
    *) exit 0 ;;
  esac
fi
exit 0
EOF

# curl：按 URL 编造 JSON，支持 -o / -w / -f
cat > "$BIN/curl" <<'EOF'
#!/usr/bin/env bash
out=""; fmt=""; fail_on_http=0; url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -w) fmt="$2"; shift 2 ;;
    --max-time|-H|-d|-X) shift 2 ;;
    -f|-fsS|-fs) fail_on_http=1; shift ;;
    -sS|-s|-S|-L|-k|--silent|-XPOST) shift ;;
    *) url="$1"; shift ;;
  esac
done
echo "curl $url" >> "$STUB_CURL_LOG"

code=200
case "$url" in
  */api/health) body='{"ok":true,"service":"artex","version":"stub"}' ;;
  */api/auth/status) body='{"initialized":true}' ;;
  */api/auth/init) body='{"ok":true}' ;;
  */api/auth/login) body='{"token":"stub-artex-token"}' ;;
  */api/user/login) body='{"data":{"access_token":"stub-ss-token"}}' ;;
  */api/apikey/list) body='{"data":{"list":[]}}' ;;
  */api/apikey/create) body='{"data":{"key":"ssk_stub0123456789","id":"k1"}}' ;;
  */api/apikey/delete) body='{"ok":true}' ;;
  */api/sync/scopesentry/datasource) body='{"enabled":true}' ;;
  */api/sync/scopesentry/status) body='{"enabled":true,"tools":[{"name":"asm_a"},{"name":"asm_b"}]}' ;;
  */api/system/hitrate*task=__status_probe__) code=404; body='{"error":"task not found"}' ;;
  */api/system/hitrate*) body='{"tables":{"capabilities":1,"assets_owned":0},"tools":{"bash":3}}' ;;
  */api/tasks) body='{"tasks":[{"id":"7","name":"stub"}],"active":"7"}' ;;
  *:8082/*|*:8082) body='{"ok":true}' ;;
  *api.ipify.org*) body='203.0.113.7' ;;
  *) body='{"ok":true}' ;;
esac

if [ -n "$out" ]; then printf '%s' "$body" > "$out"; else printf '%s' "$body"; fi
if [ -n "$fmt" ]; then printf '%s' "${fmt//\%\{http_code\}/$code}"; fi
if [ "$fail_on_http" = "1" ] && [ "$code" -ge 400 ]; then exit 22; fi
exit 0
EOF

# jq：只实现 deploy/*.sh 里真正用到的几个表达式（--arg / -n / -r / -e）
# python 解释器由 harness 选（Windows 上 python3 可能只是商店别名），不走 shebang。
if python3 -c 'raise SystemExit(0)' >/dev/null 2>&1; then STUB_PY=python3; else STUB_PY=python; fi
export STUB_PY
cat > "$BIN/jq" <<'EOF'
#!/usr/bin/env bash
exec "$STUB_PY" "$STUB_JQ_IMPL" "$@"
EOF
cat > "$TMP/jq_impl.py" <<'EOF'
import json, re, sys

args = sys.argv[1:]
raw = exit_status = null_input = False
kv = {}
filter_expr = None
i = 0
while i < len(args):
    a = args[i]
    if a == "-r":
        raw = True
    elif a == "-e":
        exit_status = True
    elif a in ("-n", "-c"):
        null_input = True
    elif a == "--arg":
        kv[args[i+1]] = args[i+2]
        i += 2
    elif a.startswith("-"):
        pass
    else:
        filter_expr = a
    i += 1

def emit(value, fallback=None):
    if value is None:
        if fallback is None:
            return
        value = fallback
    if isinstance(value, (dict, list)):
        print(json.dumps(value, ensure_ascii=False, separators=(",", ":")))
    else:
        print(str(value).lower() if isinstance(value, bool) else value)

def dig(obj, path):
    cur = obj
    for part in path.strip(".").split("."):
        if part == "":
            continue
        idx = None
        m = re.match(r"^([^\[]*)(?:\[(\d+)\])?(\[\]\?)?$", part)
        if not m:
            return None
        key, index, spread = m.group(1), m.group(2), m.group(3)
        if key:
            if not isinstance(cur, dict) or key not in cur:
                return None
            cur = cur[key]
        if index is not None:
            if not isinstance(cur, list) or int(index) >= len(cur):
                return None
            cur = cur[int(index)]
        if spread:
            return cur
    return cur

if null_input and filter_expr and filter_expr.strip().startswith("{"):
    tpl = filter_expr
    for k, v in kv.items():
        tpl = tpl.replace("$" + k, json.dumps(v, ensure_ascii=False))
    tpl = re.sub(r'([{,]\s*)([A-Za-z_][A-Za-z0-9_]*)(\s*:)', r'\1"\2"\3', tpl)
    try:
        print(json.dumps(json.loads(tpl), ensure_ascii=False, separators=(",", ":")))
        sys.exit(0)
    except Exception:
        sys.exit(1)

data = {}
if not null_input:
    try:
        data = json.load(sys.stdin)
    except Exception:
        data = {}

f = (filter_expr or ".").strip()

# 选择器：'.data.list[]? | select(.name == $n) | .id'
m = re.match(r"^(.*?)\[\]\?\s*\|\s*select\(\.(\w+)\s*==\s*\$(\w+)\)\s*\|\s*\.(\w+)$", f)
if m:
    base, field, var, out_field = m.group(1), m.group(2), m.group(3), m.group(4)
    items = dig(data, base) or []
    for it in items if isinstance(items, list) else []:
        if isinstance(it, dict) and str(it.get(field)) == str(kv.get(var, "")):
            print(it.get(out_field, ""))
    sys.exit(0)

if f == "[.tools[]?] | length":
    tools = dig(data, ".tools") or []
    print(len(tools))
    sys.exit(0)

if f == ".ok == true":
    ok = dig(data, ".ok") is True
    if exit_status:
        sys.exit(0 if ok else 1)
    print("true" if ok else "false")
    sys.exit(0)

if f == '.tables | type == "object"':
    ok = isinstance(dig(data, ".tables"), dict)
    if exit_status:
        sys.exit(0 if ok else 1)
    print("true" if ok else "false")
    sys.exit(0)

m = re.match(r'^(.*?)\s*//\s*(empty|false|"([^"]*)")$', f)
if m:
    path, kind = m.group(1), m.group(2)
    value = dig(data, path)
    if value is None:
        if kind == "empty":
            sys.exit(0)
        emit(kind.strip('"'))
    else:
        emit(value)
    sys.exit(0)

value = dig(data, f)
if value is None:
    sys.exit(0)
emit(value)
EOF

chmod +x "$BIN"/*
export PATH="$BIN:$PATH"
export STUB_DOCKER_LOG="$LOG" STUB_CURL_LOG="$CURL_LOG" STUB_JQ_IMPL="$TMP/jq_impl.py"
: > "$LOG"; : > "$CURL_LOG"

echo
echo "== 第一次：bootstrap.sh --with-asm（含测绘）=="
( cd "$REPO" && bash deploy/bootstrap.sh --with-asm ) > "$OUT1" 2>&1
RC1=$?
if [ "$RC1" = "0" ]; then pass "退出码 0"; else fail "退出码 $RC1（日志：$OUT1）"; tail -20 "$OUT1"; fi
grep -q "STATUS PASS" "$OUT1" && pass "体检 STATUS PASS" || { fail "体检不是 PASS"; grep -E "^\s+\[x\]|STATUS" "$OUT1" | head -10; }
grep -q "接通：ARTEX 已发现 2 个测绘工具" "$OUT1" && pass "wire.sh 接通（2 个工具）" || { fail "wire.sh 未接通"; grep -E "接线|ScopeSentry|ARTEX" "$OUT1" | tail -6; }

ENV1="$REPO/.env"
if grep -q '^ASM_ENABLED=1$' "$ENV1"; then pass "ASM_ENABLED=1 已落盘"; else fail "ASM_ENABLED 不是 1"; fi
PASS_HASH1=$(grep '^ARTEX_PASSWORD=' "$ENV1" | cut -d= -f2-)
grep -q -- "--profile asm" "$LOG" && pass "compose 收到 --profile asm" || fail "compose 没收到 --profile asm"
if grep -qE '(^| )pull( |$)' "$LOG"; then fail "出现了 docker compose pull（本地构建路线不该 pull）"; else pass "全程没有 pull"; fi
grep -q "docker compose .*up -d" "$LOG" && pass "compose up 被调用" || fail "compose up 没被调用"

echo
# 第二次单独记日志：不然后面的「没有 --profile asm」会被第一次的输出干扰。
: > "$LOG"
echo "== 第二次：bootstrap.sh --no-asm（幂等 + 关掉测绘）=="
( cd "$REPO" && bash deploy/bootstrap.sh --no-asm ) > "$OUT2" 2>&1
RC2=$?
if [ "$RC2" = "0" ]; then pass "退出码 0"; else fail "退出码 $RC2（日志：$OUT2）"; tail -20 "$OUT2"; fi
grep -q "STATUS PASS" "$OUT2" && pass "体检 STATUS PASS" || { fail "体检不是 PASS"; grep -E "^\s+\[x\]|STATUS" "$OUT2" | head -10; }
if grep -q '^ASM_ENABLED=0$' "$ENV1"; then pass "ASM_ENABLED=0 已落盘"; else fail "ASM_ENABLED 没被改成 0"; fi
PASS_HASH2=$(grep '^ARTEX_PASSWORD=' "$ENV1" | cut -d= -f2-)
[ -n "$PASS_HASH1" ] && [ "$PASS_HASH1" = "$PASS_HASH2" ] && pass ".env 密码幂等（重跑不换密码）" || fail "重跑后 ARTEX_PASSWORD 变了"
# 第二次的 compose up 不应再带 --profile asm（$LOG 已在第二次开始前清空）
if grep "up -d" "$LOG" | grep -q -- "--profile asm"; then
  fail "第二次仍带了 --profile asm"
else
  pass "第二次没再拉测绘"
fi
if grep -q "ScopeSentry" "$OUT2" && grep -q "本次未启动" "$OUT2"; then pass "输出明确写了测绘未启动"; fi

echo
echo "== 检查 status.sh 的迁移判据 =="
grep -q "schema_migrations" "$OUT1" && pass "status.sh 读到了迁移号" || fail "status.sh 没读到迁移号"
grep -q "已到 0011" "$OUT1" && pass "迁移判据跟着 0011 走（不是写死 0010）" || fail "迁移判据没更新到 0011"

echo
if [ "$FAILS" -eq 0 ]; then
  echo "STUB_RESULT PASS"
  exit 0
fi
echo "STUB_RESULT FAIL（$FAILS 项）"
exit 1
