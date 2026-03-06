package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/grafana/tempo/pkg/api"
)

type mcpStatusCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	OrgID       string `help:"optional orgID"`
}

func (cmd *mcpStatusCmd) Run(_ *globalOptions) error {
	mcpURL := buildMCPURL(cmd.APIEndpoint)

	c, initResult, err := initMCPClient(mcpURL, cmd.OrgID)
	if err != nil {
		return err
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := c.Ping(ctx); err != nil {
		return fmt.Errorf("MCP ping failed: %w", err)
	}

	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("MCP list tools failed: %w", err)
	}

	fmt.Println("MCP server is healthy")
	fmt.Printf("  Server:   %s %s\n", initResult.ServerInfo.Name, initResult.ServerInfo.Version)
	fmt.Printf("  Protocol: %s\n", initResult.ProtocolVersion)
	fmt.Printf("  Endpoint: %s\n", mcpURL)
	fmt.Printf("  Tools:    %d available\n", len(toolsResult.Tools))

	return nil
}

// buildMCPURL constructs the MCP endpoint URL from a base Tempo API endpoint.
func buildMCPURL(endpoint string) string {
	return strings.TrimRight(endpoint, "/") + api.PathMCP
}

// initMCPClient creates an MCP client, runs the initialize handshake, and returns the client and result.
func initMCPClient(mcpURL, orgID string) (*mcpclient.Client, *mcp.InitializeResult, error) {
	var opts []transport.StreamableHTTPCOption
	if orgID != "" {
		opts = append(opts, transport.WithHTTPHeaders(map[string]string{
			"X-Scope-OrgID": orgID,
		}))
	}

	c, err := mcpclient.NewStreamableHttpClient(mcpURL, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

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
		c.Close()
		return nil, nil, fmt.Errorf("MCP initialize failed: %w", err)
	}

	return c, initResult, nil
}
