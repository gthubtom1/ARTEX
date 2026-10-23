-- 0008_review_ack.sql — 审查官 ack 窗口（无人值守时的「你不在我才动」）
-- 幂等：全部 ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS；只加列与索引，不改既有语义。
-- 文件内不写 BEGIN/COMMIT（runner 会剥离，见 db/migrations.go stripTxControl）。
--
-- 唯一事实源：docs/14-复盘官-最终方案.md §4（ack 窗口）与 §6（状态机）。
-- 纪律：这三列只记「人有没有回应 / 什么时候该自己处理」——
--       超时后低风险动作最多「减浪费」，提新方向/换阶段只写成建议，绝不改目标、绝不收工。

-- ───────── 1. review_runs：ack 回执状态 / 回执时间 / 回执截止 ─────────
-- ack_status 在 0006 已经建过（默认 ''），这里保留 IF NOT EXISTS 以兼容手工做过一半迁移的库。
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS ack_status   TEXT        NOT NULL DEFAULT '';
    -- ''=还没人回应 | held=我在，我自己处理 | accepted=采纳 | rejected=不要 | escalated=超时按档位处理 | expired
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS ack_at       TIMESTAMPTZ;             -- 人回应（或超时处理）的时间
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS ack_deadline TIMESTAMPTZ;             -- 无人值守窗口：now()+90s，只有要动作的投递才有

-- 20 秒粒度的扫描只查「到点且还没人回应」的少量行。
CREATE INDEX IF NOT EXISTS idx_review_runs_ack_pending
    ON review_runs (ack_deadline) WHERE ack_status = '' AND ack_deadline IS NOT NULL;
