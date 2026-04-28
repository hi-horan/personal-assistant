# Personal Assistant

Personal AI assistant service in Go, using ADK-Go, Cobra, YAML config, `slog`, OpenTelemetry, PostgreSQL/ParadeDB, and HTTP SSE.

## Run

```sh
make config
make run
```

The binary form is:

```sh
./bin/assistant run -c config.yaml
```

## API

All application APIs use `/api/v1`:

- `GET /api/v1/sessions`
- `POST /api/v1/sessions`
- `GET /api/v1/sessions/{session_id}/messages`
- `POST /api/v1/sessions/{session_id}/chat`
- `POST /api/v1/sessions/{session_id}/chat/stream`

The root `/` serves a small web chat page.

## Providers

Use `model_provider` to choose the active provider. The preferred configuration path is the `providers:` map in `config.example.yaml`.

Implemented provider types:

- `echo`
- `gemini`
- `openai_compat`

OpenAI-compatible defaults are registered for OpenAI, OpenRouter, DeepSeek, DashScope, GLM/BigModel, Groq, Mistral, xAI, MiniMax, Moonshot, SiliconFlow, Together, Fireworks, Perplexity, Novita, and Ollama.

## Validation

```sh
make fmt
make test
make build
```
