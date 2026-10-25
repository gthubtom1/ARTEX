#!/usr/bin/env bash
# 打印一个短期 token（给宿主侧手工用；判据脚本请改用 _token.sh 的 artex_token）。
set -u
. "$(dirname "$0")/_token.sh"
artex_token
