-- 0007_loot_explain.sql — 战果清单 L1/L2 解释缓存。
-- 幂等：IF NOT EXISTS；只新增表与索引，不改既有语义；文件内不写 BEGIN/COMMIT（runner 会剥离）。
-- 纪律：只存 AI 写出的解释文本，不复制战果本身；状态与 plain/objects/proof_state 仍由规则实时计算。

CREATE TABLE IF NOT EXISTS loot_explanations (
    task_id       BIGINT      NOT NULL,
    row_key       TEXT        NOT NULL,                  -- 普通行 key | 'suggest'
    evidence_hash TEXT        NOT NULL DEFAULT '',       -- sha256(该行/该任务 findings 摘要)，变化即失效
    status        TEXT        NOT NULL DEFAULT 'done',   -- done|failed
    text          TEXT        NOT NULL DEFAULT '',       -- L1 人话 / L2 rows JSON
    model         TEXT        NOT NULL DEFAULT '',       -- adviser|rule
    tokens_in     INTEGER     NOT NULL DEFAULT 0,
    tokens_out    INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, row_key, evidence_hash)
);

CREATE INDEX IF NOT EXISTS idx_loot_explanations_task
    ON loot_explanations (task_id, row_key, created_at DESC);
