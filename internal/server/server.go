package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"personal-assistant/internal/chat"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type Config struct {
	Addr    string
	AppName string
	Logger  *slog.Logger
	Chat    *chat.Service
}

func New(cfg Config) *http.Server {
	mux := http.NewServeMux()
	startedAt := time.Now()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(indexHTML))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"status":     "ready",
			"app":        cfg.AppName,
			"started_at": startedAt.Format(time.RFC3339),
		})
	})
	if cfg.Chat != nil {
		api := &apiHandler{chat: cfg.Chat, logger: cfg.Logger}
		mux.HandleFunc("/api/v1/sessions", api.sessions)
		mux.HandleFunc("/api/v1/sessions/", api.sessionByID)
	}

	handler := requestLogger(cfg.Logger)(otelhttp.NewHandler(mux, "http.server"))
	return &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

type apiHandler struct {
	chat   *chat.Service
	logger *slog.Logger
}

type createSessionRequest struct {
	Title string `json:"title"`
}

type chatRequest struct {
	Message string `json:"message"`
}

func (h *apiHandler) sessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions, err := h.chat.ListSessions(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]any{"sessions": sessions})
	case http.MethodPost:
		var req createSessionRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		sess, err := h.chat.CreateSession(r.Context(), req.Title)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSONStatus(w, http.StatusCreated, map[string]any{"session": sess})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeErrorText(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *apiHandler) sessionByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	sessionID, action := parts[0], parts[1]
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet && action == "messages":
		messages, err := h.chat.Messages(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		writeJSON(w, map[string]any{"messages": messages})
	case len(parts) == 2 && r.Method == http.MethodPost && action == "chat":
		var req chatRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		result, err := h.chat.Chat(r.Context(), sessionID, req.Message)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, result)
	case len(parts) == 3 && r.Method == http.MethodPost && action == "chat" && parts[2] == "stream":
		var req chatRequest
		if err := readJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		h.streamChat(w, r, sessionID, req.Message)
	default:
		http.NotFound(w, r)
	}
}

func (h *apiHandler) streamChat(w http.ResponseWriter, r *http.Request, sessionID, message string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErrorText(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	enc := json.NewEncoder(w)
	for event := range h.chat.ChatStream(r.Context(), sessionID, message) {
		if event.Error != "" {
			_, _ = w.Write([]byte("event: error\n"))
		} else {
			_, _ = w.Write([]byte("event: message\n"))
		}
		_, _ = w.Write([]byte("data: "))
		if err := enc.Encode(event); err != nil {
			if h.logger != nil {
				h.logger.Warn("sse encode failed", slog.Any("error", err))
			}
			return
		}
		_, _ = w.Write([]byte("\n"))
		flusher.Flush()
	}
	_, _ = w.Write([]byte("event: done\ndata: {}\n\n"))
	flusher.Flush()
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	err := json.NewDecoder(r.Body).Decode(dst)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

const indexHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Personal Assistant</title>
  <style>
    body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #f7f7f8; color: #202124; }
    .app { display: grid; grid-template-columns: 260px 1fr; min-height: 100vh; }
    aside { border-right: 1px solid #ddd; background: #fff; padding: 16px; }
    main { display: grid; grid-template-rows: auto 1fr auto; min-width: 0; }
    header { padding: 16px 20px; border-bottom: 1px solid #ddd; background: #fff; font-weight: 600; }
    button { border: 1px solid #c8c8cc; background: #fff; border-radius: 6px; padding: 8px 10px; cursor: pointer; }
    .sessions { display: grid; gap: 8px; margin-top: 16px; }
    .session { text-align: left; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .session.active { border-color: #111; }
    .messages { padding: 20px; overflow: auto; display: grid; align-content: start; gap: 12px; }
    .msg { max-width: 780px; padding: 10px 12px; border-radius: 8px; background: #fff; border: 1px solid #e2e2e4; white-space: pre-wrap; }
    .msg.user { justify-self: end; background: #e9f2ff; border-color: #cfe1ff; }
    form { display: flex; gap: 10px; padding: 16px 20px; border-top: 1px solid #ddd; background: #fff; }
    input { flex: 1; min-width: 0; border: 1px solid #c8c8cc; border-radius: 6px; padding: 10px 12px; font-size: 14px; }
    @media (max-width: 720px) { .app { grid-template-columns: 1fr; } aside { border-right: 0; border-bottom: 1px solid #ddd; } }
  </style>
</head>
<body>
  <div class="app">
    <aside>
      <button id="new-session">新会话</button>
      <div id="sessions" class="sessions"></div>
    </aside>
    <main>
      <header id="title">Personal Assistant</header>
      <div id="messages" class="messages"></div>
      <form id="form">
        <input id="input" autocomplete="off" placeholder="输入消息">
        <button>发送</button>
      </form>
    </main>
  </div>
  <script>
    let currentSession = "";
    const sessionsEl = document.querySelector("#sessions");
    const messagesEl = document.querySelector("#messages");
    const titleEl = document.querySelector("#title");
    async function api(path, options = {}) {
      const res = await fetch("/api/v1" + path, { headers: { "content-type": "application/json" }, ...options });
      if (!res.ok) throw new Error(await res.text());
      return res.json();
    }
    function renderMessages(messages) {
      messagesEl.innerHTML = "";
      for (const m of messages) {
        const el = document.createElement("div");
        el.className = "msg " + m.role;
        el.textContent = m.content;
        messagesEl.appendChild(el);
      }
      messagesEl.scrollTop = messagesEl.scrollHeight;
    }
    async function loadSessions() {
      const data = await api("/sessions");
      sessionsEl.innerHTML = "";
      for (const s of data.sessions) {
        const btn = document.createElement("button");
        btn.className = "session" + (s.id === currentSession ? " active" : "");
        btn.textContent = s.title || s.id;
        btn.onclick = () => selectSession(s.id, s.title);
        sessionsEl.appendChild(btn);
      }
      if (!currentSession && data.sessions.length) await selectSession(data.sessions[0].id, data.sessions[0].title);
    }
    async function selectSession(id, title) {
      currentSession = id;
      titleEl.textContent = title || id;
      const data = await api("/sessions/" + id + "/messages");
      renderMessages(data.messages);
      await loadSessions();
    }
    document.querySelector("#new-session").onclick = async () => {
      const data = await api("/sessions", { method: "POST", body: JSON.stringify({}) });
      await selectSession(data.session.id, data.session.title);
    };
    document.querySelector("#form").onsubmit = async (e) => {
      e.preventDefault();
      const input = document.querySelector("#input");
      const message = input.value.trim();
      if (!message) return;
      if (!currentSession) {
        const data = await api("/sessions", { method: "POST", body: JSON.stringify({ title: message.slice(0, 24) }) });
        currentSession = data.session.id;
      }
      input.value = "";
      const data = await api("/sessions/" + currentSession + "/chat", { method: "POST", body: JSON.stringify({ message }) });
      renderMessages(data.messages);
      await loadSessions();
    };
    loadSessions().catch(console.error);
  </script>
</body>
</html>`

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeErrorText(w, status, err.Error())
}

func writeErrorText(w http.ResponseWriter, status int, message string) {
	writeJSONStatus(w, status, map[string]any{"error": message})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Debug("http request completed",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}
