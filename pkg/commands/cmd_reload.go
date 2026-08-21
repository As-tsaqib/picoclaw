package commands

import "context"

func reloadCommand() Definition {
	return Definition{
		Name:        "reload",
		Description: "Reload the configuration file",
		Usage:       "/reload",
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			if rt == nil || rt.ReloadConfig == nil {
				return req.Reply(unavailableMsg)
			}
			if err := rt.ReloadConfig(); err != nil {
				return req.Reply(UserFacingError(err, "Configuration could not be reloaded. Please try again."))
			}
			return req.Reply("Config reload triggered!")
		},
	}
}
