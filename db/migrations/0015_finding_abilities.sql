-- 0015_finding_abilities.sql — 战况卡 P0-c：每条漏洞「能换到什么」的 AI 解释缓存。
-- 幂等：IF NOT EXISTS；只新增一张表；文件内不写 BEGIN/COMMIT。
--
-- 为什么单独一张表：loot_explanations 的键是 (task_id,row_key)，row 是"能力行"；
-- 战况卡要的是"每条 finding 一句现实后果"，粒度是 finding，混用会让两边互相踩键。
--
-- 纪律：
--   · evidence_hash = sha256(标题|摘要|证据原文)，证据一变 hash 就变，旧解释自然失效；
--   · 只存 AI 写的一句话（status=done 才用），状态/等级/已拿到仍由规则实时算；
--   · (task_id,finding_id) 唯一：同一条漏洞永远只留最新一次解释，不会越积越多。
CREATE TABLE IF NOT EXISTS finding_abilities (
    task_id       BIGINT      NOT NULL,
    finding_id    BIGINT      NOT NULL,
    evidence_hash TEXT        NOT NULL DEFAULT '',
    status        TEXT        NOT NULL DEFAULT 'done',   -- done|failed
    text          TEXT        NOT NULL DEFAULT '',       -- 一句话：这个洞能换来什么
    model         TEXT        NOT NULL DEFAULT '',       -- adviser|rule
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, finding_id)
);

CREATE INDEX IF NOT EXISTS idx_finding_abilities_task
    ON finding_abilities (task_id);
