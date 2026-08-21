package commands

import (
	"context"
	"strings"
)

func checkpointCommand() Definition {
	return Definition{
		Name:        "checkpoint",
		Description: "List, resume, or archive durable task checkpoints",
		Category:    "Checkpoints",
		Examples:    []string{"/checkpoint", "/checkpoint archive cp-1"},
		Handler:     checkpointOperationHandler("dashboard", false, ""),
		SubCommands: []SubCommand{
			{
				Name:        "list",
				Description: "List task checkpoints",
				Handler:     checkpointOperationHandler("list", false, ""),
			},
			{
				Name:        "resume",
				Description: "Resume a checkpoint",
				ArgsUsage:   "<id>",
				Handler:     checkpointOperationHandler("resume", true, "/checkpoint resume <id>"),
			},
			{
				Name:        "archive",
				Description: "Archive a checkpoint",
				ArgsUsage:   "<id>",
				Examples:    []string{"/checkpoint archive cp-1"},
				Handler:     checkpointOperationHandler("archive", true, "/checkpoint archive <id>"),
			},
			{
				Name:        "forget",
				Description: "Compatibility alias for archive",
				ArgsUsage:   "<id>",
				Deprecated:  true,
				Replacement: "/checkpoint archive <id>",
				Handler:     checkpointOperationHandler("archive", true, "/checkpoint forget <id>"),
			},
		},
	}
}

func checkpointOperationHandler(operation string, requiresID bool, usage string) Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.CheckpointCommand == nil {
			return req.Reply(unavailableMsg)
		}
		id := ""
		if requiresID {
			id = strings.TrimSpace(nthToken(req.Text, 2))
			if id == "" {
				return req.Reply("Usage: " + usage)
			}
		}
		content, err := rt.CheckpointCommand(ctx, CheckpointCommandRequest{Operation: operation, ID: id})
		if err != nil {
			return req.Reply(UserFacingError(err, "Checkpoint service is temporarily unavailable. Please try again."))
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}
