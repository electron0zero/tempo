package main

import (
	"fmt"
	"net/url"
	"os"

	"github.com/grafana/tempo/pkg/api"
	"github.com/grafana/tempo/pkg/httpclient"
	"github.com/grafana/tempo/pkg/tracediffsvg"
)

type queryTraceDiffSVGCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	BaseTraceID string `arg:"" help:"base trace ID to compare from"`
	NextTraceID string `arg:"" help:"next trace ID to compare to"`

	Output string `short:"o" help:"output SVG file path" default:"trace-diff.svg"`
	OrgID  string `help:"optional orgID"`
}

func (cmd *queryTraceDiffSVGCmd) Run(_ *globalOptions) error {
	client := httpclient.New(cmd.APIEndpoint, cmd.OrgID)

	diffURL := cmd.APIEndpoint + api.PathTraceDiff + "?base=" + url.QueryEscape(cmd.BaseTraceID) + "&next=" + url.QueryEscape(cmd.NextTraceID)
	body, err := client.GetLLMFormat(diffURL)
	if err != nil {
		return err
	}

	f, err := os.Create(cmd.Output)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if err := tracediffsvg.RenderFromLLMJSON(f, []byte(body), cmd.BaseTraceID, cmd.NextTraceID); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "wrote %s\n", cmd.Output)
	return nil
}
