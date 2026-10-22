-- 0002_p3_decisions.sql — 决策队列（能力卡裁决）与复查执行
-- 幂等；由 db.applyMigrations 在启动锁内执行（文件内不写事务）。

-- 能力卡裁决留痕：谁在什么时候决定了什么（只追加，不改历史）。
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS decided_at    TIMESTAMPTZ;
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS decided_by    TEXT NOT NULL DEFAULT '';
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS decision_note TEXT NOT NULL DEFAULT '';
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS decision_action TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_caps_decided ON capabilities (task_id, decided_at DESC)
    WHERE decided_at IS NOT NULL;

-- 复查执行器：记录"是谁跑的、命令是什么"，便于事后复现。
ALTER TABLE asset_checks ADD COLUMN IF NOT EXISTS command TEXT NOT NULL DEFAULT '';
ALTER TABLE asset_checks ADD COLUMN IF NOT EXISTS executor TEXT NOT NULL DEFAULT '';

-- 复查任务游标：避免多实例重复跑同一轮（单行 upsert）。
CREATE TABLE IF NOT EXISTS recheck_state (
    id         INT PRIMARY KEY DEFAULT 1,
    last_run_at TIMESTAMPTZ,
    last_detail TEXT NOT NULL DEFAULT '',
    CONSTRAINT recheck_state_singleton CHECK (id = 1)
);
INSERT INTO recheck_state (id) VALUES (1) ON CONFLICT DO NOTHING;
