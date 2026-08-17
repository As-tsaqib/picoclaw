package commands

import (
	"context"
	"fmt"
	"strings"
)

func switchCommand() Definition {
	return Definition{
		Name:        "switch",
		Description: "Deprecated compatibility command; use /model",
		SubCommands: []SubCommand{
			{
				Name:        "model",
				Description: "Deprecated: switch the current session model",
				ArgsUsage:   "to <name>",
				Handler: func(ctx context.Context, req Request, rt *Runtime) error {
					value := nthToken(req.Text, 3)
					if nthToken(req.Text, 2) != "to" || strings.TrimSpace(value) == "" {
						return req.Reply("Usage: /switch model to <name>")
					}
					if rt != nil && rt.ModelCommand != nil {
						content, err := rt.ModelCommand(
							ctx,
							ModelCommandRequest{Operation: "use", Argument: value},
						)
						if err != nil {
							if strings.Contains(err.Error(), "tidak ditemukan") {
								return req.Reply(fmt.Sprintf("model %q not found in model_list or providers", value))
							}
							return req.Reply(err.Error())
						}
						if content == nil {
							return req.Reply(unavailableMsg)
						}
						content = content.Clone()
						content.Paragraphs = append(
							content.Paragraphs,
							"Tip: /switch model sudah deprecated. Gunakan /model untuk pengelolaan model.",
						)
						if content.Fallback != "" {
							content.Fallback += "\n\nTip: /switch model sudah deprecated. Gunakan /model."
						}
						return req.replyStructured(*content)
					}
					if rt == nil || rt.SwitchModel == nil {
						return req.Reply(unavailableMsg)
					}
					oldModel, err := rt.SwitchModel(value)
					if err != nil {
						return req.Reply(err.Error())
					}
					return req.Reply(fmt.Sprintf("Switched model from %s to %s", oldModel, value))
				},
			},
			{
				Name:        "channel",
				Description: "Moved to /check channel",
				Handler: func(_ context.Context, req Request, _ *Runtime) error {
					return req.Reply("This command has moved. Please use: /check channel <name>")
				},
			},
		},
	}
}
