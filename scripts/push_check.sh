#!/usr/bin/env bash
# 清理机器 / 换机 / 重装系统**之前**跑这个：确认 GitHub 上真的是最新的一份。
#
# 背景：本项目曾出现"本地领先 59 个提交、GitHub 还停在上游 v0.5.9"的状态。
# 那时候清掉本地目录 = 全部工作蒸发。所以把检查做成脚本，不靠记性。
#
# 用法：bash scripts/push_check.sh
# 退出码：0 = 已同步可以放心清理；1 = 有未推送/未提交的东西，先处理
set -uo pipefail
cd "$(dirname "$0")/.."
FAIL=0

echo "== 1/4 远端 =="
REMOTE=$(git remote get-url origin 2>/dev/null || echo "")
echo "origin = ${REMOTE:-<未配置>}"
git remote get-url upstream >/dev/null 2>&1 && echo "注意：仓库里还有 upstream 远端（上游作者），别推错地方"

echo "== 2/4 未提交改动 =="
DIRTY=$(git status --porcelain | grep -v '^!!' | wc -l | tr -d ' ')
if [ "$DIRTY" != "0" ]; then
  echo "[!] 有 $DIRTY 个未提交改动："
  git status --short | head -10
  FAIL=1
else
  echo "OK  工作区干净"
fi

echo "== 3/4 未推送提交 =="
git fetch origin --quiet 2>/dev/null || echo "[!] fetch 失败（网络/凭据），下面的比较可能不准"
AHEAD=$(git rev-list --count origin/main..HEAD 2>/dev/null || echo "?")
BEHIND=$(git rev-list --count HEAD..origin/main 2>/dev/null || echo "?")
echo "本地领先 origin/main: ${AHEAD}   落后: ${BEHIND}"
if [ "$AHEAD" != "0" ]; then
  echo "[!] 有 $AHEAD 个提交只在本地 —— 清掉本地目录就没了。先 git push origin HEAD:main"
  git log --oneline origin/main..HEAD | head -10
  FAIL=1
fi

echo "== 4/4 不在仓库里但你可能想留的东西 =="
for f in config.json jwt.key; do
  [ -f "$f" ] && echo "  $f  ← 含凭据，故意不上传 GitHub；要保留请自己复制走"
done
[ -d dist ] && echo "  dist/  ← 成品目录（GitHub Releases 里有打包好的 zip，可不留）"
echo "  数据库：本地 PG 是临时的，重装后重建即可（INSTALL-AI.md 有步骤）"

echo
if [ "$FAIL" = "0" ]; then
  echo "✅ 已同步：GitHub 上的就是最新一份，可以放心清理/换机。"
  exit 0
else
  echo "❌ 未同步：先 push 并提交，再清理本地。"
  exit 1
fi
