package main

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

type mcpToolsCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	OrgID       string `help:"optional orgID"`
}

func (cmd *mcpToolsCmd) Run(_ *globalOptions) error {
	mcpURL := buildMCPURL(cmd.APIEndpoint)

	c, _, err := initMCPClient(mcpURL, cmd.OrgID)
	if err != nil {
		return err
	}
	defer c.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	toolsResult, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return fmt.Errorf("MCP list tools failed: %w", err)
	}

	fmt.Printf("MCP tools (%d):\n", len(toolsResult.Tools))
	for _, t := range toolsResult.Tools {
		desc := t.Description
		if len(desc) > 60 {
			desc = desc[:57] + "..."
		}
		fmt.Printf("  - %-25s %s\n", t.Name, desc)
	}

	return nil
}
