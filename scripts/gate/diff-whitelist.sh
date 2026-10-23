#!/usr/bin/env bash
# 门禁 M2/M3：检查本次改动是否只落在任务卡允许的文件里。
#
# 用法:
#   scripts/gate/diff-whitelist.sh docs/06-任务卡/CARD-B01.md
#   scripts/gate/diff-whitelist.sh docs/06-任务卡/CARD-B01.md --staged   # 只查已暂存
#
# 卡片里需要两个围栏代码块（缺省视为空，不做限制）：
#   ```whitelist        ← 允许改动的文件（前缀匹配）
#   web/src/app/(main)/function/tasks/detail/_tabs/cando-tab.tsx
#   server/cando.go
#   ```
#   ```forbidden        ← 明令禁止改动的文件（精确/前缀匹配），命中即失败
#   function/capabilities/page.tsx
#   ```
#
# 退出码: 0 = 通过；1 = 有越界改动；2 = 卡文件/围栏缺失

set -uo pipefail
cd "$(dirname "$0")/../.." || exit 2

CARD="${1:-}"
MODE="${2:-}"

if [ -z "$CARD" ] || [ ! -f "$CARD" ]; then
  echo "用法: $0 <任务卡.md> [--staged]" >&2
  exit 2
fi

# 抽出某个围栏块的内容
extract_block() {
  awk -v tag="$1" '
    $0 ~ "^```"tag"[[:space:]]*$" { inb=1; next }
    inb && $0 ~ "^```[[:space:]]*$" { inb=0; next }
    inb { print }
  ' "$CARD" | sed -e 's/[[:space:]]*$//' -e '/^$/d' -e '/^#/d'
}

WHITELIST="$(extract_block whitelist)"
FORBIDDEN="$(extract_block forbidden)"

if [ -z "$WHITELIST" ] && [ -z "$FORBIDDEN" ]; then
  echo "✗ 卡片里没有 whitelist / forbidden 围栏，无法做门禁检查" >&2
  exit 2
fi

# 改动清单：已暂存 or 工作区（含未跟踪）
if [ "$MODE" = "--staged" ]; then
  CHANGED="$(git -c core.quotepath=false diff --cached --name-only)"
else
  CHANGED="$( { git -c core.quotepath=false diff --name-only; git -c core.quotepath=false diff --cached --name-only; git -c core.quotepath=false ls-files --others --exclude-standard; } | sort -u )"
fi

if [ -z "$CHANGED" ]; then
  echo "✓ 没有改动，门禁通过"
  exit 0
fi

match_prefix() {  # $1=file  $2=pattern 列表（换行分隔）
  local f="$1"
  while IFS= read -r pat; do
    [ -z "$pat" ] && continue
    case "$f" in
      "$pat"|"$pat"/*) return 0 ;;
    esac
    # 也支持 "xxx.*" 这种极简通配
    # shellcheck disable=SC2254
    case "$f" in
      $pat) return 0 ;;
    esac
  done <<EOF
$2
EOF
  return 1
}

BAD=0
VIOL_FORBIDDEN=""
VIOL_OUTSIDE=""

while IFS= read -r f; do
  [ -z "$f" ] && continue
  if [ -n "$FORBIDDEN" ] && match_prefix "$f" "$FORBIDDEN"; then
    VIOL_FORBIDDEN="${VIOL_FORBIDDEN}${f}
"
    BAD=1
    continue
  fi
  if [ -n "$WHITELIST" ] && ! match_prefix "$f" "$WHITELIST"; then
    VIOL_OUTSIDE="${VIOL_OUTSIDE}${f}
"
    BAD=1
  fi
done <<EOF
$CHANGED
EOF

if [ "$BAD" -eq 0 ]; then
  echo "✓ 门禁 M2/M3 通过：$(printf '%s\n' "$CHANGED" | wc -l | tr -d ' ') 个改动文件全部在卡片允许范围内"
  exit 0
fi

echo "✗ 门禁未通过"
if [ -n "$VIOL_FORBIDDEN" ]; then
  echo
  echo "命中「不许动」清单："
  printf '%s' "$VIOL_FORBIDDEN" | sed 's/^/  - /'
fi
if [ -n "$VIOL_OUTSIDE" ]; then
  echo
  echo "越界改动（不在 whitelist 内）："
  printf '%s' "$VIOL_OUTSIDE" | sed 's/^/  - /'
  echo
  echo "要么改回去，要么先在任务卡的 whitelist 里补上并说明理由。"
fi
exit 1
