-- 0011_loot_row_state.sql — 战果清单的人工状态（B-07）。
-- 幂等：IF NOT EXISTS；只新增表与索引，不改既有语义；文件内不写 BEGIN/COMMIT。
--
-- 为什么：战果表的 status 一直由规则实时算（done/run/tried/blank），
-- blank 的行**不显示**。用户想"这行我试过了"或"这行跟我这个站无关"时没有入口，
-- 于是面板永远只剩机器算出来的那几行，人判断不了"还差哪几项没动"。
--
-- 纪律：state 只表达**人的判断**，不覆盖证据。
--   · 有 finding 证据 → 永远 done（证据优先，人标的 na 不会把已拿到的战果藏起来）；
--   · 没有证据时才用 state 决定显示：todo（还没试）/ na（不适用）；
--   · 同一个 (task_id,row_key) 只有一行，重复标记是覆盖（UPSERT），取消标记是删除。

CREATE TABLE IF NOT EXISTS loot_row_states (
    task_id    BIGINT      NOT NULL,
    row_key    TEXT        NOT NULL,                  -- 与 lootRows 的 def.key 对齐（AI 推断的行用自己的 slug）
    state      TEXT        NOT NULL,                  -- todo | na
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, row_key)
);

-- 战果接口按 task_id 一次取全部标记，索引只需覆盖 task_id。
CREATE INDEX IF NOT EXISTS idx_loot_row_states_task
    ON loot_row_states (task_id);
