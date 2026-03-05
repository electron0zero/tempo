package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/grafana/tempo/pkg/api"
)

type queryMCPStatusCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`

	OrgID     string `help:"optional orgID"`
	ListTools bool   `name:"list-tools" help:"list available MCP tools"`
	Claude    bool   `help:"print Claude Code MCP config"`
	Cursor    bool   `help:"print Cursor MCP config"`
	Windsurf  bool   `help:"print Windsurf MCP config"`
}

func (cmd *queryMCPStatusCmd) Run(_ *globalOptions) error {
	mcpURL := strings.TrimRight(cmd.APIEndpoint, "/") + api.PathMCP

	var opts []transport.StreamableHTTPCOption
	if cmd.OrgID != "" {
		opts = append(opts, transport.WithHTTPHeaders(map[string]string{
			"X-Scope-OrgID": cmd.OrgID,
		}))
	}

	c, err := mcpclient.NewStreamableHttpClient(mcpURL, opts...)
	if err != nil {
		return fmt.Errorf("failed to create MCP client: %w", err)
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	initResult, err := c.Initialize(ctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo: mcp.Implementation{
				Name:    "tempo-cli",
				Version: "0.1.0",
			},
		},
	})
	if err != nil {
		return fmt.Errorf("MCP initialize failed: %w", err)
	}

	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("MCP ping failed: %w", err)
	}

	// Fetch tool count (or full list if --list-tools)
	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("MCP list tools failed: %w", err)
	}

	fmt.Println("MCP server is healthy")
	fmt.Printf("  Server:   %s %s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Printf("  Protocol: %s\n", initResult.ProtocolVersion)
	fmt.Printf("  Endpoint: %s\n", mcpURL)

	if cmd.ListTools {
		fmt.Println("  Tools:")
		for _, t := range toolsResult.Tools {
			desc := t.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Printf("    - %-25s %s\n", t.Name, desc)
		}
	} else {
		fmt.Printf("  Tools:    %d available (use --list-tools to see details)\n", len(toolsResult.Tools))
	}

	if cmd.Claude {
		printAgentConfig("Claude Code", "Add to ~/.claude/settings.json under \"mcpServers\":", mcpURL, "claude")
	}
	if cmd.Cursor {
		printAgentConfig("Cursor", "Add to .cursor/mcp.json under \"mcpServers\":", mcpURL, "cursor")
	}
	if cmd.Windsurf {
		printAgentConfig("Windsurf", "Add to ~/.codeium/windsurf/mcp_config.json under \"mcpServers\":", mcpURL, "windsurf")
	}

	return nil
}

func printAgentConfig(agent, instruction, mcpURL, format string) {
	fmt.Printf("\n--- %s ---\n%s\n\n", agent, instruction)

	var cfg map[string]any
	switch format {
	case "claude":
		cfg = map[string]any{
			"tempo": map[string]any{
				"type": "url",
				"url":  mcpURL,
			},
		}
	case "cursor":
		cfg = map[string]any{
			"tempo": map[string]any{
				"url": mcpURL,
			},
		}
	case "windsurf":
		cfg = map[string]any{
			"tempo": map[string]any{
				"serverUrl": mcpURL,
			},
		}
	}

	out, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Println(string(out))
}
