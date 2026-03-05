package main

import (
	"fmt"
	"net/url"
	"os"

	"github.com/gogo/protobuf/jsonpb"
	"github.com/grafana/tempo/pkg/api"
	"github.com/grafana/tempo/pkg/httpclient"
)

type queryTraceDiffCmd struct {
	APIEndpoint string `arg:"" help:"tempo api endpoint"`
	BaseTraceID string `arg:"" help:"base trace ID to compare from"`
	NextTraceID string `arg:"" help:"next trace ID to compare to"`

	LLM   bool   `name:"llm" help:"return response in LLM format (simplified JSON)"`
	OrgID string `help:"optional orgID"`
}

func (cmd *queryTraceDiffCmd) Run(_ *globalOptions) error {
	client := httpclient.New(cmd.APIEndpoint, cmd.OrgID)

	if cmd.LLM {
		diffURL := cmd.APIEndpoint + api.PathTraceDiff + "?base=" + url.QueryEscape(cmd.BaseTraceID) + "&next=" + url.QueryEscape(cmd.NextTraceID)
		body, err := client.GetLLMFormat(diffURL)
		if err != nil {
			return err
		}
		fmt.Println(body)
		return nil
	}

	resp, err := client.QueryTraceDiff(cmd.BaseTraceID, cmd.NextTraceID)
	if err != nil {
		return err
	}

	if resp.Message != "" {
		_, _ = fmt.Fprintf(os.Stderr, "status: %s , message: %s\n", resp.Status, resp.Message)
	}

	marshaller := &jsonpb.Marshaler{}
	return marshaller.Marshal(os.Stdout, resp)
}
