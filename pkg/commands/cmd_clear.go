package commands

import "context"

func clearCommand() Definition {
	return Definition{
		Name:        "clear",
		Description: "Clear history in the current session",
		Usage:       "/clear",
		Aliases:     []string{"reset"},
		Category:    "Sessions",
		Examples:    []string{"/clear", "/reset"},
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			if rt == nil || rt.ClearHistory == nil {
				return req.Reply(unavailableMsg)
			}
			if err := rt.ClearHistory(); err != nil {
				return req.Reply(UserFacingError(err, "Chat history could not be cleared. Please try again."))
			}
			return req.Reply("Chat history cleared!")
		},
	}
}
