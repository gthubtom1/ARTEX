-- 0016_action_runs.sql — 业务动作工单（P2）：对话即授权 + 留证 + 证据驱动收口。
-- 幂等：IF NOT EXISTS；只新增一张表；文件内不写 BEGIN/COMMIT。
--
-- 为什么需要它：战况卡已经能列出"我现在能做到的 7 件事"，但用户说不清
-- "我说了要做、到底做了没有、证据在哪"。这里把这条链落成可核对的工单：
--
--   requested —— 人在主对话里说了「帮我…」，后端按关键词登记（say_it 存授权原文）
--   done      —— 该动作对应的战果行出现了**新证据**（base_ref 之后），自动收口
--   failed    —— 人/审查官显式回报失败（附原因）
--   aborted   —— 人放弃（例如发现前置条件不成立）
--
-- 纪律：
--   · 永远不自动执行：这张表只记"人要求过 / 有没有结果"，执行仍由 agent 用工具做；
--   · base_ref 是登记时刻的最大证据 id：只有**新证据**才算这次做成了，
--     不会拿历史战果冒充本次结果（同 P0-c 的 hash 缓存思路）；
--   · 一个 (task_id, action_class) 只保留最新一条有效工单（见 idx_action_runs_open），
--     避免同一条指令被重复登记成一堆工单。
CREATE TABLE IF NOT EXISTS action_runs (
    id           BIGSERIAL   PRIMARY KEY,
    task_id      BIGINT      NOT NULL,
    action_class TEXT        NOT NULL,               -- data_export|account_read|account_create|money_adjust|money_approve|deface|persist
    say_it       TEXT        NOT NULL DEFAULT '',    -- 人的原话（授权的凭据）
    state        TEXT        NOT NULL DEFAULT 'requested', -- requested|done|failed|aborted
    base_ref     BIGINT      NOT NULL DEFAULT 0,     -- 登记时刻 max(证据 id)
    evidence     TEXT        NOT NULL DEFAULT '',    -- 留证：请求/响应摘要、行数、改前改后
    evidence_ref BIGINT      NOT NULL DEFAULT 0,     -- 关联的 finding/证据 id（0=还没关联）
    note         TEXT        NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_action_runs_task
    ON action_runs (task_id, created_at DESC);

-- 每个任务每类动作最多一条"进行中"工单：重复发话只更新原话，不新增。
CREATE UNIQUE INDEX IF NOT EXISTS idx_action_runs_open
    ON action_runs (task_id, action_class) WHERE state = 'requested';
