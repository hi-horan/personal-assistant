# AGENTS.md

技术栈: golang, paradedb(pgvector, pg_search), adk-go, slog, otel(tempo, prometheus, grafana)

## 目标：实现个人 AI 聊天助手,大量，深度参考 /Users/horan/Desktop/llm/openclaw1/goclaw 项目并进行裁剪，其他多租户等功能全都不要，只保留以下
1. 所有llm provider
2. hooks 系统， 7 个生命周期事件（SessionStart、UserPromptSubmit、PreToolUse、PostToolUse、Stop、SubagentStart/Stop）— 同步/异步，防 SSRF HTTP 处理器，审计日志
3. Audio / TTS 管理器	统一音频管理器，支持 4 个 TTS provider：ElevenLabs（流式）、OpenAI、Edge TTS、MiniMax；语音 LRU 缓存（1 000 租户，TTL 1 小时）
4. 32 个内置工具	文件系统、网页搜索、浏览器、代码执行、记忆等
5. 修改部分：有个简单页面，可以多session切换，聊天窗口，展示聊天记录，与web交互还是用 http sse
6. 三层记忆	L0/L1/L2 配合 consolidation worker（episodic、semantic、dreaming、dedup）
7. 知识库 Knowledge Vault	Wikilink 文档网格、LLM 自动摘要 + 语义自动链接、BM25 + 向量混合搜索
8. 知识图谱	基于 LLM 的实体/关系提取，支持图遍历
9. Agent 进化	Guardrail + suggestion engine；预定义 agent 自我优化 SOUL.md / CAPABILITIES.md 并构建新 skill
10. Mode Prompt 系统	可切换的 prompt 模式（full / task / minimal / none），支持 per-agent 覆盖
11. MCP 支持	连接 Model Context Protocol 服务器（stdio/SSE/HTTP）(adk-go自带， 请用adk-go从新实现)
12. Skills 系统	基于 SKILL.md 的知识库，支持混合搜索；支持发布、授权，以及 evolution 驱动的 skill draft (adk-go自带skills， 请用adk-go从新实现)
13. Quality Gate	基于 hook 的输出验证，可配置反馈循环
14. 扩展思考	每个 provider 的推理模式（Anthropic、OpenAI、DashScope）
15. Prompt 缓存	在重复前缀上最高降低约 90% 成本；v3 cache-boundary marker
16. 8 阶段 Agent Pipeline	context → history → prompt → think → act → observe → memory → summarize（v3，始终启用）

## 修改点
1. 改成个人应用，多租户，多agent，多用户，全去掉
2. 命令行解析用 cobra，
3. agent 框架用 google 的 adk-go, 它里面实现了 mcp，skills，tool，看能不能服用，不用 goclaw自身实现的
4. 配置从 yaml 文件读取 比如 ./assistant run -c config.yaml
5. 日志用 slog， 遥测用 otel push
6. prompt 等能配置等东西全都放到 config文件
7. 日志要记录 traceid，spanid，与 trace 关联
8. 日志slog记录参数要用 attrs
9. 注释用中文，要详略得当，复杂的地方加注释，简单的略掉
10. 要懂得代码可读性，代码抽象
11. db 主键用 bigint 后端服务分配id，id为递增微妙时间戳，支持并发
12. db fts 用 jieba

## 重点：

1. 重点关注记忆相关的问题，比如 摘要，压缩，embedding，bm25, 混合搜索，尽量仿照 goclaw ，这是我关注的重点
2. goclaw 官网 https://docs.goclaw.sh, 数据库 schema: https://docs.goclaw.sh/database-schema



## 注意

1. 按照目前golang通常优雅的实现，代码组织结构优化，不是完全照搬 goclaw 实现
2. 最底层的框架可能不照搬，比如 是否能用 kratos实现 http sse
3. db的表结构先确定一下，需要我先审核一下，goclaw 表过多，要大幅删减
4. 当前的代码实现不用考虑太多，推倒重来都是可以的
5. 日志记录详细，debug级别 重要操作 前后都要记录
6. 错误处理要充分覆盖
7. metrics 要详尽