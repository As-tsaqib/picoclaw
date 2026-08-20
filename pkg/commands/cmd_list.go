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
		// A model inventory must come from the mature model catalog domain.
		// Falling back to GetModelInfo would silently collapse /list models back
		// to the single active model, which is semantically false for discovery.
		if rt == nil || rt.ModelCommand == nil {
			return req.Reply(unavailableMsg)
		}
		content, err := rt.ModelCommand(ctx, ModelCommandRequest{Operation: "list"})
		if err != nil {
			return req.Reply(unavailableMsg)
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}

func listSkillsHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		// Use the same picker/catalog semantic as /use. This preserves its
		// deterministic ordering, bounded pagination, and secure interaction state.
		if rt != nil && rt.SkillCommand != nil {
			content, err := rt.SkillCommand(ctx, SkillCommandRequest{Operation: "dashboard"})
			if err != nil {
				return req.Reply(unavailableMsg)
			}
			if content != nil {
				return req.replyStructured(*content)
			}
		}
		if rt == nil || rt.ListSkillNames == nil {
			return req.Reply(unavailableMsg)
		}
		items := append([]string(nil), rt.ListSkillNames()...)
		sort.SliceStable(items, func(i, j int) bool {
			left, right := strings.ToLower(items[i]), strings.ToLower(items[j])
			if left == right {
				return items[i] < items[j]
			}
			return left < right
		})
		return req.replyStructured(numberedListContent(
			"Skills",
			"Skill",
			items,
			fmt.Sprintf("Installed skills: %v", items),
		))
	}
}
