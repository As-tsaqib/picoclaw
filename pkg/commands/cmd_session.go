package commands

import (
	"context"
	"strings"
)

func sessionCommand() Definition {
	return Definition{
		Name:        "session",
		Description: "List, name, create, and switch conversation sessions",
		Handler:     sessionOperationHandler("list", 1),
		SubCommands: []SubCommand{
			{Name: "list", Description: "List sessions in the current safe scope", Handler: sessionOperationHandler("list", 2)},
			{Name: "current", Description: "Show the active session", Handler: sessionOperationHandler("current", 2)},
			{Name: "new", Description: "Create and activate a named session", ArgsUsage: "[name]", Handler: sessionOperationHandler("new", 2)},
			{Name: "rename", Description: "Rename the active session", ArgsUsage: "<new-name>", Handler: sessionOperationHandler("rename", 2)},
			{Name: "use", Description: "Switch by number or short ID", ArgsUsage: "<number|short-id>", Handler: sessionOperationHandler("use", 2)},
		},
	}
}

func sessionOperationHandler(operation string, argumentToken int) Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.SessionCommand == nil {
			return req.Reply(unavailableMsg)
		}
		argument := ""
		if argumentToken >= 0 {
			argument = strings.TrimSpace(afterNthToken(req.Text, argumentToken))
		}
		if operation == "rename" && argument == "" {
			return req.Reply("Usage: /session rename <new-name>")
		}
		if operation == "use" && argument == "" {
			return req.Reply("Usage: /session use <number|short-id>")
		}
		content, err := rt.SessionCommand(ctx, SessionCommandRequest{
			Operation: operation,
			Argument:  argument,
		})
		if err != nil {
			return req.Reply("Session command failed: " + err.Error())
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}
