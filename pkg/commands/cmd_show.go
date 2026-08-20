package commands

import (
	"context"
	"fmt"
)

func showCommand() Definition {
	return Definition{
		Name:        "show",
		Description: "Show current configuration",
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
							[]string{"Properti", "Nilai"},
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
				Description: "Show active tools for an MCP server",
				ArgsUsage:   "<server>",
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
				return req.Reply("Model command failed: " + err.Error())
			}
			if content != nil {
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
				[]string{"Properti", "Nilai"},
				[][]string{{"Current Model", name}, {"Provider", provider}},
				fallback,
			),
		)
	}
}
