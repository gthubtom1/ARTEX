-- 0013_chains.sql — 利用链（把"漏洞→授权→收益"串成一条可回溯的链）
-- 幂等：IF NOT EXISTS；只新增表；文件内不写 BEGIN/COMMIT。
--
-- 为什么：用户判定「串联漏洞和线索才是渗透本质」。此前每个漏洞是孤立节点，
-- 没有地方记录"这条洞走到了哪一步、卡在哪个授权、最后拿到了什么"。
-- 链是**派生**的（由 findings / task_grants / 收益证据算出来），但落库保存，
-- 这样跨轮次、跨会话都能累计与统计（例如"平均几步拿到 shell"）。
CREATE TABLE IF NOT EXISTS chains (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT      NOT NULL,
    chain_key  TEXT        NOT NULL,            -- F<finding_id>，一条漏洞一条链
    title      TEXT        NOT NULL DEFAULT '',
    status     TEXT        NOT NULL DEFAULT 'open',  -- open|done|dead
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (task_id, chain_key)
);

CREATE TABLE IF NOT EXISTS chain_steps (
    id          BIGSERIAL PRIMARY KEY,
    task_id     BIGINT      NOT NULL,
    chain_key   TEXT        NOT NULL,
    step_key    TEXT        NOT NULL,           -- confirm|authorize:<class>|gain:<kind>
    seq         INTEGER     NOT NULL DEFAULT 0,
    kind        TEXT        NOT NULL DEFAULT '',-- entry|authorize|gain
    title       TEXT        NOT NULL DEFAULT '',
    status      TEXT        NOT NULL DEFAULT 'pending', -- done|pending|failed
    ref_id      BIGINT      NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (task_id, chain_key, step_key)
);
CREATE INDEX IF NOT EXISTS idx_chain_steps_task ON chain_steps (task_id, chain_key, seq);
