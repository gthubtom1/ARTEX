-- 0009_review_expect.sql — 审查官「可观测预期 + 观察窗」（docs/15 双向交互协议）
-- 幂等：全部 ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS；只加列与索引，不改既有语义。
-- 文件内不写 BEGIN/COMMIT（runner 会剥离，见 db/migrations.go stripTxControl）。
--
-- 唯一事实源：docs/15-复盘官-双向交互协议.md §3（生命周期）与 §7（落地增量 1）。
-- 纪律：expectation 是「这条建议怎么算起效」的一句话；observe_until 到点由 review ticker
--       回访；outcome 只记 effective|ineffective|voided，空串 = 还在观察窗里。

-- ───────── 1. review_runs：预期信号 / 观察窗到期 / 观察结论 ─────────
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS expectation   TEXT        NOT NULL DEFAULT '';
    -- 一句话、可机检的预期，例："3 轮内出现涉及 IIS 配置的新事实"。
    -- E1：有动作（level=action）但这里为空 → 门直接丢弃，不投递。
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS observe_until TIMESTAMPTZ;              -- 观察窗到期时间（有动作且已投递 = now()+10 分钟）
ALTER TABLE review_runs ADD COLUMN IF NOT EXISTS outcome       TEXT        NOT NULL DEFAULT '';
    -- ''=还没回访 | effective=预期出现 | ineffective=预期没出现 | voided=被新事实推翻

-- 20 秒粒度的观察窗扫描只查「到点且还没结论」的少量行。
CREATE INDEX IF NOT EXISTS idx_review_runs_observe_pending
    ON review_runs (task_id, observe_until) WHERE outcome = '';
