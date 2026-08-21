package commands

import (
	"context"
	"strings"
)

func memoryCommand() Definition {
	return Definition{
		Name:        "memory",
		Description: "Inspect and manage curated durable memory",
		Category:    "Memory",
		Examples:    []string{"/memory", "/memory search project", "/memory pending"},
		Handler:     memoryOperationHandler("dashboard"),
		SubCommands: []SubCommand{
			{
				Name:        "status",
				Description: "Show memory configuration and capacity",
				Handler:     memoryOperationHandler("status"),
			},
			{
				Name:        "profile",
				Description: "Show the compiled private current-user profile",
				Handler:     memoryOperationHandler("profile"),
			},
			{
				Name:        "list",
				Description: "List current workspace/user entries",
				Handler:     memoryOperationHandler("list"),
			},
			{
				Name:        "search",
				Description: "Search current scoped memory",
				ArgsUsage:   "<query>",
				Handler:     memoryOperationHandler("search"),
			},
			{
				Name:        "edit",
				Description: "Replace an entry's content",
				ArgsUsage:   "<id> <content>",
				Handler:     memoryOperationHandler("edit"),
			},
			{
				Name:        "pin",
				Description: "Pin an active entry",
				ArgsUsage:   "<id>",
				Handler:     memoryOperationHandler("pin"),
			},
			{
				Name:        "unpin",
				Description: "Unpin an entry",
				ArgsUsage:   "<id>",
				Handler:     memoryOperationHandler("unpin"),
			},
			{
				Name:        "archive",
				Description: "Archive an entry",
				ArgsUsage:   "<id>",
				Handler:     memoryOperationHandler("archive"),
			},
			{
				Name:        "restore",
				Description: "Restore an archived entry",
				ArgsUsage:   "<id>",
				Handler:     memoryOperationHandler("restore"),
			},
			{
				Name:        "forget",
				Description: "Remove a current-user memory entry",
				ArgsUsage:   "<id>",
				Handler:     memoryOperationHandler("forget"),
			},
			{
				Name:        "pending",
				Description: "List staged memory changes",
				Handler:     memoryOperationHandler("pending"),
			},
			{
				Name:        "approve",
				Description: "Approve one or all staged changes",
				ArgsUsage:   "<id|all>",
				Handler:     memoryOperationHandler("approve"),
			},
			{
				Name:        "reject",
				Description: "Reject one or all staged changes",
				ArgsUsage:   "<id|all>",
				Handler:     memoryOperationHandler("reject"),
			},
			{
				Name:        "review",
				Description: "Run one bounded review of delivered turns",
				Handler:     memoryOperationHandler("review"),
			},
		},
	}
}

func memoryOperationHandler(operation string) Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.MemoryCommand == nil {
			return req.Reply(unavailableMsg)
		}
		semantic, usage := parseMemorySemanticRequest(operation, req.Text)
		if usage != "" {
			return req.Reply(usage)
		}
		content, err := rt.MemoryCommand(ctx, semantic)
		if err != nil {
			return req.Reply(UserFacingError(err, "Memory service is temporarily unavailable. Please try again."))
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}

func parseMemorySemanticRequest(operation, text string) (MemoryCommandRequest, string) {
	request := MemoryCommandRequest{Operation: operation}
	switch operation {
	case "search":
		request.Query = strings.TrimSpace(afterNthToken(text, 2))
		request.Argument = request.Query
		if request.Query == "" {
			return request, "Usage: /memory search <query>"
		}
	case "edit":
		request.ID = strings.TrimSpace(nthToken(text, 2))
		request.Content = strings.TrimSpace(afterNthToken(text, 3))
		if request.ID == "" || request.Content == "" {
			return request, "Usage: /memory edit <id> <content>"
		}
	case "pin", "unpin", "archive", "restore", "forget":
		request.ID = strings.TrimSpace(nthToken(text, 2))
		if request.ID == "" {
			return request, "Usage: /memory " + operation + " <id>"
		}
	case "approve", "reject":
		request.ID = strings.TrimSpace(nthToken(text, 2))
		if request.ID == "" {
			return request, "Usage: /memory " + operation + " <id|all>"
		}
	}
	return request, ""
}

func afterNthToken(input string, n int) string {
	parts := strings.Fields(input)
	if n < 0 || n >= len(parts) {
		return ""
	}
	return strings.Join(parts[n:], " ")
}
