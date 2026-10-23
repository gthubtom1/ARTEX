-- 0010_error_intent.sql — 错误事件带上意图 id（复盘官「意图↔错误」精确归属）
-- 幂等：ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS；只加列与索引，不改既有语义。
-- 文件内不写 BEGIN/COMMIT（runner 会剥离，见 db/migrations.go stripTxControl）。
--
-- 为什么：0010 之前 task_error_events 只有 task_id + tool（"worker:work#N"，N 是 worker
-- 序号，不是意图 id）+ 纯错误文本。复盘官快照只能靠 detail 子串里出现意图 id 来猜归属，
-- 于是正常情况永远猜不中（B 级动作几乎不产生），偶尔猜中也是 IP/端口数字撞上意图 id
-- （实测 task#2 run#19 把 70/43/17 判成重开，真正 blocked 的是 20/47/53）。
--
-- 纪律：intent_id=0 表示「归属未知」（历史行 / 平台级事件 / 非 worker 调用），
--       永远不要用 0 去精确匹配任何意图；快照只对 intent_id=0 的行启用旧的子串兜底。

ALTER TABLE task_error_events ADD COLUMN IF NOT EXISTS intent_id BIGINT NOT NULL DEFAULT 0;

-- 快照按 (task_id, intent_id) 取最近 6 小时的错误，ts DESC 让「最新一条」排在最前。
CREATE INDEX IF NOT EXISTS idx_task_error_events_intent
    ON task_error_events (task_id, intent_id, ts DESC);
