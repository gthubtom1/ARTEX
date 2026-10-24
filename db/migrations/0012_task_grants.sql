-- 0012_task_grants.sql — 即时授权（just-in-time authorization）
-- 幂等：IF NOT EXISTS；只新增表；文件内不写 BEGIN/COMMIT。
--
-- 为什么：零基础用户不知道"拿到某个漏洞能干什么"，所以**不能预先把边界写死**
-- （写死了以后要改就得改代码）。正确做法是：系统算出现在能升级到哪一级、
-- 需要什么动作，把"能做啥/风险/我会执行什么"摆给用户，用户当场决定。
--
-- 语义：
--   · 授权粒度 = (任务, 动作类别)，一个任务里允许不等于其它任务允许；
--   · granted=false 记录"用户明确拒绝过"，避免每次重复弹同一条；
--   · 不设过期：任务生命周期内有效，可在同一张卡上撤销。
CREATE TABLE IF NOT EXISTS task_grants (
    task_id      BIGINT      NOT NULL,
    action_class TEXT        NOT NULL,          -- file_write|data_dump|persist|lateral
    granted      BOOLEAN     NOT NULL DEFAULT false,
    note         TEXT        NOT NULL DEFAULT '',
    decided_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (task_id, action_class)
);
