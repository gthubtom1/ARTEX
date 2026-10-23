#!/usr/bin/env bash
# 把 shortlist.jsonl 逐条派给 ARTEX（POST /api/tasks/quickstart），限并发。
# 用法：bash triage/dispatch.sh triage/out/shortlist.jsonl [并发数]
#   TARGET_TIER=legacy  目标档位：lab（本地靶场）| legacy（有已知漏洞族的老站）| public（只读低风险）
#   TIMEOUT_MIN=30      单任务上限分钟
set -euo pipefail
cd "$(dirname "$0")/.."
set -a; . ./.env; set +a

LIST="${1:?用法: bash triage/dispatch.sh <shortlist.jsonl> [并发数]}"
CONC="${2:-2}"
TIER="${TARGET_TIER:-legacy}"
MINUTES="${TIMEOUT_MIN:-30}"
ARTEX="http://127.0.0.1:${ARTEX_PORT:-8787}"
OUT="data/dispatched.tsv"

TOKEN=$(curl -sS -X POST "$ARTEX/api/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"ARTEX\",\"password\":\"${ARTEX_PASSWORD}\"}" | jq -r '.token // empty')
[ -n "$TOKEN" ] || { echo "[x] ARTEX 登录失败"; exit 1; }

mkdir -p data
[ -f "$OUT" ] || printf 'task_id\turl\tts\n' > "$OUT"

# 跳过已派过的 URL
done_urls=$(cut -f2 "$OUT" | tail -n +2 | sort -u)

dispatch_one() {
  url="$1"; score="$2"
  if grep -qxF "$url" <<<"$done_urls"; then
    echo "  skip(已派) $url"; return 0
  fi
  tier="$TIER"
  case "$url" in
    http://127.0.0.1*|http://localhost*) tier=lab ;;
  esac
  resp=$(curl -sS -X POST "$ARTEX/api/tasks/quickstart" \
    -H 'Content-Type: application/json' \
    -H "Authorization: Bearer $TOKEN" \
    -d "$(jq -nc --arg u "$url" --arg t "$tier" --argjson m "$MINUTES" '{url:$u,tier:$t,timeout_min:$m}')")
  id=$(printf '%s' "$resp" | jq -r '.id // .task_id // .data.id // empty')
  if [ -n "$id" ]; then
    printf '%s\t%s\t%s\n' "$id" "$url" "$(date -Is)" >> "$OUT"
    echo "  [+] task=$id [$score] $url"
  else
    echo "  [x] 失败 $url → $(printf '%s' "$resp" | head -c 160)"
  fi
}
export -f dispatch_one
export ARTEX TOKEN TIER MINUTES OUT done_urls

echo "[*] 派单：$(wc -l < "$LIST") 条，并发 $CONC，档位 $TIER，单任务 ${MINUTES}min"
jq -r '[.url, (.score|tostring)] | @tsv' "$LIST" \
  | xargs -P "$CONC" -I{} bash -c 'IFS=$'"'"'\t'"'"' read -r u s <<< "{}"; dispatch_one "$u" "$s"'

echo
echo "[+] 已派单记录：$OUT （$(($(wc -l < "$OUT")-1)) 条）"
echo "    看进度：ARTEX → 任务列表；或 curl -H \"Authorization: Bearer \$TOKEN\" $ARTEX/api/tasks"
