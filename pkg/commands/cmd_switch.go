package commands

import (
	"context"
	"strings"
)

func switchCommand() Definition {
	return Definition{
		Name:        "switch",
		Description: "Deprecated compatibility syntax for model selection",
		Category:    "Models",
		Deprecated:  true,
		Replacement: "/model use <model>",
		Examples:    []string{"/switch model to gpt-5"},
		SubCommands: []SubCommand{
			{
				Name:        "model",
				Description: "Compatibility form for /model use <model>",
				ArgsUsage:   "to <model>",
				Deprecated:  true,
				Replacement: "/model use <model>",
				Examples:    []string{"/switch model to gpt-5"},
				Handler: func(ctx context.Context, req Request, rt *Runtime) error {
					value := afterNthToken(req.Text, 3)
					if !strings.EqualFold(nthToken(req.Text, 2), "to") || strings.TrimSpace(value) == "" {
						return req.Reply("Usage: /switch model to <model>\nDeprecated: use /model use <model> instead.")
					}
					if rt == nil || rt.ModelCommand == nil {
						return req.Reply(unavailableMsg)
					}
					content, err := rt.ModelCommand(ctx, ModelCommandRequest{
						Operation:    "use",
						Argument:     strings.TrimSpace(value),
						LegacySwitch: true,
					})
					if err != nil {
						return req.Reply(UserFacingError(err,
							"Model service is temporarily unavailable. Please try again."))
					}
					if content == nil {
						return req.Reply(unavailableMsg)
					}
					return req.replyStructured(*content)
				},
			},
			{
				Name:        "channel",
				Description: "Deprecated read-only compatibility guidance",
				ArgsUsage:   "[name]",
				Deprecated:  true,
				Replacement: "/check channel <name>",
				Handler: func(_ context.Context, req Request, _ *Runtime) error {
					return req.Reply("/switch channel is deprecated and does not change channel state. Use /check channel <name> for status.")
				},
			},
		},
	}
}
