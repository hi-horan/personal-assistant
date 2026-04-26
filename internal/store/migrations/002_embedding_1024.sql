DO $$
BEGIN
  IF EXISTS (
    SELECT 1
    FROM pg_attribute a
    JOIN pg_class c ON c.oid = a.attrelid
    JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
      AND c.relname = 'memory_chunks'
      AND a.attname = 'embedding'
      AND NOT a.attisdropped
      AND format_type(a.atttypid, a.atttypmod) <> 'vector(1024)'
  ) THEN
    DROP INDEX IF EXISTS memory_chunks_embedding_hnsw_idx;
    TRUNCATE TABLE memory_chunks, memories;
    ALTER TABLE memory_chunks
      ALTER COLUMN embedding TYPE vector(1024);
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS memory_chunks_embedding_hnsw_idx
  ON memory_chunks USING hnsw (embedding vector_cosine_ops);
