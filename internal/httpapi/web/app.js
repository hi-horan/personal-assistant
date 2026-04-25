const state = {
  userId: "me",
  sessionId: "",
  sessions: [],
};

const els = {
  status: document.querySelector("#status"),
  userId: document.querySelector("#userId"),
  sessions: document.querySelector("#sessions"),
  newSession: document.querySelector("#newSession"),
  refreshSessions: document.querySelector("#refreshSessions"),
  sessionTitle: document.querySelector("#sessionTitle"),
  sessionId: document.querySelector("#sessionId"),
  messages: document.querySelector("#messages"),
  form: document.querySelector("#chatForm"),
  message: document.querySelector("#message"),
  send: document.querySelector("#send"),
  details: document.querySelector("#details"),
};

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      "content-type": "application/json",
      ...(options.headers || {}),
    },
  });
  const text = await response.text();
  const data = text ? JSON.parse(text) : null;
  if (!response.ok) {
    const message = data?.error?.message || response.statusText;
    throw new Error(message);
  }
  return data;
}

function setStatus(text, isError = false) {
  els.status.textContent = text;
  els.status.className = isError ? "error" : "";
}

function updateSessionHeader() {
  const session = state.sessions.find((item) => item.id === state.sessionId);
  els.sessionTitle.textContent = session?.title || (state.sessionId ? "Active session" : "No session selected");
  els.sessionId.textContent = state.sessionId || "auto-create on send";
}

function renderSessions() {
  els.sessions.innerHTML = "";
  for (const session of state.sessions) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = session.id === state.sessionId ? "active" : "";
    button.innerHTML = `<strong>${escapeHTML(session.title || "Untitled")}</strong><small>${escapeHTML(session.id)}</small>`;
    button.addEventListener("click", () => selectSession(session.id));
    els.sessions.append(button);
  }
  updateSessionHeader();
}

async function refreshSessions() {
  state.userId = els.userId.value.trim() || "me";
  const data = await api(`/v1/sessions?user_id=${encodeURIComponent(state.userId)}`, {
    headers: {},
  });
  state.sessions = data.sessions || [];
  renderSessions();
}

async function selectSession(sessionId) {
  state.sessionId = sessionId;
  updateSessionHeader();
  renderSessions();
  els.messages.innerHTML = "";
  const data = await api(`/v1/sessions/${encodeURIComponent(sessionId)}?user_id=${encodeURIComponent(state.userId)}`, {
    headers: {},
  });
  for (const event of data.events || []) {
    if (event.text) {
      appendMessage(event.author === "user" ? "user" : "assistant", event.text, event.timestamp || "");
    }
  }
}

async function createSession() {
  state.userId = els.userId.value.trim() || "me";
  const title = `Session ${new Date().toLocaleString()}`;
  const session = await api("/v1/sessions", {
    method: "POST",
    body: JSON.stringify({ user_id: state.userId, title }),
  });
  state.sessionId = session.id;
  await refreshSessions();
  els.messages.innerHTML = "";
}

async function sendMessage(event) {
  event.preventDefault();
  const message = els.message.value.trim();
  if (!message) return;

  state.userId = els.userId.value.trim() || "me";
  appendMessage("user", message, new Date().toLocaleTimeString());
  els.message.value = "";
  els.send.disabled = true;
  setStatus("Sending...");

  try {
    const assistantMessage = appendMessage("assistant", "", new Date().toLocaleTimeString());
    let streamedText = "";
    let finalResponse = null;
    await streamChat("/v1/chat/stream", {
      method: "POST",
      body: JSON.stringify({
        user_id: state.userId,
        session_id: state.sessionId || undefined,
        message,
      }),
    }, {
      session(event) {
        state.sessionId = event.session_id || state.sessionId;
        updateSessionHeader();
      },
      delta(event) {
        streamedText += event.text || "";
        setMessageText(assistantMessage, streamedText || "...");
      },
      final(event) {
        finalResponse = event.response;
        state.sessionId = event.session_id || state.sessionId;
        setMessageText(assistantMessage, event.text || streamedText || "(empty response)");
      },
    });
    if (finalResponse) {
      els.details.textContent = JSON.stringify({
        session_id: finalResponse.session_id,
        rag: finalResponse.rag,
        memory: finalResponse.memory,
        events: finalResponse.events,
      }, null, 2);
    }
    await refreshSessions();
    setStatus("Ready");
  } catch (error) {
    appendMessage("assistant", error.message, new Date().toLocaleTimeString(), true);
    setStatus(error.message, true);
  } finally {
    els.send.disabled = false;
    els.message.focus();
  }
}

function appendMessage(role, text, meta, isError = false) {
  const item = document.createElement("article");
  item.className = `message ${role}`;
  item.innerHTML = `
    <div class="meta">${escapeHTML(role)}${meta ? ` · ${escapeHTML(meta)}` : ""}</div>
    <div class="bubble ${isError ? "error" : ""}">${escapeHTML(text)}</div>
  `;
  els.messages.append(item);
  els.messages.scrollTop = els.messages.scrollHeight;
  return item;
}

function setMessageText(item, text) {
  const bubble = item.querySelector(".bubble");
  if (bubble) {
    bubble.textContent = text;
  }
  els.messages.scrollTop = els.messages.scrollHeight;
}

async function streamChat(path, options, handlers) {
  const response = await fetch(path, {
    ...options,
    headers: {
      "content-type": "application/json",
      accept: "text/event-stream",
      ...(options.headers || {}),
    },
  });
  if (!response.ok || !response.body) {
    const text = await response.text();
    throw new Error(text || response.statusText);
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const blocks = buffer.split("\n\n");
    buffer = blocks.pop() || "";
    for (const block of blocks) {
      handleSSEBlock(block, handlers);
    }
  }
  if (buffer.trim()) {
    handleSSEBlock(buffer, handlers);
  }
}

function handleSSEBlock(block, handlers) {
  let event = "message";
  const dataLines = [];
  for (const line of block.split("\n")) {
    if (line.startsWith("event:")) {
      event = line.slice(6).trim();
    } else if (line.startsWith("data:")) {
      dataLines.push(line.slice(5).trimStart());
    }
  }
  if (!dataLines.length) return;
  const payload = JSON.parse(dataLines.join("\n"));
  if (event === "error") {
    throw new Error(payload.message || "stream failed");
  }
  const handler = handlers[event];
  if (handler) {
    handler(payload);
  }
}

function escapeHTML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#039;");
}

async function init() {
  els.form.addEventListener("submit", sendMessage);
  els.newSession.addEventListener("click", createSession);
  els.refreshSessions.addEventListener("click", () => refreshSessions().catch((error) => setStatus(error.message, true)));
  els.userId.addEventListener("change", () => {
    state.sessionId = "";
    refreshSessions().catch((error) => setStatus(error.message, true));
  });

  try {
    await api("/healthz", { headers: {} });
    setStatus("Ready");
    await refreshSessions();
  } catch (error) {
    setStatus(error.message, true);
  }
}

init();
