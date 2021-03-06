package main

import (
	"context"
	"os"

	"github.com/m-mizutani/deepalert"
	"github.com/m-mizutani/deepalert/inspector"
)

func lookupHostname(value string) *string {
	response := "resolved.hostname" // It's jsut example, OK?
	return &response
}

func handler(ctx context.Context, attr deepalert.Attribute) (*deepalert.TaskResult, error) {
	// Check type of the attribute
	if attr.Type != deepalert.TypeIPAddr {
		return nil, nil
	}

	// Example
	resp := lookupHostname(attr.Value)
	if resp == nil {
		return nil, nil
	}

	result := deepalert.TaskResult{
		Contents: []deepalert.ReportContent{
			&deepalert.ReportHost{
				HostName: []string{*resp},
			},
		},
	}

	return &result, nil
}

func main() {
	inspector.Start(inspector.Arguments{
		Handler:         handler,
		Author:          "testInspector",
		ContentQueueURL: os.Getenv("CONTENT_QUEUE"),
		AttrQueueURL:    os.Getenv("ATTRIBUTE_QUEUE"),
	})
}
