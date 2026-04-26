CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS sessions (
  id text PRIMARY KEY,
  app_name text NOT NULL,
  user_id text NOT NULL,
  title text NOT NULL DEFAULT '',
  state jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS sessions_app_user_updated_idx
  ON sessions (app_name, user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS session_events (
  seq bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  id text NOT NULL UNIQUE,
  session_id text NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  app_name text NOT NULL,
  user_id text NOT NULL,
  invocation_id text NOT NULL DEFAULT '',
  author text NOT NULL DEFAULT '',
  branch text NOT NULL DEFAULT '',
  content_text text NOT NULL DEFAULT '',
  event_json jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS session_events_session_seq_idx
  ON session_events (session_id, seq);

CREATE INDEX IF NOT EXISTS session_events_session_created_idx
  ON session_events (session_id, created_at DESC);

CREATE TABLE IF NOT EXISTS session_summaries (
  session_id text PRIMARY KEY REFERENCES sessions(id) ON DELETE CASCADE,
  app_name text NOT NULL,
  user_id text NOT NULL,
  summary text NOT NULL,
  covered_until_seq bigint NOT NULL DEFAULT 0,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS memories (
  id text PRIMARY KEY,
  app_name text NOT NULL,
  user_id text NOT NULL,
  kind text NOT NULL DEFAULT 'episodic',
  content text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  importance double precision NOT NULL DEFAULT 0.5,
  source_session_id text REFERENCES sessions(id) ON DELETE SET NULL,
  source_event_id text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS memories_source_event_id_idx
  ON memories (source_event_id)
  WHERE source_event_id IS NOT NULL AND source_event_id <> '';

CREATE INDEX IF NOT EXISTS memories_app_user_updated_idx
  ON memories (app_name, user_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS memory_chunks (
  id text PRIMARY KEY,
  memory_id text NOT NULL REFERENCES memories(id) ON DELETE CASCADE,
  app_name text NOT NULL,
  user_id text NOT NULL,
  content text NOT NULL,
  metadata jsonb NOT NULL DEFAULT '{}'::jsonb,
  embedding vector(1024),
  tsv tsvector GENERATED ALWAYS AS (to_tsvector('simple', content)) STORED,
  created_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE memory_chunks
  ADD COLUMN IF NOT EXISTS embedding vector(1024);

CREATE INDEX IF NOT EXISTS memory_chunks_tsv_idx
  ON memory_chunks USING gin (tsv);

CREATE INDEX IF NOT EXISTS memory_chunks_app_user_idx
  ON memory_chunks (app_name, user_id);

CREATE INDEX IF NOT EXISTS memory_chunks_embedding_hnsw_idx
  ON memory_chunks USING hnsw (embedding vector_cosine_ops);
