package commands

import (
	"context"
	"strings"
)

func checkpointCommand() Definition {
	return Definition{
		Name:        "checkpoint",
		Description: "List and resume task checkpoints",
		SubCommands: []SubCommand{
			{Name: "list", Description: "List active/suspended checkpoints", Handler: checkpointListHandler},
			{Name: "resume", Description: "Resume a checkpoint", ArgsUsage: "<id>", Handler: checkpointResumeHandler},
			{Name: "forget", Description: "Archive a checkpoint", ArgsUsage: "<id>", Handler: checkpointForgetHandler},
		},
	}
}

func checkpointListHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.CheckpointList == nil {
		return req.Reply(unavailableMsg)
	}
	response, err := rt.CheckpointList()
	if err != nil {
		return req.Reply("Failed to list checkpoints: " + err.Error())
	}
	return req.replyStructured(informationalLinesContent("Checkpoints", response))
}

func checkpointResumeHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.CheckpointResume == nil {
		return req.Reply(unavailableMsg)
	}
	id := strings.TrimSpace(nthToken(req.Text, 2))
	if id == "" {
		return req.Reply("Usage: /checkpoint resume <id>")
	}
	response, err := rt.CheckpointResume(id)
	if err != nil {
		return req.Reply("Failed to resume checkpoint: " + err.Error())
	}
	return req.Reply(response)
}

func checkpointForgetHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.CheckpointForget == nil {
		return req.Reply(unavailableMsg)
	}
	id := strings.TrimSpace(nthToken(req.Text, 2))
	if id == "" {
		return req.Reply("Usage: /checkpoint forget <id>")
	}
	response, err := rt.CheckpointForget(id)
	if err != nil {
		return req.Reply("Failed to forget checkpoint: " + err.Error())
	}
	return req.Reply(response)
}
