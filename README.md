# Personal Assistant

First version of a small personal assistant service built with Go, ADK-Go, PostgreSQL, `slog`, and OpenTelemetry.

## Scope

- Multi-session chat API.
- Durable ADK session storage in PostgreSQL.
- Short-term memory through ADK session history plus a rolling session summary.
- Long-term memory and RAG through PostgreSQL full-text search.
- Hybrid RAG with PostgreSQL full-text search plus pgvector.
- Optional MCP stdio toolsets attached to the ADK LLM agent.
- Structured logs with `log/slog`.
- OpenTelemetry tracing and metrics for HTTP, database-facing flows, chat, memory, and RAG steps.
- Request-scoped logs include `trace_id` and `span_id` when an OpenTelemetry span is active.

No UI, auth, billing, scheduler, or unrelated assistant features are included in this version.

## Requirements

- Go 1.25+.
- PostgreSQL 15+ with pgvector.
- A Gemini API key if `model_provider: gemini`.

## Quick Start

```sh
make config
createdb personal_assistant
make run
```

The service reads configuration from YAML, runs migrations at startup, and listens on `http_addr`, defaulting to `:8080`.

Set `printconfig: true` to print the final effective configuration at startup after defaults, normalization, and derived values are applied. It defaults to `false` and prints full values without redaction.

For local wiring without a model key, keep `model_provider: echo`. For real calls, update `config.yaml`:

```yaml
model_provider: gemini
gemini_api_key: "..."
```

Embeddings default to local deterministic hashing so the vector pipeline works without external calls:

```yaml
embedding_provider: hash
embedding_model: text-embedding-004
embedding_dimension: 768
```

For semantic vector search in production, use Gemini embeddings:

```yaml
embedding_provider: gemini
embedding_model: text-embedding-004
gemini_api_key: "..."
```

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

Pinned local stack images:

- PostgreSQL with pgvector: `pgvector/pgvector:0.8.2-pg16-bookworm`
- OTel Collector Contrib: `otel/opentelemetry-collector-contrib:0.150.0`
- Prometheus: `prom/prometheus:v3.11.2-distroless`
- Tempo: `grafana/tempo:2.8.3`
- Grafana: `grafana/grafana:13.0.1`

Local service URLs:

- Assistant: `http://localhost:8080`
- Grafana: `http://localhost:3000` with `admin` / `admin`
- Prometheus: `http://localhost:9090`
- Tempo: `http://localhost:3200`
- OTel Collector OTLP HTTP: `http://localhost:4318`

Grafana provisions the `Personal Assistant Overview` dashboard automatically under the `Personal Assistant` folder. It includes panels for chat volume and latency, HTTP errors, RAG retrievals and latency, memory writes/searches, session events, OTel Collector trace/metric pipeline health, and recent Tempo traces.

To inspect traces directly, open Grafana, go to `Explore`, select the `Tempo` data source, and run a TraceQL query such as:

```text
{resource.service.name="personal-assistant"}
```

The local Grafana Tempo data source uses HTTP query APIs. Streaming search is intentionally disabled because the Compose stack exposes Tempo's HTTP endpoint to Grafana, not the Tempo gRPC streaming query endpoint.

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

Docker Compose wires `otel_exporter_otlp_endpoint` to `http://otel-collector:4318`. The collector exposes application metrics to Prometheus and sends traces to Tempo. Grafana is provisioned with Prometheus and Tempo datasources.

## HTTP API

Create a session:

```sh
curl -sS -X POST localhost:8080/v1/sessions \
  -H 'content-type: application/json' \
  -d '{"user_id":"me","title":"daily"}'
```

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
- `internal/httpapi`: JSON HTTP API.
- `internal/modelx`: model provider adapters.
- `internal/observability`: `slog` and OpenTelemetry setup.
- `internal/rag`: RAG retrieval formatting.
- `internal/embedding`: hash and Gemini embedding providers.
- `internal/store`: PostgreSQL session, memory, summary, hybrid search, and migration code.
- `internal/store/migrations`: embedded SQL schema.

See `docs/ARCHITECTURE.md` for the design details.
