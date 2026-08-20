package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func listCommand() Definition {
	return Definition{
		Name:        "list",
		Description: "List resources",
		Handler:     discoveryDashboardHandler("list"),
		SubCommands: []SubCommand{
			{
				Name:        "models",
				Description: "List configured models",
				Handler:     listModelsHandler(),
			},
			{
				Name:        "channels",
				Description: "List enabled channels",
				Handler: func(_ context.Context, req Request, rt *Runtime) error {
					if rt == nil || rt.GetEnabledChannels == nil {
						return req.Reply(unavailableMsg)
					}
					items := append([]string(nil), rt.GetEnabledChannels()...)
					sort.SliceStable(items, func(i, j int) bool {
						left, right := strings.ToLower(items[i]), strings.ToLower(items[j])
						if left == right {
							return items[i] < items[j]
						}
						return left < right
					})
					return req.replyStructured(numberedListContent(
						"Channels",
						"Channel",
						items,
						fmt.Sprintf("Enabled channels: %v", items),
					))
				},
			},
			{
				Name:        "agents",
				Description: "List registered agents",
				Handler:     agentsHandler(),
			},
			{
				Name:        "skills",
				Description: "List installed skills",
				Handler:     listSkillsHandler(),
			},
			{
				Name:        "mcp",
				Description: "List configured MCP servers",
				Handler:     listMCPServersHandler(),
			},
		},
	}
}

func listModelsHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		// Delegate to the model catalog/domain so this never collapses to only
		// the active model. The legacy source is retained only for minimal runtimes.
		if rt != nil && rt.ModelCommand != nil {
			content, err := rt.ModelCommand(ctx, ModelCommandRequest{Operation: "list"})
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
		fallback := fmt.Sprintf("Configured Model: %s (Provider: %s)", name, provider)
		return req.replyStructured(
			tableContent(
				"Models",
				[]string{"No", "Provider", "Model"},
				[][]string{{"1", provider, name}},
				fallback,
			),
		)
	}
}

func listSkillsHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		// Use the same picker/catalog semantic as /use. This preserves its
		// deterministic ordering, bounded pagination, and secure interaction state.
		if rt != nil && rt.SkillCommand != nil {
			content, err := rt.SkillCommand(ctx, SkillCommandRequest{Operation: "dashboard"})
			if err != nil {
				return req.Reply("Skill command failed: " + err.Error())
			}
			if content != nil {
				return req.replyStructured(*content)
			}
		}
		if rt == nil || rt.ListSkillNames == nil {
			return req.Reply(unavailableMsg)
		}
		items := rt.ListSkillNames()
		return req.replyStructured(numberedListContent(
			"Skills",
			"Skill",
			items,
			fmt.Sprintf("Installed skills: %v", items),
		))
	}
}
