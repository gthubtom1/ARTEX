-- 0001_p0_schema.sql — P0 地基（幂等；由 db.applyMigrations 在启动锁内执行，文件内不写事务）
-- 执行方式：应用启动时自动执行（ledger: schema_migrations.version = 文件名）
--         也可手工：psql "$ARTEX_PG_DSN" -v ON_ERROR_STOP=1 -f db/migrations/0001_p0_schema.sql
--
-- 设计要点（依据 db/schema.sql 只读审计）：
--   1) 既有表一律复用：assets / tasks / findings / exploration_nodes / settings /
--      finding_retests / finding_cases / task_intelligence —— 不重建。
--   2) 交付物表（证据/能力卡/假设）的任务外键用 SET NULL，避免删任务时静默丢证据。
--   3) 审计表不设外键（与 notification_events.finding_id 的既有政策一致），只存 task_id。
--   4) 表名 local_tools_cache 而非 tools_cache，避免与 mcp_tools_cache / tools 混淆。
--   5) 每张表都带业务幂等键（唯一索引），重跑不产生重复行。

-- ─────────────────────────── 1. assets 扩列（CMDB 式属性） ───────────────────────────
ALTER TABLE assets ADD COLUMN IF NOT EXISTS owner       TEXT        NOT NULL DEFAULT '';
ALTER TABLE assets ADD COLUMN IF NOT EXISTS env         TEXT        NOT NULL DEFAULT 'unknown';
ALTER TABLE assets ADD COLUMN IF NOT EXISTS criticality INT         NOT NULL DEFAULT 3;
ALTER TABLE assets ADD COLUMN IF NOT EXISTS tags        TEXT[]      NOT NULL DEFAULT '{}';
ALTER TABLE assets ADD COLUMN IF NOT EXISTS lifecycle   TEXT        NOT NULL DEFAULT 'discovered';
ALTER TABLE assets ADD COLUMN IF NOT EXISTS first_seen  TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE assets ADD COLUMN IF NOT EXISTS last_change TIMESTAMPTZ;
-- last_seen 在 schema.sql:80 已存在且类型一致，此处不再 ADD，避免重复定义。

-- 存量行回填：first_seen 用真实创建时间，而不是迁移时刻
UPDATE assets SET first_seen = created_at WHERE first_seen > created_at;

CREATE INDEX IF NOT EXISTS idx_assets_lifecycle   ON assets (lifecycle);
CREATE INDEX IF NOT EXISTS idx_assets_criticality ON assets (criticality DESC);
CREATE INDEX IF NOT EXISTS idx_assets_tags_gin    ON assets USING GIN (tags);

-- ─────────────────────────── 2. assets_owned（长期持有台账，全局） ───────────────────────────
CREATE TABLE IF NOT EXISTS assets_owned (
    id               BIGSERIAL PRIMARY KEY,
    asset_key        TEXT        NOT NULL UNIQUE,          -- 全局稳定键：host 优先，其次 ip:port
    asset_id         BIGINT      REFERENCES assets(id) ON DELETE SET NULL,
    host             TEXT        NOT NULL DEFAULT '',
    ip               TEXT        NOT NULL DEFAULT '',
    port             INTEGER,
    access_type      TEXT        NOT NULL DEFAULT 'shell', -- shell|webshell|ssh|vpn|jump|admin|db
    privilege        TEXT        NOT NULL DEFAULT 'user',  -- user|root|admin|system
    depth            INTEGER     NOT NULL DEFAULT 0,
    via_asset        BIGINT      REFERENCES assets_owned(id) ON DELETE SET NULL,
    status           TEXT        NOT NULL DEFAULT 'owned', -- owned|verified|stale|lost|cleaned
    first_task_id    BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    first_owned_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_verified_at TIMESTAMPTZ,
    next_check_at    TIMESTAMPTZ,
    evidence         TEXT        NOT NULL DEFAULT '',
    notes            TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_owned_status ON assets_owned (status);
CREATE INDEX IF NOT EXISTS idx_owned_next   ON assets_owned (next_check_at) WHERE status IN ('owned','verified','stale');
CREATE INDEX IF NOT EXISTS idx_owned_via    ON assets_owned (via_asset);

-- ─────────────────────────── 3. creds_vault（凭据：只存密文 + 指纹） ───────────────────────────
-- 与 task_intelligence(kind='credential') 的分工：
--   task_intelligence 记"任务内情报叙述"（不含明文秘密）；
--   creds_vault 记"可跨任务复用的秘密本体"（AES-GCM 密文），报告/日志/知识库只出现引用 id。
CREATE TABLE IF NOT EXISTS creds_vault (
    id           BIGSERIAL PRIMARY KEY,
    task_id      BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    asset_id     BIGINT      REFERENCES assets(id) ON DELETE SET NULL,
    kind         TEXT        NOT NULL DEFAULT 'password',   -- password|hash|token|key|ssh_key|conn_str
    username     TEXT        NOT NULL DEFAULT '',
    secret_enc   BYTEA       NOT NULL,
    secret_nonce BYTEA       NOT NULL,
    fingerprint  TEXT        NOT NULL DEFAULT '',            -- 去重指纹（HMAC，不是明文）
    verified     BOOLEAN     NOT NULL DEFAULT false,
    status       TEXT        NOT NULL DEFAULT 'discovered',  -- discovered|verified|in_use|rotated|invalid|archived
    used_on      TEXT[]      NOT NULL DEFAULT '{}',
    use_count    INTEGER     NOT NULL DEFAULT 0,
    source       TEXT        NOT NULL DEFAULT '',            -- config|history|browser|dpapi|cloud|manual
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at   TIMESTAMPTZ,
    notes        TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_creds_status ON creds_vault (status);
CREATE INDEX IF NOT EXISTS idx_creds_task   ON creds_vault (task_id);
CREATE INDEX IF NOT EXISTS idx_creds_asset  ON creds_vault (asset_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_creds_fingerprint ON creds_vault (fingerprint) WHERE fingerprint <> '';

-- ─────────────────────────── 4. access_refs（到 owned 主机的连接方式） ───────────────────────────
CREATE TABLE IF NOT EXISTS access_refs (
    id         BIGSERIAL PRIMARY KEY,
    owned_id   BIGINT      NOT NULL REFERENCES assets_owned(id) ON DELETE CASCADE,
    proto      TEXT        NOT NULL,                       -- shell|webshell|ssh|vpn|jump|admin|db
    conn       JSONB       NOT NULL DEFAULT '{}',          -- 连接参数（不含明文口令）
    cred_ref   BIGINT      REFERENCES creds_vault(id) ON DELETE SET NULL,
    status     TEXT        NOT NULL DEFAULT 'up',          -- up|down|unknown
    last_ok_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_access_owned ON access_refs (owned_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_access_refs_key
    ON access_refs (owned_id, proto, md5(conn::text));

-- ─────────────────────────── 5. routes（可达路由） ───────────────────────────
CREATE TABLE IF NOT EXISTS routes (
    id             BIGSERIAL PRIMARY KEY,
    task_id        BIGINT      REFERENCES tasks(id) ON DELETE CASCADE,
    cidr           CIDR        NOT NULL,
    via_owned      BIGINT      REFERENCES assets_owned(id) ON DELETE SET NULL,
    proxy_endpoint TEXT        NOT NULL DEFAULT '',        -- socks5://127.0.0.1:1080
    hops           INTEGER     NOT NULL DEFAULT 1,
    status         TEXT        NOT NULL DEFAULT 'up',       -- up|down
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_routes_task ON routes (task_id);
CREATE INDEX IF NOT EXISTS idx_routes_cidr ON routes USING GIST (cidr inet_ops);
CREATE UNIQUE INDEX IF NOT EXISTS uq_routes_task_cidr
    ON routes (COALESCE(task_id,0), cidr, COALESCE(via_owned,0));

-- ─────────────────────────── 6. asset_checks / change_events（复查与变更） ───────────────────────────
CREATE TABLE IF NOT EXISTS asset_checks (
    id         BIGSERIAL PRIMARY KEY,
    owned_id   BIGINT      REFERENCES assets_owned(id) ON DELETE CASCADE,
    asset_id   BIGINT      REFERENCES assets(id) ON DELETE SET NULL,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind       TEXT        NOT NULL,                        -- http|ssh|port|login|webshell
    ok         BOOLEAN     NOT NULL DEFAULT false,
    latency_ms INTEGER,
    changed    BOOLEAN     NOT NULL DEFAULT false,
    check_key  TEXT        NOT NULL DEFAULT '',             -- 业务幂等键：同一轮复查只记一次
    detail     TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_checks_owned ON asset_checks (owned_id, ts DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_asset_checks_key
    ON asset_checks (owned_id, kind, check_key) WHERE check_key <> '';

CREATE TABLE IF NOT EXISTS change_events (
    id       BIGSERIAL PRIMARY KEY,
    task_id  BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    asset_id BIGINT      REFERENCES assets(id) ON DELETE SET NULL,
    ts       TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind     TEXT        NOT NULL,                          -- port|subdomain|tech|cert|banner
    event_key TEXT       NOT NULL DEFAULT '',               -- 业务幂等键
    before   JSONB       NOT NULL DEFAULT '{}',
    after    JSONB       NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_change_asset ON change_events (asset_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_change_task  ON change_events (task_id, ts DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_change_events_key
    ON change_events (asset_id, kind, event_key) WHERE event_key <> '';

-- ─────────────────────────── 7. capabilities（能力卡，派生自 finding/fact） ───────────────────────────
-- grp/item 对应 kb/capabilities/taxonomy.md 的 13 组 82 项。
CREATE TABLE IF NOT EXISTS capabilities (
    id               BIGSERIAL PRIMARY KEY,
    task_id          BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    grp              INTEGER     NOT NULL,                   -- 1..13
    item             TEXT        NOT NULL,                   -- 组内编号，如 '1.2'
    can              TEXT        NOT NULL DEFAULT '',
    surface          TEXT        NOT NULL DEFAULT '',
    evidence         TEXT        NOT NULL DEFAULT '',
    proof_level      TEXT        NOT NULL DEFAULT 'P2',      -- P1..P4 / D1..D4
    confidence       TEXT        NOT NULL DEFAULT 'observed',-- observed|inferred
    impact           TEXT        NOT NULL DEFAULT '',
    risk             TEXT        NOT NULL DEFAULT '',
    next_if_approved TEXT        NOT NULL DEFAULT '',
    source_node_ids  BIGINT[]    NOT NULL DEFAULT '{}',
    status           TEXT        NOT NULL DEFAULT 'open',    -- open|approved|declined|done
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_caps_task   ON capabilities (task_id, grp);
CREATE INDEX IF NOT EXISTS idx_caps_status ON capabilities (status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_capabilities_item ON capabilities (task_id, grp, item);

-- ─────────────────────────── 8. assumptions（假设账本） ───────────────────────────
CREATE TABLE IF NOT EXISTS assumptions (
    id             BIGSERIAL PRIMARY KEY,
    task_id        BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    text           TEXT        NOT NULL,
    status         TEXT        NOT NULL DEFAULT 'unverified', -- unverified|verified|refuted
    evidence_node  BIGINT      REFERENCES exploration_nodes(id) ON DELETE SET NULL,
    created_round  INTEGER     NOT NULL DEFAULT 0,
    resolved_round INTEGER,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_assump_task ON assumptions (task_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS uq_assumptions_text ON assumptions (task_id, md5(text));

-- ─────────────────────────── 9. evidence_timeline（证据时间线） ───────────────────────────
CREATE TABLE IF NOT EXISTS evidence_timeline (
    id          BIGSERIAL PRIMARY KEY,
    task_id     BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    asset_id    BIGINT      REFERENCES assets(id) ON DELETE SET NULL,
    finding_id  BIGINT      REFERENCES findings(id) ON DELETE SET NULL,
    node_id     BIGINT      REFERENCES exploration_nodes(id) ON DELETE SET NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    kind        TEXT        NOT NULL,                        -- request|command|file|dns|screenshot
    content_ref TEXT        NOT NULL DEFAULT '',             -- traffic id / cmd-output 路径 / blob 引用
    sha256      TEXT        NOT NULL DEFAULT '',
    actor       TEXT        NOT NULL DEFAULT '',             -- planner|worker:<id>|adviser|human
    meta        JSONB       NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_ev_task    ON evidence_timeline (task_id, ts);
CREATE INDEX IF NOT EXISTS idx_ev_finding ON evidence_timeline (finding_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_evidence_ref
    ON evidence_timeline (COALESCE(task_id,0), kind, content_ref) WHERE content_ref <> '';

-- ─────────────────────────── 10. audit_log（全量审计；无外键，只追加） ───────────────────────────
CREATE TABLE IF NOT EXISTS audit_log (
    id          BIGSERIAL PRIMARY KEY,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    task_id     BIGINT      NOT NULL DEFAULT 0,              -- 刻意不设 FK：审计必须长期留存
    actor       TEXT        NOT NULL DEFAULT '',
    tool        TEXT        NOT NULL DEFAULT '',
    target      TEXT        NOT NULL DEFAULT '',
    action      TEXT        NOT NULL DEFAULT '',
    result      TEXT        NOT NULL DEFAULT '',
    args_digest TEXT        NOT NULL DEFAULT '',
    err         TEXT        NOT NULL DEFAULT '',
    meta        JSONB       NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_audit_task ON audit_log (task_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit_log (tool, ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_ts   ON audit_log (ts DESC);

-- ─────────────────────────── 11. idem_keys（幂等键） ───────────────────────────
CREATE TABLE IF NOT EXISTS idem_keys (
    key        TEXT        PRIMARY KEY,
    task_id    BIGINT      REFERENCES tasks(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_idem_task ON idem_keys (task_id);
CREATE INDEX IF NOT EXISTS idx_idem_created ON idem_keys (created_at);

-- ─────────────────────────── 12. 本地工具面（发现/缓存/事件） ───────────────────────────
-- 注意：表名 local_tools_cache，避免与 mcp_tools_cache(865) / tools(936) 混淆。
CREATE TABLE IF NOT EXISTS local_tools_cache (
    id          BIGSERIAL PRIMARY KEY,
    name        TEXT        NOT NULL,
    path        TEXT        NOT NULL,
    kind        TEXT        NOT NULL DEFAULT 'binary',      -- binary|python|go|docker|snap|script
    version     TEXT        NOT NULL DEFAULT '',
    sha256      TEXT        NOT NULL DEFAULT '',
    caps        TEXT[]      NOT NULL DEFAULT '{}',
    healthy     BOOLEAN     NOT NULL DEFAULT false,
    scan_source TEXT        NOT NULL DEFAULT '',             -- path|dpkg|pipx|go|full|desktop|docker
    checked_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, path)
);
CREATE INDEX IF NOT EXISTS idx_local_tools_name ON local_tools_cache (name);

CREATE TABLE IF NOT EXISTS tool_events (
    id          BIGSERIAL PRIMARY KEY,
    ts          TIMESTAMPTZ NOT NULL DEFAULT now(),
    tool        TEXT        NOT NULL,
    action      TEXT        NOT NULL,                        -- discover|install|fallback|fail|health
    result      TEXT        NOT NULL DEFAULT '',
    duration_ms INTEGER,
    detail      TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_tool_events_tool ON tool_events (tool, ts DESC);

-- ─────────────────────────── 13. 出口审计（netguard） ───────────────────────────
CREATE TABLE IF NOT EXISTS egress_log (
    id         BIGSERIAL PRIMARY KEY,
    ts         TIMESTAMPTZ NOT NULL DEFAULT now(),
    task_id    BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    egress_ip  TEXT        NOT NULL DEFAULT '',
    node       TEXT        NOT NULL DEFAULT '',
    ok         BOOLEAN     NOT NULL DEFAULT false,
    detail     TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_egress_task ON egress_log (task_id, ts DESC);

CREATE TABLE IF NOT EXISTS net_events (
    id      BIGSERIAL PRIMARY KEY,
    ts      TIMESTAMPTZ NOT NULL DEFAULT now(),
    task_id BIGINT      REFERENCES tasks(id) ON DELETE SET NULL,
    kind    TEXT        NOT NULL,                            -- verify|switch|fail|recover
    detail  TEXT        NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_net_events_task ON net_events (task_id, ts DESC);
