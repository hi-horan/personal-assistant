# Personal Assistant

First version of a small personal assistant service built with Go, ADK-Go, PostgreSQL, `slog`, and OpenTelemetry.

## Scope

- Multi-session chat API.
- Durable ADK session storage in PostgreSQL.
- Short-term memory through ADK session history plus a rolling session summary.
- Long-term memory and RAG through PostgreSQL full-text search.
- Hybrid RAG with ParadeDB BM25 plus pgvector.
- Optional MCP stdio toolsets attached to the ADK LLM agent.
- Structured logs with `log/slog`.
- OpenTelemetry tracing and metrics for HTTP, database-facing flows, chat, memory, and RAG steps.
- Request-scoped logs include `trace_id` and `span_id` when an OpenTelemetry span is active.

No UI, auth, billing, scheduler, or unrelated assistant features are included in this version.

## Requirements

- Go 1.25+.
- PostgreSQL 15+ with pgvector.
- A Gemini API key if `model_provider: gemini`.
- A Z.ai / BigModel API key if `model_provider: glm`.

## Quick Start

```sh
make config
createdb personal_assistant
make run
```

The service reads configuration from YAML, runs migrations at startup, and listens on `http_addr`, defaulting to `:8080`.

Set `printconfig: true` to print the final effective configuration at startup after defaults, normalization, and derived values are applied. It defaults to `false` and prints full values without redaction.

The root assistant prompt is configured with `instruction`. The default and example config use Chinese:

```yaml
instruction: |
  你是一个简洁、可靠的个人助手。
  请优先使用当前会话历史、会话摘要和长期记忆中与问题相关的信息。
  记忆上下文：
  {rag_context?}
```

Open `http://localhost:8080/` for the built-in web client. It supports selecting a user, creating or loading sessions, sending chat messages, and inspecting the latest RAG/memory details.

For local wiring without a model key, keep `model_provider: echo`. For real calls, update `config.yaml`:

```yaml
model_provider: gemini
gemini_api_key: "..."
```

To use GLM-4.6V through the Z.ai / BigModel OpenAI-compatible API:

```yaml
model_provider: glm
glm_model: glm-4.6v
glm_api_key: "..."
glm_base_url: "https://open.bigmodel.cn/api/paas/v4"
glm_thinking_type: ""
```

Set `glm_thinking_type: enabled` or `disabled` only when you want to send GLM's provider-specific `thinking` option. The current HTTP API is text-only, so GLM-4.6V is called as a chat model even though the model itself supports vision inputs.

The GLM provider supports both non-streaming and ADK streaming model calls. The `/v1/chat` endpoint returns one JSON response, while `/v1/chat/stream` streams chat progress as server-sent events for the built-in web client.

When `log_level: debug`, GLM logs the final converted provider request body before the HTTP call and the raw provider response body before conversion. Streaming GLM calls log each raw SSE `data:` chunk. These logs use the request context, so `trace_id` and `span_id` are included when a span is active.

Embeddings default to local deterministic hashing so the vector pipeline works without external calls:

```yaml
embedding_provider: hash
embedding_model: text-embedding-004
embedding_dimension: 1024
```

For semantic vector search in production, use Gemini embeddings:

```yaml
embedding_provider: gemini
embedding_model: text-embedding-004
gemini_api_key: "..."
```

Or use BigModel Embedding-3:

```yaml
embedding_provider: bigmodel
embedding_model: embedding-3
embedding_dimension: 1024
bigmodel_api_key: "..."
bigmodel_base_url: "https://open.bigmodel.cn/api/paas/v4"
```

The PostgreSQL schema currently uses `memory_chunks.embedding vector(1024)`, so BigModel requests send `dimensions: 1024`.
Embedding vectors are stored and queried as returned by the provider. The project does not normalize vectors in Go because pgvector cosine distance (`<=>`) handles length during similarity calculation.

Then run:

```sh
make run
```

After building a binary, the equivalent command is:

```sh
make build
./bin/assistant run -c config.yaml
```

## Make Targets

```sh
make help
make config
make run
make build
make test
make tidy
make fmt
make check
make clean
make docker-build
make docker-up
make docker-down
make docker-logs
```

Use `CONFIG=/path/to/config.yaml make run` to run with a non-default config file.

## Docker

Build the image:

```sh
make docker-build
```

Run PostgreSQL, the assistant, OTel Collector, Prometheus, Tempo, and Grafana together:

```sh
make docker-up
```

Compose uses pinned image tags and `config.compose.yaml`, which points `database_url` at the `postgres` service and exports OTel data to `otel-collector`.

All Compose services run with `TZ=Asia/Shanghai`. PostgreSQL also sets `PGTZ=Asia/Shanghai`, and Grafana sets `GF_DATE_FORMATS_DEFAULT_TIMEZONE=Asia/Shanghai`.

The local database image is ParadeDB on PostgreSQL 17. If a previous local stack used a PostgreSQL 16 volume, recreate the `postgres-data` volume before starting Compose.

Pinned local stack images:

- ParadeDB with pg_search and pgvector: `paradedb/paradedb:0.23.1-pg17`
- OTel Collector Contrib: `otel/opentelemetry-collector-contrib:0.150.1`
- Prometheus: `prom/prometheus:v3.11.2-distroless`
- Tempo: `grafana/tempo:2.8.4`
- Grafana: `grafana/grafana:13.0.1`

Local service URLs:

- Assistant: `http://localhost:8080`
- Grafana: `http://localhost:3000` with `admin` / `admin`
- Prometheus: `http://localhost:9090`
- Tempo: `http://localhost:3200`
- OTel Collector OTLP HTTP: `http://localhost:4318`

Grafana provisions the `Personal Assistant Overview` dashboard automatically under the `Personal Assistant` folder. It includes panels for chat volume and latency, HTTP errors, RAG retrievals and latency, memory writes/searches, session events, OTel Collector trace/metric pipeline health, and recent Tempo traces.

Grafana plugin preinstall and auto-update are disabled in Compose. The local observability stack only uses built-in Prometheus and Tempo data sources, so it does not need Grafana's background plugin installer.

To inspect traces directly, open Grafana, go to `Explore`, select the `Tempo` data source, and run a TraceQL query such as:

```text
{resource.service.name="personal-assistant"}
```

The local Grafana Tempo data source uses Tempo's HTTP query API at `http://tempo:3200`. Do not point Grafana at `tempo:4317`; that port is OTLP ingest, not the Tempo query API. The dashboard's trace panel starts with a broad `{}` TraceQL query so Grafana display issues can be separated from service-name filtering.

Stop services with:

```sh
make docker-down
```

## Observability

Logs are JSON lines written to stdout. Logs include a relative `source` value in `path/to/file.go:line` form. Logs emitted with a request context include `trace_id` and `span_id`, so they can be correlated with OpenTelemetry traces.

Set `otel_exporter_otlp_endpoint` to enable OTLP HTTP trace export. Metrics use the same endpoint by default; set `otel_metrics_endpoint` only when metrics should go to a different OTLP HTTP endpoint.

```yaml
otel_service_name: personal-assistant
otel_exporter_otlp_endpoint: "http://localhost:4318"
otel_metrics_endpoint: ""
```

Docker Compose wires `otel_exporter_otlp_endpoint` to `http://otel-collector:4318`. The collector exposes application metrics to Prometheus and sends traces to Tempo through OTLP HTTP on `http://tempo:4318`. Grafana is provisioned with Prometheus and Tempo datasources.

The local collector also has a `debug` trace exporter enabled. When the assistant emits spans, `docker compose logs otel-collector` should show exported trace batches. If collector logs show trace batches but Grafana has no results, the issue is between Collector, Tempo, and Grafana. If collector logs show no trace batches after assistant requests, the issue is between the assistant and Collector.

Trace troubleshooting flow:

```sh
docker compose logs --tail=200 otel-collector
docker compose logs --tail=200 tempo
curl -sS http://localhost:3200/ready
curl -G 'http://localhost:3200/api/search' --data-urlencode 'q={resource.service.name="personal-assistant"}'
```

If Collector debug logs include trace IDs, query one directly:

```sh
curl -sS http://localhost:3200/api/traces/<trace_id>
```

Prometheus also scrapes Tempo metrics. Use `tempo_distributor_spans_received_total` or related `tempo_*` series to confirm Tempo ingestion.

## HTTP API

Create a session:

```sh
curl -sS -X POST localhost:8080/v1/sessions \
  -H 'content-type: application/json' \
  -d '{"user_id":"me","title":"daily"}'
```

Session, event, memory, and memory chunk IDs are stored as `bigint` microsecond timestamp IDs allocated by the backend. HTTP and ADK-facing APIs expose those IDs as decimal strings. `app_name` is limited to 256 characters and `user_id` is limited to 50 characters.

Chat:

```sh
curl -sS -X POST localhost:8080/v1/chat \
  -H 'content-type: application/json' \
  -d '{"user_id":"me","session_id":"<session id>","message":"remember that I prefer short answers"}'
```

Save memory explicitly:

```sh
curl -sS -X POST localhost:8080/v1/memories \
  -H 'content-type: application/json' \
  -d '{"user_id":"me","content":"I prefer short answers","kind":"semantic"}'
```

Search memory:

```sh
curl -sS 'localhost:8080/v1/memories/search?user_id=me&q=answer%20style'
```

## MCP

Add MCP servers under `mcp.servers` in `config.yaml`:

```yaml
mcp:
  servers:
    - name: filesystem
      command: npx
      args:
        - -y
        - "@modelcontextprotocol/server-filesystem"
        - /tmp
      env: {}
      require_confirmation: true
```

The first version supports stdio MCP servers. Sensitive toolsets should use `require_confirmation`.

## Project Layout

- `cmd/assistant`: executable entrypoint.
- `Dockerfile`: multi-stage container build for the assistant binary.
- `compose.yaml`: local PostgreSQL, assistant, OTel Collector, Prometheus, Tempo, and Grafana stack.
- `deploy/`: local observability stack configuration.
- `internal/app`: application wiring and ADK runner setup.
- `internal/config`: YAML config loading and validation.
- `internal/httpapi`: JSON HTTP API and SSE chat endpoint.
- `internal/httpapi/web`: embedded static web client.
- `internal/modelx`: model provider adapters.
- `internal/observability`: `slog` and OpenTelemetry setup.
- `internal/rag`: RAG retrieval formatting.
- `internal/embedding`: hash, Gemini, and BigModel embedding providers.
- `internal/store`: PostgreSQL session, memory, summary, hybrid search, and migration code.
- `internal/store/migrations`: embedded SQL schema.

See `docs/ARCHITECTURE.md` for the design details.
