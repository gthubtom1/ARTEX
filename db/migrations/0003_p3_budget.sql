-- 0003_p3_budget.sql — 决策预算（continue 的硬熔断依据）
-- 幂等；由 db.applyMigrations 在启动锁内执行（文件内不写事务）。

ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS budget_started_at TIMESTAMPTZ;
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS budget_minutes    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS budget_rounds     INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_caps_budget ON capabilities (task_id, budget_started_at)
    WHERE budget_started_at IS NOT NULL;
