package main

import (
	"encoding/json"
	"fmt"
)

type queryMCPConfigCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	OrgID       string `help:"optional orgID"`
	Claude      bool   `help:"print Claude Code MCP config"`
	Cursor      bool   `help:"print Cursor MCP config"`
	Windsurf    bool   `help:"print Windsurf MCP config"`
}

func (cmd *queryMCPConfigCmd) Run(_ *globalOptions) error {
	mcpURL := buildMCPURL(cmd.APIEndpoint)

	// If no specific flag, print all
	all := !cmd.Claude && !cmd.Cursor && !cmd.Windsurf

	if cmd.Claude || all {
		printAgentConfig("Claude Code", "Add to ~/.claude/settings.json under \"mcpServers\":", mcpURL, "claude")
	}
	if cmd.Cursor || all {
		printAgentConfig("Cursor", "Add to .cursor/mcp.json under \"mcpServers\":", mcpURL, "cursor")
	}
	if cmd.Windsurf || all {
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
