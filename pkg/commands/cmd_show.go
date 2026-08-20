package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func showCommand() Definition {
	return Definition{
		Name:        "show",
		Description: "Show current configuration",
		Category:    "Discovery",
		Examples:    []string{"/show", "/show mcp filesystem"},
		Handler:     discoveryDashboardHandler("show"),
		SubCommands: []SubCommand{
			{
				Name:        "model",
				Description: "Show current model and provider",
				Handler:     showModelHandler(),
			},
			{
				Name:        "channel",
				Description: "Show current channel",
				Handler: func(_ context.Context, req Request, _ *Runtime) error {
					fallback := fmt.Sprintf("Current Channel: %s", req.Channel)
					return req.replyStructured(
						tableContent(
							"Channel",
							detailHeaderColumns(),
							[][]string{{"Current Channel", req.Channel}},
							fallback,
						),
					)
				},
			},
			{
				Name:        "agents",
				Description: "Show registered agents",
				Handler:     agentsHandler(),
			},
			{
				Name:        "mcp",
				Description: "Show status and current tools for one MCP server",
				ArgsUsage:   "<server>",
				Examples:    []string{"/show mcp filesystem"},
				Handler:     showMCPToolsHandler(),
			},
		},
	}
}

func showModelHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		// Mature model semantics are session-aware. Keep GetModelInfo only as a
		// compatibility fallback for narrow test/minimal runtimes.
		if rt != nil && rt.ModelCommand != nil {
			content, err := rt.ModelCommand(ctx, ModelCommandRequest{Operation: "current"})
			if err != nil {
				return req.Reply(UserFacingError(err, "Model service is temporarily unavailable. Please try again."))
			}
			if content != nil {
				prependCurrentModelFallback(content)
				return req.replyStructured(*content)
			}
		}
		if rt == nil || rt.GetModelInfo == nil {
			return req.Reply(unavailableMsg)
		}
		name, provider := rt.GetModelInfo()
		fallback := fmt.Sprintf("Current Model: %s (Provider: %s)", name, provider)
		return req.replyStructured(
			tableContent(
				"Model",
				detailHeaderColumns(),
				[][]string{{"Current Model", name}, {"Provider", provider}},
				fallback,
			),
		)
	}
}

func prependCurrentModelFallback(content *bus.StructuredContent) {
	if content == nil {
		return
	}
	properties := make(map[string]string)
	for _, table := range content.Tables {
		for _, row := range table.Rows {
			if len(row) < 2 {
				continue
			}
			properties[strings.TrimSpace(row[0])] = strings.TrimSpace(row[1])
		}
	}
	name := properties["Alias"]
	if name == "" || name == "-" {
		name = properties["Model"]
	}
	provider := properties["Provider"]
	if name == "" || name == "-" || provider == "" || provider == "-" {
		return
	}
	summary := fmt.Sprintf("Current Model: %s (Provider: %s)", name, provider)
	fallback := strings.TrimSpace(content.Fallback)
	if fallback == "" {
		content.Fallback = summary
		return
	}
	if !strings.Contains(fallback, summary) {
		content.Fallback = summary + "\n" + fallback
	}
}
