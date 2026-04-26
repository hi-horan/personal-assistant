CREATE EXTENSION IF NOT EXISTS pg_search;
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS sessions (
  id bigint PRIMARY KEY,
  app_name varchar(256) NOT NULL,
  user_id varchar(50) NOT NULL,
  title text NOT NULL DEFAULT '',
  state jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sessions_app_user_updated_idx
  ON sessions (app_name, user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS session_events (
  id bigint PRIMARY KEY,
  session_id bigint NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  app_name varchar(256) NOT NULL,
  user_id varchar(50) NOT NULL,
  invocation_id text NOT NULL DEFAULT '',
  author text NOT NULL DEFAULT '',
  branch text NOT NULL DEFAULT '',
  content_text text NOT NULL DEFAULT '',
  event_json jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS session_events_session_id_idx
  ON session_events (session_id, id);

CREATE INDEX IF NOT EXISTS session_events_session_created_idx
  ON session_events (session_id, created_at DESC);

CREATE TABLE IF NOT EXISTS session_summaries (
  session_id bigint PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
  app_name varchar(256) NOT NULL,
  user_id varchar(50) NOT NULL,
  summary text NOT NULL,
  covered_until_event_id bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS memories (
  id bigint PRIMARY KEY,
  app_name varchar(256) NOT NULL,
  user_id varchar(50) NOT NULL,
  kind text NOT NULL DEFAULT 'episodic',
  content text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  importance double precision NOT NULL DEFAULT 0.5,
  source_session_id bigint REFERENCES sessions(id) ON DELETE SET NULL,
  source_event_id bigint,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS memories_source_event_id_idx
  ON memories (source_event_id)
  WHERE source_event_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS memories_app_user_updated_idx
  ON memories (app_name, user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS memory_chunks (
  id bigint PRIMARY KEY,
  memory_id bigint NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  app_name varchar(256) NOT NULL,
  user_id varchar(50) NOT NULL,
  content text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  embedding vector(1024),
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS memory_chunks_app_user_idx
  ON memory_chunks (app_name, user_id);

CREATE INDEX IF NOT EXISTS memory_chunks_embedding_hnsw_idx
  ON memory_chunks USING hnsw (embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS memory_chunks_bm25_idx
  ON memory_chunks
  USING bm25 (id, app_name, user_id, (content::pdb.jieba))
  WITH (
    key_field = 'id',
    text_fields = '{
      "id": {"tokenizer": {"type": "keyword"}},
      "app_name": {"tokenizer": {"type": "keyword"}},
      "user_id": {"tokenizer": {"type": "keyword"}}
    }'
  );
