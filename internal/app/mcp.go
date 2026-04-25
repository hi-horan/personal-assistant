package app

import (
	"fmt"
	"os"
	"os/exec"
	"sort"

	"personal-assistant/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
)

func newMCPToolsets(cfg config.MCPConfig) ([]tool.Toolset, error) {
	toolsets := make([]tool.Toolset, 0, len(cfg.Servers))
	for _, server := range cfg.Servers {
		command := exec.Command(server.Command, server.Args...)
		command.Env = append(os.Environ(), envPairs(server.Env)...)
		toolset, err := mcptoolset.New(mcptoolset.Config{
			Transport:           &mcp.CommandTransport{Command: command},
			RequireConfirmation: server.RequireConfirmation,
		})
		if err != nil {
			return nil, fmt.Errorf("create mcp toolset %q: %w", server.Name, err)
		}
		toolsets = append(toolsets, toolset)
	}
	return toolsets, nil
}

func envPairs(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}
