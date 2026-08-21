package commands

import (
	"context"
	"strings"
)

func modelCommand() Definition {
	return Definition{
		Name:        "model",
		Description: "View and switch the model for this session",
		Category:    "Models",
		Examples:    []string{"/model", "/model use gpt-5", "/model search coding"},
		Handler:     modelOperationHandler("dashboard", 1),
		SubCommands: []SubCommand{
			{
				Name:        "current",
				Description: "Show the active model for this session",
				Handler:     modelOperationHandler("current", 2),
			},
			{Name: "list", Description: "List configured models", Handler: modelOperationHandler("list", 2)},
			{
				Name:        "use",
				Description: "Use a configured or discovered model in this session",
				ArgsUsage:   "<alias|model>",
				Examples:    []string{"/model use gpt-5"},
				Handler:     modelOperationHandler("use", 2),
			},
			{
				Name:        "default",
				Description: "Return this session to the agent default model",
				Handler:     modelOperationHandler("default", 2),
			},
			{
				Name:        "search",
				Description: "Search configured and cached available models",
				ArgsUsage:   "<query>",
				Handler:     modelOperationHandler("search", 2),
			},
		},
	}
}

func modelOperationHandler(operation string, argumentToken int) Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.ModelCommand == nil {
			return req.Reply(unavailableMsg)
		}
		argument := ""
		if argumentToken >= 0 {
			argument = strings.TrimSpace(afterNthToken(req.Text, argumentToken))
		}
		if (operation == "use" || operation == "search") && argument == "" {
			return req.Reply(
				"Usage: /model " + operation + " <" + map[string]string{"use": "alias|model", "search": "query"}[operation] + ">",
			)
		}
		content, err := rt.ModelCommand(ctx, ModelCommandRequest{Operation: operation, Argument: argument})
		if err != nil {
			return req.Reply(UserFacingError(err, "Model service is temporarily unavailable. Please try again."))
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}
