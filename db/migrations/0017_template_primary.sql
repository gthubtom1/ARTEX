-- 0017_template_primary.sql — 模板分"开局"与"补充打法"（docs/25 §六）。
-- 幂等：ADD COLUMN IF NOT EXISTS；只加一列；文件内不写 BEGIN/COMMIT。
--
-- 为什么：8 个模板并排 = 零基础用户挑不出来，而其中 5 个其实是"打到一半该说的话"，
-- 不是开局。用 is_primary 把 3 个开局放主视线，其余收进「更多」。
-- 具体哪 3 个、以及三段落文本的回填在 db/seed_templates.go（只在老文本未被改过时覆盖）。
ALTER TABLE task_templates ADD COLUMN IF NOT EXISTS is_primary BOOLEAN NOT NULL DEFAULT false;

CREATE INDEX IF NOT EXISTS idx_task_templates_primary
    ON task_templates (is_primary, updated_at DESC);
