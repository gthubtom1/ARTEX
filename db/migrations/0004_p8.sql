-- 0004_p8.sql — P8 阶段新增：台账连续失败计数、决策冻结资产、任务级错误事件、基准跑分摘要。
-- 幂等：全部 IF NOT EXISTS；只加列不改语义；文件内不写 BEGIN/COMMIT（runner 会剥离）。

-- ─────────────────────────── 1. 台账连续失败计数（P8-5） ───────────────────────────
ALTER TABLE assets_owned ADD COLUMN IF NOT EXISTS consecutive_fails INTEGER NOT NULL DEFAULT 0;

-- ───────────────────── 2. 决策「换目标」的冻结资产落库（P8-5） ─────────────────────
-- decide_capability(action=switch) 时把 decision.Verdict.FreezeAssetIDs 写进来，
-- add_intent 之前查这个集合，命中就拒绝为该资产派单（让"换目标"按钮真的生效）。
ALTER TABLE capabilities ADD COLUMN IF NOT EXISTS frozen_asset_ids BIGINT[] NOT NULL DEFAULT '{}';

-- ─────────────────────── 3. 任务级错误事件（P8-8 错误可解释面板） ───────────────────────
-- 无外键：删任务不丢审计。class 固定 5 类，见 internal/errev。
CREATE TABLE IF NOT EXISTS task_error_events (
    id      BIGSERIAL PRIMARY KEY,
    task_id BIGINT      NOT NULL DEFAULT 0,
    ts      TIMESTAMPTZ NOT NULL DEFAULT now(),
    class   TEXT        NOT NULL DEFAULT '',
    tool    TEXT        NOT NULL DEFAULT '',
    detail  TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_task_error_events_task ON task_error_events (task_id, ts DESC);

-- ─────────────────────── 4. 基准跑分摘要（P8-1，明细落 bench/results/*.json） ───────────────────────
CREATE TABLE IF NOT EXISTS bench_runs (
    id            BIGSERIAL PRIMARY KEY,
    run_key       TEXT        NOT NULL UNIQUE,
    set_name      TEXT        NOT NULL DEFAULT '',
    model_profile TEXT        NOT NULL DEFAULT '',
    total         INTEGER     NOT NULL DEFAULT 0,
    passed        INTEGER     NOT NULL DEFAULT 0,
    duration_ms   BIGINT      NOT NULL DEFAULT 0,
    total_tokens  BIGINT      NOT NULL DEFAULT 0,
    notes         TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_bench_runs_created ON bench_runs (created_at DESC);
