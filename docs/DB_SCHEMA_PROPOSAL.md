# Phase 1 DB Schema Proposal

This is a review draft, not an applied migration. It keeps the app personal-only: no tenants, teams, users, agent sharing, billing, quotas, channels, or RBAC.

## Design Decisions

- PostgreSQL/ParadeDB is the only primary store.
- Config stays in YAML unless it is runtime state or audit history.
- IDs use `uuid` primary keys for public references.
- Searchable tables also include `search_id BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE` because ParadeDB BM25 requires a unique key field and it must be the first indexed column.
- DB full-text search uses ParadeDB BM25 with the `pdb.jieba` tokenizer for Chinese text fields. Do not use PostgreSQL `to_tsvector('simple')` as the primary FTS path.
- Embeddings use `vector(1024)` for the first migration. Changing embedding dimension later requires a migration.
- Every table uses `created_at` and usually `updated_at`; soft-delete is only used where user-visible recovery matters.
- `metadata JSONB` is allowed at edges, but core query fields are promoted to columns.

## Extensions

```sql
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_search;
```

## Core Chat

```sql
-- sessions: 聊天会话注册表，用于 Web 页面切换会话，也保存会话级 prompt 模式和滚动摘要。
CREATE TABLE sessions (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title              TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'archived')),
    prompt_mode        TEXT NOT NULL DEFAULT 'full'
        CHECK (prompt_mode IN ('full', 'task', 'minimal', 'none')),
    model_provider     TEXT NOT NULL DEFAULT '',
    model_name         TEXT NOT NULL DEFAULT '',
    summary            TEXT NOT NULL DEFAULT '',
    summary_token_count INT NOT NULL DEFAULT 0,
    metadata           JSONB NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archived_at        TIMESTAMPTZ
);

CREATE INDEX idx_sessions_updated ON sessions(updated_at DESC);
CREATE INDEX idx_sessions_status ON sessions(status, updated_at DESC);

-- runs: 单次助手请求运行记录，用于跟踪固定 8 阶段 pipeline、阶段耗时、token 用量和最终状态。
CREATE TABLE runs (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id          UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    input_message_id    UUID,
    output_message_id   UUID,
    current_stage       TEXT NOT NULL DEFAULT 'context',
    error_code          TEXT NOT NULL DEFAULT '',
    error_message       TEXT NOT NULL DEFAULT '',
    prompt_tokens       INT NOT NULL DEFAULT 0,
    completion_tokens   INT NOT NULL DEFAULT 0,
    total_tokens        INT NOT NULL DEFAULT 0,
    tool_call_count     INT NOT NULL DEFAULT 0,
    stage_timings       JSONB NOT NULL DEFAULT '{}',
    metadata            JSONB NOT NULL DEFAULT '{}',
    started_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at         TIMESTAMPTZ
);

CREATE INDEX idx_runs_session_started ON runs(session_id, started_at DESC);
CREATE INDEX idx_runs_status ON runs(status, started_at DESC);

-- messages: 追加式聊天记录，保存 user/assistant/tool 消息以及兼容 ADK 的结构化 parts。
CREATE TABLE messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    run_id          UUID REFERENCES runs(id) ON DELETE SET NULL,
    seq             BIGINT NOT NULL,
    role            TEXT NOT NULL
        CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    author          TEXT NOT NULL DEFAULT '',
    content         TEXT NOT NULL DEFAULT '',
    parts           JSONB NOT NULL DEFAULT '[]',
    tool_call_id    TEXT NOT NULL DEFAULT '',
    tool_name       TEXT NOT NULL DEFAULT '',
    token_count     INT NOT NULL DEFAULT 0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(session_id, seq)
);

CREATE INDEX idx_messages_session_seq ON messages(session_id, seq);
CREATE INDEX idx_messages_run ON messages(run_id);

-- run_events: 持久化运行事件流，用于 SSE 回放和调试 pipeline 阶段、工具调用、记忆动作与错误。
CREATE TABLE run_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id      UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    session_id  UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    stage       TEXT NOT NULL,
    event_type  TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_run_events_run_created ON run_events(run_id, created_at);
CREATE INDEX idx_run_events_session_created ON run_events(session_id, created_at DESC);
```

## Provider Usage

Provider config and keys stay in YAML/env. The DB only records runtime usage.

```sql
-- llm_usage: LLM provider/model 使用流水，用于成本、延迟、prompt cache、metrics 和审计诊断。
CREATE TABLE llm_usage (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            UUID REFERENCES runs(id) ON DELETE SET NULL,
    session_id        UUID REFERENCES sessions(id) ON DELETE SET NULL,
    provider          TEXT NOT NULL,
    model             TEXT NOT NULL,
    operation         TEXT NOT NULL
        CHECK (operation IN ('chat', 'embedding', 'summary', 'kg_extract', 'tts', 'quality_gate')),
    prompt_tokens     INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens      INT NOT NULL DEFAULT 0,
    cost_usd          NUMERIC(12, 6),
    latency_ms        INT NOT NULL DEFAULT 0,
    cache_hit         BOOLEAN NOT NULL DEFAULT false,
    cache_boundary    TEXT NOT NULL DEFAULT '',
    error_code        TEXT NOT NULL DEFAULT '',
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_llm_usage_created ON llm_usage(created_at DESC);
CREATE INDEX idx_llm_usage_run ON llm_usage(run_id);
CREATE INDEX idx_llm_usage_provider_model ON llm_usage(provider, model, created_at DESC);
```

## Memory: L0/L1/L2

`memory_documents` and `memory_chunks` are long-term raw memory and RAG chunks. `episodic_summaries` is L1 session memory. KG tables are L2 semantic memory.

```sql
-- memory_documents: 长期记忆源文档，保存手动笔记、导入文本、记忆文件等原始内容。
CREATE TABLE memory_documents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    source_kind  TEXT NOT NULL DEFAULT 'manual'
        CHECK (source_kind IN ('manual', 'conversation', 'vault', 'skill', 'import')),
    content      TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(path)
);

CREATE INDEX idx_memory_documents_updated ON memory_documents(updated_at DESC);
CREATE INDEX idx_memory_documents_hash ON memory_documents(content_hash);

-- memory_chunks: 记忆文档切片，提供基于 jieba 分词的 BM25 和向量索引，用于混合检索。
CREATE TABLE memory_chunks (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    search_id     BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    document_id   UUID NOT NULL REFERENCES memory_documents(id) ON DELETE CASCADE,
    chunk_index   INT NOT NULL,
    path          TEXT NOT NULL,
    text          TEXT NOT NULL,
    text_hash     TEXT NOT NULL DEFAULT '',
    token_count   INT NOT NULL DEFAULT 0,
    embedding     vector(1024),
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(document_id, chunk_index)
);

CREATE INDEX idx_memory_chunks_document ON memory_chunks(document_id, chunk_index);
CREATE INDEX idx_memory_chunks_embedding ON memory_chunks
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX idx_memory_chunks_bm25 ON memory_chunks
    USING bm25 (search_id, (path::pdb.jieba), (text::pdb.jieba), metadata)
    WITH (key_field = 'search_id');

-- episodic_summaries: L1 情景记忆，由完成的 run/session 生成，保存完整摘要、L0 摘要、embedding 和召回信号。
CREATE TABLE episodic_summaries (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    search_id         BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    session_id         UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_run_id      UUID REFERENCES runs(id) ON DELETE SET NULL,
    session_key        TEXT NOT NULL DEFAULT '',
    summary            TEXT NOT NULL,
    l0_abstract        TEXT NOT NULL DEFAULT '',
    key_topics         TEXT[] NOT NULL DEFAULT '{}',
    embedding          vector(1024),
    turn_count         INT NOT NULL DEFAULT 0,
    token_count        INT NOT NULL DEFAULT 0,
    recall_count       INT NOT NULL DEFAULT 0,
    recall_score       DOUBLE PRECISION NOT NULL DEFAULT 0,
    last_recalled_at   TIMESTAMPTZ,
    promoted_at        TIMESTAMPTZ,
    expires_at         TIMESTAMPTZ,
    metadata           JSONB NOT NULL DEFAULT '{}',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(source_run_id)
);

CREATE INDEX idx_episodic_session_created ON episodic_summaries(session_id, created_at DESC);
CREATE INDEX idx_episodic_expires ON episodic_summaries(expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX idx_episodic_recall_unpromoted ON episodic_summaries(recall_score DESC, created_at)
    WHERE promoted_at IS NULL;
CREATE INDEX idx_episodic_embedding ON episodic_summaries
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX idx_episodic_bm25 ON episodic_summaries
    USING bm25 (search_id, (session_key::pdb.jieba), (l0_abstract::pdb.jieba), (summary::pdb.jieba), metadata)
    WITH (key_field = 'search_id');
```

```sql
-- kg_entities: L2 语义记忆实体节点，由 LLM/worker 抽取，支持时间有效性、向量检索和 BM25 检索。
CREATE TABLE kg_entities (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    search_id       BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    name            TEXT NOT NULL,
    normalized_name TEXT NOT NULL,
    entity_type     TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    aliases         TEXT[] NOT NULL DEFAULT '{}',
    properties      JSONB NOT NULL DEFAULT '{}',
    source_run_id   UUID REFERENCES runs(id) ON DELETE SET NULL,
    source_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
    confidence      DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    embedding       vector(1024),
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_kg_entities_current_unique
    ON kg_entities(entity_type, normalized_name)
    WHERE valid_until IS NULL;
CREATE INDEX idx_kg_entities_type ON kg_entities(entity_type);
CREATE INDEX idx_kg_entities_current ON kg_entities(valid_until) WHERE valid_until IS NULL;
CREATE INDEX idx_kg_entities_embedding ON kg_entities
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX idx_kg_entities_bm25 ON kg_entities
    USING bm25 (search_id, (name::pdb.jieba), (normalized_name::pdb.jieba), entity_type, (description::pdb.jieba), properties)
    WITH (key_field = 'search_id');

-- kg_relations: 知识图谱实体之间的有类型有向边，保存证据、置信度和时间有效性。
CREATE TABLE kg_relations (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_entity_id  UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    target_entity_id  UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    relation_type     TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    evidence          JSONB NOT NULL DEFAULT '[]',
    properties        JSONB NOT NULL DEFAULT '{}',
    source_run_id     UUID REFERENCES runs(id) ON DELETE SET NULL,
    confidence        DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    valid_from        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(source_entity_id, relation_type, target_entity_id, valid_from)
);

CREATE INDEX idx_kg_relations_source ON kg_relations(source_entity_id, relation_type);
CREATE INDEX idx_kg_relations_target ON kg_relations(target_entity_id);
CREATE INDEX idx_kg_relations_current ON kg_relations(valid_until) WHERE valid_until IS NULL;

-- kg_dedup_candidates: KG 实体去重候选队列，保存相似度匹配发现的潜在重复实体。
CREATE TABLE kg_dedup_candidates (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_a_id  UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    entity_b_id  UUID NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    similarity   DOUBLE PRECISION NOT NULL,
    reason       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'merged', 'dismissed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at  TIMESTAMPTZ,
    UNIQUE(entity_a_id, entity_b_id)
);

CREATE INDEX idx_kg_dedup_status ON kg_dedup_candidates(status, similarity DESC);
```

## Knowledge Vault

The first version stores current document content in DB for simpler hybrid search and sync. A filesystem sync worker can still treat files as the source of truth.

```sql
-- vault_documents: Knowledge Vault 文档注册表和当前内容，用于 wikilink 笔记、摘要、语义链接和混合搜索。
CREATE TABLE vault_documents (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    search_id      BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    path           TEXT NOT NULL,
    title          TEXT NOT NULL DEFAULT '',
    doc_type       TEXT NOT NULL DEFAULT 'note',
    content        TEXT NOT NULL DEFAULT '',
    summary        TEXT NOT NULL DEFAULT '',
    content_hash   TEXT NOT NULL DEFAULT '',
    embedding      vector(1024),
    metadata       JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at     TIMESTAMPTZ,
    UNIQUE(path)
);

CREATE INDEX idx_vault_documents_type ON vault_documents(doc_type) WHERE deleted_at IS NULL;
CREATE INDEX idx_vault_documents_updated ON vault_documents(updated_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX idx_vault_documents_embedding ON vault_documents
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_vault_documents_bm25 ON vault_documents
    USING bm25 (search_id, (path::pdb.jieba), (title::pdb.jieba), doc_type, (content::pdb.jieba), (summary::pdb.jieba), metadata)
    WITH (key_field = 'search_id')
    WHERE deleted_at IS NULL;

-- vault_links: Vault 文档图谱边，来源包括 wikilink、语义自动链接、手动链接和引用链接。
CREATE TABLE vault_links (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_doc_id  UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    to_doc_id    UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    link_type    TEXT NOT NULL DEFAULT 'wikilink'
        CHECK (link_type IN ('wikilink', 'semantic', 'manual', 'citation')),
    context      TEXT NOT NULL DEFAULT '',
    confidence   DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(from_doc_id, to_doc_id, link_type)
);

CREATE INDEX idx_vault_links_from ON vault_links(from_doc_id);
CREATE INDEX idx_vault_links_to ON vault_links(to_doc_id);

-- vault_versions: Vault 文档轻量版本历史，为后续回滚和 diff 工作流预留。
CREATE TABLE vault_versions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    doc_id       UUID NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    version      INT NOT NULL,
    content      TEXT NOT NULL DEFAULT '',
    content_hash TEXT NOT NULL DEFAULT '',
    change_kind  TEXT NOT NULL DEFAULT 'edit',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(doc_id, version)
);
```

## Hooks And Quality Gate

```sql
-- hooks: 生命周期 hook 配置，覆盖 SessionStart、UserPromptSubmit、工具前后、Stop 和 subagent 事件。
CREATE TABLE hooks (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name           TEXT NOT NULL,
    event          TEXT NOT NULL
        CHECK (event IN (
            'SessionStart',
            'UserPromptSubmit',
            'PreToolUse',
            'PostToolUse',
            'Stop',
            'SubagentStart',
            'SubagentStop'
        )),
    handler_type   TEXT NOT NULL
        CHECK (handler_type IN ('builtin', 'http', 'command', 'script', 'prompt')),
    enabled        BOOLEAN NOT NULL DEFAULT true,
    async          BOOLEAN NOT NULL DEFAULT false,
    timeout_ms     INT NOT NULL DEFAULT 5000,
    matcher        JSONB NOT NULL DEFAULT '{}',
    config         JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(name)
);

CREATE INDEX idx_hooks_event_enabled ON hooks(event) WHERE enabled = true;

-- hook_audit_logs: hook 执行审计日志，记录决策、耗时、拦截动作、失败原因和 Quality Gate 结果。
CREATE TABLE hook_audit_logs (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_id        UUID REFERENCES hooks(id) ON DELETE SET NULL,
    run_id         UUID REFERENCES runs(id) ON DELETE SET NULL,
    session_id     UUID REFERENCES sessions(id) ON DELETE SET NULL,
    event          TEXT NOT NULL,
    handler_type   TEXT NOT NULL,
    status         TEXT NOT NULL
        CHECK (status IN ('started', 'allowed', 'blocked', 'failed', 'timeout', 'skipped')),
    request        JSONB NOT NULL DEFAULT '{}',
    response       JSONB NOT NULL DEFAULT '{}',
    error_message  TEXT NOT NULL DEFAULT '',
    latency_ms     INT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_hook_audit_run ON hook_audit_logs(run_id, created_at);
CREATE INDEX idx_hook_audit_event ON hook_audit_logs(event, created_at DESC);
```

Quality Gate uses `hooks.event = 'Stop'` plus `handler_type = 'prompt'` or `builtin`. Feedback-loop limits stay in YAML config.

## Tools, MCP, Skills

```sql
-- tool_registry: 本地内置工具目录，保存文件系统、搜索、浏览器、代码执行、记忆等工具的启用状态和配置。
CREATE TABLE tool_registry (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL DEFAULT '',
    category     TEXT NOT NULL DEFAULT '',
    enabled      BOOLEAN NOT NULL DEFAULT true,
    config       JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- mcp_servers: 可选的 MCP server 持久化配置，保存 stdio、SSE、HTTP transport 及连接策略。
CREATE TABLE mcp_servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    transport       TEXT NOT NULL
        CHECK (transport IN ('stdio', 'sse', 'http')),
    command         TEXT NOT NULL DEFAULT '',
    args            TEXT[] NOT NULL DEFAULT '{}',
    endpoint_url    TEXT NOT NULL DEFAULT '',
    env             JSONB NOT NULL DEFAULT '{}',
    tool_filter     TEXT[] NOT NULL DEFAULT '{}',
    require_confirmation BOOLEAN NOT NULL DEFAULT true,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_connected_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- mcp_tool_cache: MCP 工具发现结果缓存，避免启动或搜索时立即重连所有 MCP server。
CREATE TABLE mcp_tool_cache (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id     UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    tool_name     TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    input_schema  JSONB NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(server_id, tool_name)
);
```

```sql
-- skills: 基于 SKILL.md 的知识单元，保存元数据、可搜索内容、embedding、发布状态和 evolution draft 来源。
CREATE TABLE skills (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    search_id       BIGINT GENERATED ALWAYS AS IDENTITY UNIQUE,
    name            TEXT NOT NULL,
    slug            TEXT NOT NULL UNIQUE,
    description     TEXT NOT NULL DEFAULT '',
    version         INT NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('draft', 'active', 'archived')),
    visibility      TEXT NOT NULL DEFAULT 'private'
        CHECK (visibility IN ('private', 'published')),
    source_path     TEXT NOT NULL DEFAULT '',
    content         TEXT NOT NULL DEFAULT '',
    frontmatter     JSONB NOT NULL DEFAULT '{}',
    tags            TEXT[] NOT NULL DEFAULT '{}',
    embedding       vector(1024),
    evolution_origin JSONB NOT NULL DEFAULT '{}',
    published_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_skills_status ON skills(status, updated_at DESC);
CREATE INDEX idx_skills_embedding ON skills
    USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
CREATE INDEX idx_skills_bm25 ON skills
    USING bm25 (search_id, (name::pdb.jieba), (slug::pdb.jieba), (description::pdb.jieba), (content::pdb.jieba), frontmatter)
    WITH (key_field = 'search_id');

-- skill_resources: skill 附属资源文件，例如 references、scripts、templates 和 assets。
CREATE TABLE skill_resources (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id     UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    path         TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    content      BYTEA NOT NULL DEFAULT ''::bytea,
    content_hash TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(skill_id, path)
);
```

## Evolution

```sql
-- evolution_metrics: evolution 原始指标，供 guardrail 和 suggestion engine 使用，覆盖检索、工具、质量、反馈和记忆。
CREATE TABLE evolution_metrics (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id       UUID REFERENCES runs(id) ON DELETE SET NULL,
    session_id   UUID REFERENCES sessions(id) ON DELETE SET NULL,
    metric_type  TEXT NOT NULL
        CHECK (metric_type IN ('retrieval', 'tool', 'quality', 'feedback', 'memory')),
    metric_key   TEXT NOT NULL,
    value        JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evolution_metrics_type ON evolution_metrics(metric_type, metric_key, created_at DESC);

-- evolution_suggestions: 可审核的自我优化建议，目标包括 prompt、skill、工具策略、记忆策略和 Quality Gate。
CREATE TABLE evolution_suggestions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    suggestion_type TEXT NOT NULL
        CHECK (suggestion_type IN ('prompt', 'skill', 'tool_policy', 'memory_policy', 'quality_gate')),
    title           TEXT NOT NULL,
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL DEFAULT '',
    target_path     TEXT NOT NULL DEFAULT '',
    patch           TEXT NOT NULL DEFAULT '',
    parameters      JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'applied')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reviewed_at     TIMESTAMPTZ,
    applied_at      TIMESTAMPTZ
);

CREATE INDEX idx_evolution_suggestions_status ON evolution_suggestions(status, created_at DESC);
```

## Audio/TTS

The TTS LRU cache should be in-process plus filesystem cache. DB stores generated assets only when a session/message needs durable references.

```sql
-- audio_assets: 生成音频/TTS 文件的持久引用，用于音频产物需要关联到 session 或 message 的场景。
CREATE TABLE audio_assets (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id    UUID REFERENCES sessions(id) ON DELETE SET NULL,
    message_id    UUID REFERENCES messages(id) ON DELETE SET NULL,
    provider      TEXT NOT NULL,
    voice         TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    input_hash    TEXT NOT NULL,
    mime_type     TEXT NOT NULL DEFAULT 'audio/mpeg',
    byte_size     BIGINT NOT NULL DEFAULT 0,
    storage_path  TEXT NOT NULL,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audio_assets_message ON audio_assets(message_id);
CREATE INDEX idx_audio_assets_hash ON audio_assets(provider, model, voice, input_hash);
```

## Tables Intentionally Removed From GoClaw

- `tenants`, tenant grants, tenant backup/restore state.
- `users`, `agent_shares`, team/member/access-control tables.
- channel integrations: WhatsApp, Telegram, Slack, Discord, Feishu, Zalo, etc.
- API key auth, secure CLI grants, pairing, OAuth, quotas, billing, usage snapshots.
- scheduler/cron/heartbeat/team-task tables.
- multi-agent links and delegation tables. Subagent lifecycle hooks can still emit events without persistent multi-agent topology.

## Open Review Questions

1. Should Vault document content live in DB as proposed, or should DB only store path/hash/summary while files remain source of truth?
2. Should `memory_documents` and `vault_documents` be unified, or kept separate to avoid mixing assistant memory with explicit notes?
3. Is `vector(1024)` acceptable as the fixed first migration dimension, matching current BigModel/hash config?
4. Do you want `mcp_servers` persisted in DB, or should MCP remain config-only with only discovery cache persisted?
5. Should generated audio assets be persisted, or should TTS stay fully ephemeral cache-only?
