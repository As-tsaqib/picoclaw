package commands

import (
	"context"
	"strings"
)

func memoryCommand() Definition {
	return Definition{
		Name:        "memory",
		Description: "Inspect and manage curated durable memory",
		Handler:     memoryRootHandler,
		SubCommands: []SubCommand{
			{Name: "status", Description: "Show memory configuration and capacity", Handler: memoryStatusHandler},
			{
				Name:        "profile",
				Description: "Show the compiled private current-user profile",
				Handler:     memoryProfileHandler,
			},
			{Name: "list", Description: "List current workspace/user entries", Handler: memoryListHandler},
			{
				Name: "search", Description: "Search current scoped memory",
				ArgsUsage: "<query>", Handler: memorySearchHandler,
			},
			{
				Name: "edit", Description: "Replace an entry's content",
				ArgsUsage: "<id> <content>", Handler: memoryEditHandler,
			},
			{
				Name: "pin", Description: "Pin an active entry",
				ArgsUsage: "<id>", Handler: memoryEntryActionHandler("pin"),
			},
			{
				Name: "unpin", Description: "Unpin an entry",
				ArgsUsage: "<id>", Handler: memoryEntryActionHandler("unpin"),
			},
			{
				Name: "archive", Description: "Archive an entry",
				ArgsUsage: "<id>", Handler: memoryEntryActionHandler("archive"),
			},
			{
				Name: "restore", Description: "Restore an archived entry",
				ArgsUsage: "<id>", Handler: memoryEntryActionHandler("restore"),
			},
			{
				Name: "forget", Description: "Remove a current-user memory entry",
				ArgsUsage: "<id>", Handler: memoryForgetHandler,
			},
			{Name: "pending", Description: "List staged memory changes", Handler: memoryPendingHandler},
			{
				Name: "approve", Description: "Approve one or all staged changes",
				ArgsUsage: "<id|all>", Handler: memoryApproveHandler,
			},
			{
				Name: "reject", Description: "Reject one or all staged changes",
				ArgsUsage: "<id|all>", Handler: memoryRejectHandler,
			},
			{Name: "review", Description: "Run one bounded review of delivered turns", Handler: memoryReviewHandler},
		},
	}
}

func memoryRootHandler(ctx context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryCommand == nil {
		return req.Reply(unavailableMsg)
	}
	content, err := rt.MemoryCommand(ctx, MemoryCommandRequest{
		Operation: "dashboard",
	})
	if err != nil {
		return req.Reply("Failed to open memory dashboard: " + err.Error())
	}
	if content == nil {
		return req.Reply(unavailableMsg)
	}
	return req.replyStructured(*content)
}

func memorySearchHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemorySearch == nil {
		return req.Reply(unavailableMsg)
	}
	query := strings.TrimSpace(afterNthToken(req.Text, 2))
	if query == "" {
		return req.Reply("Usage: /memory search <query>")
	}
	response, err := rt.MemorySearch(query)
	if err != nil {
		return req.Reply("Failed to search memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryEditHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryEdit == nil {
		return req.Reply(unavailableMsg)
	}
	id := strings.TrimSpace(nthToken(req.Text, 2))
	content := strings.TrimSpace(afterNthToken(req.Text, 3))
	if id == "" || content == "" {
		return req.Reply("Usage: /memory edit <id> <content>")
	}
	response, err := rt.MemoryEdit(id, content)
	if err != nil {
		return req.Reply("Failed to edit memory: " + err.Error())
	}
	return req.Reply(response)
}

func memoryEntryActionHandler(action string) Handler {
	return func(_ context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.MemoryEntryAction == nil {
			return req.Reply(unavailableMsg)
		}
		id := strings.TrimSpace(nthToken(req.Text, 2))
		if id == "" {
			return req.Reply("Usage: /memory " + action + " <id>")
		}
		response, err := rt.MemoryEntryAction(action, id)
		if err != nil {
			return req.Reply("Failed to " + action + " memory: " + err.Error())
		}
		return req.Reply(response)
	}
}

func memoryStatusHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryStatus == nil {
		return req.Reply(unavailableMsg)
	}
	response := rt.MemoryStatus()
	return req.replyStructured(keyValueContent("Memory status", response))
}

func memoryProfileHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryProfile == nil {
		return req.Reply(unavailableMsg)
	}
	response, err := rt.MemoryProfile()
	if err != nil {
		return req.Reply("Failed to show user profile: " + err.Error())
	}
	return req.Reply(response)
}

func memoryListHandler(_ context.Context, req Request, rt *Runtime) error {
	if rt == nil || rt.MemoryList == nil {
		return req.Reply(unavailableMsg)
	}
	response, err := rt.MemoryList()
	if err != nil {
		return req.Reply("Failed to list memory: " + err.Error())
	}
	return req.replyStructured(informationalLinesContent("Memory entries", response))
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
	return req.replyStructured(informationalLinesContent("Pending memory", response))
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

func afterNthToken(input string, n int) string {
	parts := strings.Fields(input)
	if n < 0 || n >= len(parts) {
		return ""
	}
	return strings.Join(parts[n:], " ")
}
