-- 0018_task_prefs.sql — 任务级偏好：用户选的业务目标 + token 上限（docs/PLANS 找洞/产品审查遗留项 P1-06、P1-02）。
-- 幂等：ADD COLUMN IF NOT EXISTS；只加列，不改已有数据语义；文件内不写 BEGIN/COMMIT。
--
-- 为什么：
--   ① goal_selected —— 用户在战况卡上选的「我的目标」原来只存在浏览器内存，刷新即丢；
--   ② token_budget   —— 用户明确要求"不要烧我太多 token"，但只有时间闸门，没有 token 闸门。
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS goal_selected TEXT NOT NULL DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS token_budget BIGINT NOT NULL DEFAULT 0;
