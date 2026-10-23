-- 0006_review_loop.sql — C-01 审查官闭环：分级投递 / 去重 / 回执 / 自动动作 / 熔断状态
-- 幂等：全部 IF NOT EXISTS；只新增列与表，不改既有语义；文件内不写 BEGIN/COMMIT（runner 会剥离）。
--
-- 唯一事实源：docs/13-复盘官-闭环与防抖.md（§7 落库字段）、docs/12-功能台账.md（补齐方案）。
-- 纪律：这些列只记「它说了什么 / 谁认了 / 它自己做了什么」——不复制战果，不造第二真相源。

-- ───────── 1. review_runs：分级 / 去重键 / 回执 / 自动执行标记 ─────────
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS dedupe_key   TEXT        NOT NULL DEFAULT '';
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS level        TEXT        NOT NULL DEFAULT 'read';  -- read|action
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS reason       TEXT        NOT NULL DEFAULT '';      -- 触发原因(cond/timer/manual/rebut)
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS reason_class TEXT        NOT NULL DEFAULT '';      -- retry:self_error / kill:target_blocked
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS seen_at      TIMESTAMPTZ;                          -- 人看过(未读红点依据)
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS ack_status   TEXT        NOT NULL DEFAULT '';      -- accepted|rejected|stale|''
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS ack_reason   TEXT        NOT NULL DEFAULT '';
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS auto_applied BOOLEAN     NOT NULL DEFAULT false;

-- R1 只对变化说话：同 (task, dedupe_key) 查历史用这条索引。
CREATE INDEX IF NOT EXISTS idx_review_runs_dedupe
    ON review_runs (task_id, dedupe_key) WHERE dedupe_key <> '';
-- 未读红点：只查未读的少量行。
CREATE INDEX IF NOT EXISTS idx_review_runs_unseen
    ON review_runs (task_id, created_at DESC) WHERE seen_at IS NULL;

-- ───────── 2. review_state：每任务一份，记配额 / 上次投递 / 熔断 ─────────
CREATE TABLE IF NOT EXISTS review_state (
    task_id         BIGINT      PRIMARY KEY,
    tier            TEXT        NOT NULL DEFAULT '',   -- ''=跟全局设置；observe|lowrisk|auto
    interventions   INTEGER     NOT NULL DEFAULT 0,    -- 已投递次数（A+B 合计，R3 配额）
    auto_actions    INTEGER     NOT NULL DEFAULT 0,    -- 已自动执行动作数（R3 上限 5）
    last_sent_at    TIMESTAMPTZ,                       -- 上次投递时间（R2 冷却）
    last_facts      INTEGER     NOT NULL DEFAULT 0,    -- 上次介入时的 fact 数（R6 无改善判定）
    tripped_at      TIMESTAMPTZ,                       -- 熔断时间（非空=已转 observe）
    trip_reason     TEXT        NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
