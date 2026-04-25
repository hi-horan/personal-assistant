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
- Embeddings are provided by `internal/embedding`, with local hash embeddings for development and Gemini embeddings for semantic production retrieval.

The first version has one root assistant agent. There are no sub-agents.

## Sessions

Sessions are durable PostgreSQL rows:

- `sessions`: session identity, user/app scope, title, session state, timestamps.
- `session_events`: append-only ADK events as JSON plus extracted text for inspection.
- `session_summaries`: rolling extractive summary for short-term compression.

`PostgresSessionService.AppendEvent` persists every ADK event, strips `temp:` state keys, applies durable state deltas, and updates the session timestamp.

## Short-Term Memory

Short-term memory is intentionally simple:

- ADK includes relevant conversation history from `session_events`.
- A rolling summary stores a compact recent-session digest after each chat turn.
- The summary is injected into the next prompt together with RAG results.

The summary is extractive in this first version. Replace `RefreshSummary` with an LLM summarizer when the rest of the system is stable.

## Long-Term Memory

Long-term memory is stored in:

- `memories`: memory record, kind, metadata, source event/session, importance.
- `memory_chunks`: searchable chunks with generated PostgreSQL `tsvector` and `pgvector` embedding.

`PostgresMemoryService.AddSessionToMemory` stores textual session events as episodic memories, deduplicated by ADK event ID. The HTTP API and `memory_save` tool can also create explicit semantic memories.

## RAG

RAG retrieval uses PostgreSQL hybrid search:

1. Embed the query with the configured embedding provider.
2. Search user/app scoped memories using `websearch_to_tsquery`.
3. Search nearby memory chunks using `embedding <=> query_embedding`.
4. Merge FTS and vector candidates.
5. Rank by FTS score, vector score, importance, and recency.
6. Format the top results into `rag_context`.
7. Pass `rag_context` with `runner.WithStateDelta`.

The default `hash` embedding provider is deterministic and local, intended for development and pipeline testing. Use `embedding_provider: gemini` for semantic vector search.

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
