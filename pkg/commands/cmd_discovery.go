package commands

import (
	"context"
	"strings"
)

func discoveryDashboardHandler(domain string) Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		if rt == nil || rt.DiscoveryCommand == nil {
			return req.Reply(unavailableMsg)
		}
		content, err := rt.DiscoveryCommand(ctx, DiscoveryCommandRequest{
			Domain:    strings.ToLower(strings.TrimSpace(domain)),
			Operation: "dashboard",
		})
		if err != nil {
			return req.Reply(UserFacingError(err, discoveryDomainLabel(domain)+" service is temporarily unavailable. Please try again."))
		}
		if content == nil {
			return req.Reply(unavailableMsg)
		}
		return req.replyStructured(*content)
	}
}

func discoveryDomainLabel(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "show":
		return "Show"
	case "list":
		return "List"
	case "check":
		return "Check"
	default:
		return "Discovery"
	}
}
