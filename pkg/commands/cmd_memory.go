package commands

import (
	"context"
	"strings"
)

func memoryCommand() Definition {
	return Definition{
		Name:        "memory",
		Description: "Inspect and manage curated durable memory",
		SubCommands: []SubCommand{
			{Name: "status", Description: "Show memory configuration and capacity", Handler: memoryStatusHandler},
			{Name: "list", Description: "List current workspace/user entries", Handler: memoryListHandler},
			{Name: "forget", Description: "Remove a current-user memory entry", ArgsUsage: "<id>", Handler: memoryForgetHandler},
			{Name: "pending", Description: "List staged memory changes", Handler: memoryPendingHandler},
			{Name: "approve", Description: "Approve one or all staged changes", ArgsUsage: "<id|all>", Handler: memoryApproveHandler},
			{Name: "reject", Description: "Reject one or all staged changes", ArgsUsage: "<id|all>", Handler: memoryRejectHandler},
			{Name: "review", Description: "Run one bounded review of delivered turns", Handler: memoryReviewHandler},
		},
	}
}

func memoryStatusHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryStatus == nil {
		return req.Reply(unavailableMsg)
	}
	return req.Reply(rt.MemoryStatus())
}

func memoryListHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryList == nil {
		return req.Reply(unavailableMsg)
	}
	response, err := rt.MemoryList()
	if err != nil {
		return req.Reply("Failed to list memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryForgetHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryForget == nil {
		return req.Reply(unavailableMsg)
	}
	id := strings.TrimSpace(nthToken(req.Text, 2))
	if id == "" {
		return req.Reply("Usage: /memory forget <id>")
	}
	response, err := rt.MemoryForget(id)
	if err != nil {
		return req.Reply("Failed to forget memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryPendingHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryPending == nil {
		return req.Reply(unavailableMsg)
	}
	response, err := rt.MemoryPending()
	if err != nil {
		return req.Reply("Failed to list pending memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryApproveHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryApprove == nil {
		return req.Reply(unavailableMsg)
	}
	id := strings.TrimSpace(nthToken(req.Text, 2))
	if id == "" {
		return req.Reply("Usage: /memory approve <id|all>")
	}
	response, err := rt.MemoryApprove(id)
	if err != nil {
		return req.Reply("Failed to approve memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryRejectHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryReject == nil {
		return req.Reply(unavailableMsg)
	}
	id := strings.TrimSpace(nthToken(req.Text, 2))
	if id == "" {
		return req.Reply("Usage: /memory reject <id|all>")
	}
	response, err := rt.MemoryReject(id)
	if err != nil {
		return req.Reply("Failed to reject memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryReviewHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryReview == nil {
		return req.Reply(unavailableMsg)
	}
	response, err := rt.MemoryReview(ctx)
	if err != nil {
		return req.Reply("Failed to start memory review: " + err.Error())
	}
	return req.Reply(response)
}
