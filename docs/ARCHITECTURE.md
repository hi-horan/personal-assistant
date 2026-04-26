# Architecture

## Runtime

The service uses ADK-Go as the agent runtime:

- Cobra exposes `assistant run -c config.yaml` as the service command.
- `printconfig: true` prints the final effective YAML configuration at startup after defaults, normalization, and derived values are applied. The dump is not redacted.
- `runner.Run` owns each chat invocation.
- `session.Service` is implemented by `internal/store.PostgresSessionService`.
- `memory.Service` is implemented by `internal/store.PostgresMemoryService`.
- The root `llmagent` receives a user message, ADK-managed conversation history, RAG context, memory tools, and optional MCP tools.
- Model providers are selected by YAML: `echo` for local wiring, `gemini` through ADK-Go's Gemini adapter, and `glm` through the OpenAI-compatible GLM chat completions adapter.
- Embeddings are provided by `internal/embedding`, with local hash embeddings for development and Gemini or BigModel Embedding-3 embeddings for semantic production retrieval.

The first version has one root assistant agent. There are no sub-agents.

## Sessions

Sessions are durable PostgreSQL rows:

- `sessions`: session identity, user/app scope, title, session state, timestamps.
- `session_events`: append-only ADK events as JSON plus extracted text for inspection.
- `session_summaries`: rolling extractive summary for short-term compression.

Session and event IDs are stored as `bigint` microsecond timestamp IDs allocated by the backend. The ADK-facing session and event APIs still expose IDs as decimal strings. The allocator is isolated behind `store.IDAllocator` so it can later be moved behind a distributed ID service. `app_name` is stored as `varchar(256)` and `user_id` as `varchar(50)`.

`PostgresSessionService.AppendEvent` skips ADK partial stream events, persists final/non-partial ADK events, strips `temp:` state keys, applies durable state deltas, and updates the session timestamp.

## Short-Term Memory

Short-term memory is intentionally simple:

- ADK includes relevant conversation history from `session_events`.
- A rolling summary stores a compact recent-session digest after each chat turn.
- The summary is injected into the next prompt together with RAG results.

The summary is extractive in this first version. Replace `RefreshSummary` with an LLM summarizer when the rest of the system is stable.

## Long-Term Memory

Long-term memory is stored in:

- `memories`: memory record, kind, metadata, source event/session, importance.
- `memory_chunks`: searchable chunks with ParadeDB BM25 indexing and `pgvector` embedding.

Memory and memory chunk IDs use the same backend-allocated `bigint` microsecond timestamp ID format. `PostgresMemoryService.AddSessionToMemory` stores textual session events as episodic memories, deduplicated by ADK event ID. The HTTP API and `memory_save` tool can also create explicit semantic memories.

## RAG

RAG retrieval uses ParadeDB BM25 plus pgvector hybrid search:

1. Embed the query with the configured embedding provider.
2. Search user/app scoped memories using ParadeDB BM25 over `memory_chunks.content`.
3. Search nearby memory chunks using `embedding <=> query_embedding`.
4. Merge BM25 and vector candidates with reciprocal rank fusion.
5. Rank by RRF score, importance, vector score, BM25 score, and recency.
6. Format the top results into `rag_context`.
7. Pass `rag_context` with `runner.WithStateDelta`.

The default `hash` embedding provider is deterministic and local, intended for development and pipeline testing. Use `embedding_provider: gemini` or `embedding_provider: bigmodel` for semantic vector search. The current pgvector schema is fixed at 1024 dimensions, so BigModel requests include `dimensions: 1024`. Embeddings are stored and queried without Go-side normalization; pgvector cosine distance (`<=>`) handles vector length during similarity calculation.

## MCP

MCP is optional and configured in YAML under `mcp.servers`. Each configured stdio server becomes an ADK `mcptoolset` and is attached to the root LLM agent. ADK creates the MCP session lazily on the first model request that needs tools.

## Observability

- `slog` is the only logger.
- Logs include a relative `source` value in `path/to/file.go:line` form; absolute source paths are not emitted.
- Request-scoped `slog` records include `trace_id` and `span_id` from the active OpenTelemetry span.
- Logs record startup, migration, chat start/end, session writes, memory writes/searches, RAG retrieval, MCP setup, and shutdown.
- OpenTelemetry traces instrument HTTP requests and internal chat/RAG/memory/session spans.
- OpenTelemetry metrics instrument HTTP requests through `otelhttp` and custom chat, RAG, memory, session, and HTTP error counters/histograms.
- Docker Compose includes OTel Collector, Prometheus, Tempo, and Grafana. The assistant exports OTLP HTTP to the collector; Prometheus scrapes collector metrics; the collector sends traces to Tempo; Grafana is provisioned with Prometheus and Tempo datasources.

## Error Handling

Application errors use typed codes in `internal/apperr`. HTTP handlers map those codes to stable JSON error responses. Lower-level errors are wrapped with context before crossing package boundaries.
