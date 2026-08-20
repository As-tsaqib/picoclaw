package commands

import (
	"context"
	"strings"
)

func checkpointCommand() Definition {
	return Definition{
		Name:        "checkpoint",
		Description: "List, resume, or archive durable task checkpoints",
		Handler:     checkpointOperationHandler("dashboard", false),
		SubCommands: []SubCommand{
			{Name: "list", Description: "List task checkpoints", Handler: checkpointOperationHandler("list", false)},
			{
				Name:        "resume",
				Description: "Resume a checkpoint",
				ArgsUsage:   "<id>",
				Handler:     checkpointOperationHandler("resume", true),
			},
			{
				Name:        "forget",
				Description: "Archive a checkpoint",
				ArgsUsage:   "<id>",
				Handler:     checkpointOperationHandler("archive", true),
			},
		},
	}
}

func checkpointOperationHandler(operation string, requiresID bool) Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.CheckpointCommand == nil {
			return req.Reply(unavailableMsg)
		}
		id := ""
		if requiresID {
			id = strings.TrimSpace(nthToken(req.Text, 2))
			if id == "" {
				if operation == "resume" {
					return req.Reply("Usage: /checkpoint resume <id>")
				}
				return req.Reply("Usage: /checkpoint forget <id>")
			}
		}
		content, err := rt.CheckpointCommand(ctx, CheckpointCommandRequest{Operation: operation, ID: id})
		if err != nil {
			return req.Reply("Checkpoint command failed: " + err.Error())
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}
