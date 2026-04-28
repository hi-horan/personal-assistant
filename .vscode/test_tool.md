curl -X POST "https://open.bigmodel.cn/api/paas/v4/chat/completions" \
  -H "Authorization: Bearer cba5feb3acce474eaaae7b20edc99842.4cEJpSUaxguXp0R1" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-4.6v",
    "messages": [
      {
        "role": "user",
        "content": "北京今天的天气怎么样？"
      },
      {
        "role": "assistant",
        "content": null,
        "tool_calls": [
          {
            "id": "call_xxx",
            "type": "function",
            "function": {
              "name": "get_weather",
              "arguments": "{\"city\":\"北京\"}"
            }
          }
        ]
      },
      {
        "role": "tool",
        "tool_call_id": "call_xxx",
        "content": "{\"city\":\"北京\",\"temperature\":\"22°C\",\"condition\":\"晴\",\"humidity\":\"45%\"}"
      }
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "get_weather",
          "description": "获取指定城市的当前天气信息",
          "parameters": {
            "type": "object",
            "properties": {
              "city": {
                "type": "string"
              }
            },
            "required": ["city"]
          }
        }
      }
    ],
    "thinking": {
      "type": "disabled"
    },
    "stream": false
  }'



curl --location 'https://open.bigmodel.cn/api/paas/v4/chat/completions' \
  -H "Authorization: Bearer cba5feb3acce474eaaae7b20edc99842.4cEJpSUaxguXp0R1" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-4.6v",
    "messages": [
      {
        "role": "user",
        "content": "请搜索最新的人工智能发展趋势，并用三句话总结。"
      }
    ],
    "tools": [
      {
        "type": "mcp",
        "mcp": {
          "server_label": "web-search-prime",
          "server_url": "https://open.bigmodel.cn/api/mcp/web_search_prime/mcp",
          "transport_type": "streamable-http",
          "headers": {
            "Authorization": "Bearer cba5feb3acce474eaaae7b20edc99842.4cEJpSUaxguXp0R1"
          },
          "allowed_tools": ["webSearchPrime"]
        }
      }
    ],
    "tool_choice": "auto",
    "stream": false
  }'
