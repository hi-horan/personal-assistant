# AGENTS.md

Project rules for coding agents working in this repository.

## Scope

- This repo is a small personal assistant service.
- Keep the scope limited to multi-session chat, short-term memory, long-term memory, RAG, MCP, HTTP API, logging, telemetry, and persistence.
- Do not add UI, auth, billing, schedulers, background job systems, multi-agent orchestration, or unrelated assistant features unless explicitly requested.

## Stack

- Go 1.25+.
- ADK-Go for agent runtime.
- PostgreSQL for durable state.
- pgvector for memory embeddings and vector search.
- Model providers: `echo`, `gemini`, and `glm`.
- Cobra for CLI commands.
- YAML for configuration.
- `log/slog` for logging.
- OpenTelemetry for tracing and metrics.

## Commands

- Show make targets: `make help`
- Create local config: `make config`
- Start service locally: `make run`
- Build binary: `make build`
- Run tests: `make test`
- Tidy dependencies: `make tidy`
- Format code: `make fmt`
- Full local check: `make check`
- Build Docker image: `make docker-build`
- Run Docker Compose stack: `make docker-up`
- Stop Docker Compose stack: `make docker-down`
- Direct command shape: `go run ./cmd/assistant run -c config.yaml`
- Binary command shape after `make build`: `./bin/assistant run -c config.yaml`

## Configuration

- Example config lives in `config.example.yaml`.
- Docker Compose config lives in `config.compose.yaml` and must not contain real secrets.
- Docker Compose observability config lives in `deploy/`.
- Docker Compose images should use explicit version tags, not `latest`, `main`, or floating major tags.
- Local config should be named `config.yaml`; it is ignored by git.
- Config is loaded only from YAML via `assistant run -c <path>`.
- `printconfig` defaults to `false`. When enabled, it prints the full effective config without redaction.
- Keep config validation in `internal/config`.
- GLM uses the OpenAI-compatible chat completions API with `model_provider: glm`.
- Use `yaml.Decoder.KnownFields(true)` or equivalent strict decoding for new config fields.
- Do not reintroduce `.env` as the primary config path.

## Architecture

- `cmd/assistant`: Cobra entrypoint and command wiring.
- `internal/app`: application orchestration, ADK runner setup, RAG injection, memory tools, MCP toolsets.
- `internal/config`: YAML config loading and validation.
- `internal/httpapi`: JSON HTTP API and error mapping.
- `internal/modelx`: model provider adapters.
- `internal/observability`: slog and OpenTelemetry setup.
- `internal/rag`: retrieval and prompt-context formatting.
- `internal/store`: PostgreSQL session, memory, summary, migration, and ADK service implementations.
- `internal/store/migrations`: embedded SQL migrations.
- `docs/ARCHITECTURE.md`: design notes. Update it when behavior or storage shape changes.

## Session And Memory Rules

- `internal/store.Store` implements both ADK `session.Service` and `memory.Service`.
- Session events are append-only in `session_events`.
- Persistent session state lives in `sessions.state`.
- Never persist ADK temporary state keys with prefix `temp:`.
- Long-term memory lives in `memories` and `memory_chunks`.
- RAG is hybrid PostgreSQL search: full-text search plus pgvector.
- Keep `embedding_dimension` aligned with the `memory_chunks.embedding vector(768)` schema unless a migration changes both.
- `embedding_provider: hash` is local and deterministic. `embedding_provider: gemini` is the semantic production path.
- Keep user/app scoping on every session and memory query.

## MCP Rules

- MCP servers are configured under `mcp.servers` in YAML.
- Current implementation supports stdio MCP servers through ADK `mcptoolset`.
- Keep MCP tool confirmation configurable per server.
- Do not execute MCP tools directly from HTTP handlers; expose them through the ADK agent/toolset path.

## Logging And Telemetry

- Use `slog`; do not introduce another logging framework.
- Logs should include a relative `source` value in `path/to/file.go:line` form. Do not emit absolute source paths.
- Request-scoped logs should use `InfoContext`, `ErrorContext`, or equivalent so `trace_id` and `span_id` are recorded.
- Log startup, config path, migration, HTTP server lifecycle, chat start/end, memory writes/searches, session changes, MCP setup, and shutdown.
- Avoid logging secrets, API keys, raw credentials, or full config payloads.
- Use OpenTelemetry spans and metrics around meaningful service, store, RAG, memory, and HTTP work.
- Compose routes OTLP HTTP to `otel-collector`, Prometheus scrapes collector metrics, Tempo stores traces, and Grafana uses Prometheus plus Tempo datasources.
- Keep error wrapping specific enough to diagnose the failing operation.

## Error Handling

- Use typed application errors from `internal/apperr` at API/service boundaries.
- HTTP handlers should return stable JSON errors.
- Wrap lower-level errors with operation context before returning.
- Do not panic for normal runtime errors.

## Code Style

- Keep code idiomatic Go.
- Prefer small packages with explicit dependencies.
- Keep business logic out of HTTP handlers.
- Keep SQL in store methods or embedded migrations.
- Use context-aware calls for I/O.
- Do not use global mutable state for app dependencies.
- Add comments only where the logic is not obvious.

## Tests And Validation

- Run `make fmt` after Go edits.
- Run `make tidy` after dependency or import changes.
- Run `make test` before handoff.
- Prefer `make check` when a change touches Go code or dependencies.
- Add focused tests when changing parsing, error mapping, storage behavior, RAG ranking, or command behavior.

## Docs

- Update `README.md` when commands, configuration, or HTTP API changes.
- Update `docs/ARCHITECTURE.md` when session, memory, RAG, MCP, telemetry, or storage design changes.
- Keep markdown examples runnable and aligned with current command names.

## Security

- Do not commit `config.yaml`, secrets, tokens, API keys, database passwords for real environments, or credential files.
- Keep `gemini_api_key` and MCP server environment values out of logs.
- Treat MCP tools as potentially sensitive; preserve confirmation support for risky toolsets.
