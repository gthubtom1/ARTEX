-- 0005_cando_review.sql — 二开「能做到什么」地图 + 复盘官 所需四张表。
-- 幂等：全部 IF NOT EXISTS；只新增不改语义；文件内不写 BEGIN/COMMIT（runner 会剥离）。
--
-- 设计约束（见 docs/06-判定标准-冻结.md）：
--   1. 这两组表**只存解释文本 / 评审结论**，不复制战果本身（不造第二真相源）；
--   2. 战果一律现查 capabilities / findings / assets_owned / exploration_nodes。

-- ───────────── 1. 地图按需细化（B2）：只存 AI 写的人话 + 证据引用 ─────────────
CREATE TABLE IF NOT EXISTS cando_annotations (
    id            BIGSERIAL PRIMARY KEY,
    task_id       BIGINT      NOT NULL,
    node_key      TEXT        NOT NULL DEFAULT '',   -- 'ep:/Templatize.asp' | 'err:target_blocked' | 'neg'
    evidence_hash TEXT        NOT NULL DEFAULT '',   -- sha256(该节点证据集合)，变化即失效
    status        TEXT        NOT NULL DEFAULT 'pending', -- pending|done|failed
    body          TEXT        NOT NULL DEFAULT '',   -- AI 写的人话（粗粒度：只定性不写数量）
    cites         JSONB       NOT NULL DEFAULT '[]', -- [{"kind":"finding","id":31},...]
    model         TEXT        NOT NULL DEFAULT '',
    tokens_in     INTEGER     NOT NULL DEFAULT 0,
    tokens_out    INTEGER     NOT NULL DEFAULT 0,
    error         TEXT        NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ,
    UNIQUE (task_id, node_key, evidence_hash)
);
CREATE INDEX IF NOT EXISTS idx_cando_annotations_task
    ON cando_annotations (task_id, node_key, created_at DESC);

-- ───────────── 2. 复盘官记忆（唯一"全局观"载体，每任务一行）─────────────
CREATE TABLE IF NOT EXISTS review_notes (
    task_id    BIGINT      PRIMARY KEY,
    version    INTEGER     NOT NULL DEFAULT 1,
    body       TEXT        NOT NULL DEFAULT '',   -- ≤400 token，固定六行，见 docs/09 §5.1
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ───────────── 3. 每次介入（快照指纹 + 结论，可回放）─────────────
CREATE TABLE IF NOT EXISTS review_runs (
    id            BIGSERIAL PRIMARY KEY,
    task_id       BIGINT      NOT NULL,
    trigger       TEXT        NOT NULL DEFAULT 'manual', -- cond|timer|manual|rebut
    snapshot_hash TEXT        NOT NULL DEFAULT '',
    round_no      BIGINT      NOT NULL DEFAULT 0,
    facts         INTEGER     NOT NULL DEFAULT 0,
    status        TEXT        NOT NULL DEFAULT 'queued', -- queued|running|done|superseded|self_resolved|failed
    verdict       TEXT        NOT NULL DEFAULT '',       -- continue|pivot|wrapup
    body          TEXT        NOT NULL DEFAULT '',       -- 人话结论
    cites         JSONB       NOT NULL DEFAULT '[]',     -- [{"kind":"intent","id":20},...]
    actions       JSONB       NOT NULL DEFAULT '[]',     -- [{"kind":"kill_intent","intent":21,"why":"..."}]
    tokens_in     INTEGER     NOT NULL DEFAULT 0,
    tokens_out    INTEGER     NOT NULL DEFAULT 0,
    model         TEXT        NOT NULL DEFAULT '',
    error         TEXT        NOT NULL DEFAULT '',
    notes_version INTEGER     NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at   TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_review_runs_task ON review_runs (task_id, created_at DESC);

-- ───────────── 4. 采纳 / 反驳 / 效果账本（可信度来源）─────────────
CREATE TABLE IF NOT EXISTS review_feedback (
    id         BIGSERIAL PRIMARY KEY,
    run_id     BIGINT      NOT NULL REFERENCES review_runs(id) ON DELETE CASCADE,
    action     TEXT        NOT NULL DEFAULT '',  -- applied|rebutted|ignored
    by_user    TEXT        NOT NULL DEFAULT '',
    rebuttal   TEXT        NOT NULL DEFAULT '',
    outcome    TEXT        NOT NULL DEFAULT '',  -- effective|neutral|wrong（3 轮后回填）
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_review_feedback_run ON review_feedback (run_id);
