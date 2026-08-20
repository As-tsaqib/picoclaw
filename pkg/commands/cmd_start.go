package commands

import "context"

func startCommand() Definition {
	return Definition{
		Name:        "start",
		Description: "Show lightweight PicoClaw onboarding",
		Usage:       "/start",
		Category:    "General",
		Examples:    []string{"/start"},
		Handler: func(_ context.Context, req Request, _ *Runtime) error {
			return req.Reply("Hello! I am PicoClaw 🦞\n\nTry /help to discover commands, /show for current state, /use to choose a skill, or just send a normal message.")
		},
	}
}
