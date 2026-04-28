# Personal Assistant Architecture

Phase 2 establishes the minimum ADK-Go chat loop. Business schema has been reviewed separately, but runtime chat state is still in-memory until the migration/store phase.

## Runtime Shape

- `cmd/assistant`: cobra commands.
- `internal/config`: YAML loading, defaults, validation, redacted logging.
- `internal/logging`: JSON `slog` setup.
- `internal/telemetry`: OpenTelemetry trace and metric exporters.
- `internal/db`: PostgreSQL pool and connectivity checks.
- `internal/modelx`: model adapters. Phase 2 includes an ADK-compatible echo model.
- `internal/chat`: ADK `llmagent`, runner, and session service wiring.
- `internal/server`: HTTP health/readiness endpoints and `/api/v1` chat API.

## Commands

- `assistant run -c config.yaml`: start the HTTP service.
- `assistant migrate -c config.yaml`: verify DB connectivity; no schema migrations are registered in phase 0.
- `assistant doctor -c config.yaml`: validate config and ping the database.

## API

All application APIs use the `/api/v1` prefix.

- `GET /api/v1/sessions`: list chat sessions.
- `POST /api/v1/sessions`: create a chat session.
- `GET /api/v1/sessions/{session_id}/messages`: list messages in a session.
- `POST /api/v1/sessions/{session_id}/chat`: send one user message and receive a JSON response.
- `POST /api/v1/sessions/{session_id}/chat/stream`: send one user message and receive server-sent events.

The root `/` serves a small web client using these APIs.

## Provider Layer

Provider selection is driven by `model_provider` and optional `providers:` YAML entries.

Implemented provider types:

- `echo`: local ADK-compatible echo model for wiring tests.
- `gemini`: ADK-Go Gemini model adapter.
- `anthropic`: native Anthropic Messages adapter with JSON and SSE support.
- `openai_compat`: generic Chat Completions adapter with JSON and SSE support.

Built-in OpenAI-compatible provider names include `openai`, `openrouter`, `deepseek`, `dashscope`, `glm`, `bigmodel`, `groq`, `mistral`, `xai`, `minimax`, `moonshot`, `siliconflow`, `together`, `fireworks`, `perplexity`, `novita`, and `ollama`. Per-provider YAML can override model, key, base URL, API path, headers, timeout, reasoning effort, thinking type, and service tier.

Legacy `gemini_*`, `glm_*`, and `bigmodel_*` config fields remain supported while the new `providers:` map becomes the preferred path.

## Schema

No application tables are created yet. The schema proposal is in `docs/DB_SCHEMA_PROPOSAL.md`.
