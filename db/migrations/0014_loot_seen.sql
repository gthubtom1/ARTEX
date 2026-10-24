-- 0014_loot_seen.sql — 战果未读游标（P0-b 红点依据）。
-- 幂等：IF NOT EXISTS；只新增一张表；文件内不写 BEGIN/COMMIT。
--
-- 为什么：用户要"有新战果时胶囊上有数字"。战果表的行是**实时算**出来的，
-- 没有 created_at 也没有"读过"标记，所以只能记一个游标：
--   seen_finding_id = 用户上次点开战果面板时，这个任务里最大的 finding id。
-- 未读数 = 已拿到（done）的行里，证据 id 最大值 > 游标的行数（见 server/brief.go countUnreadLoot）。
--
-- 纪律：
--   · 只存游标，不存行快照 —— 行永远由规则实时算，游标只是"看到哪儿了"；
--   · 一个任务一行（PRIMARY KEY task_id），重复标记是覆盖（UPSERT）；
--   · 没有这一行 = 从没看过 = 全都算未读（首屏就有红点，符合直觉）。
CREATE TABLE IF NOT EXISTS loot_seen (
    task_id         BIGINT      PRIMARY KEY,
    seen_finding_id BIGINT      NOT NULL DEFAULT 0,   -- 上次看到的最大 finding id
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
