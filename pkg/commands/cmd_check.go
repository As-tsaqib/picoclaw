package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func checkCommand() Definition {
	return Definition{
		Name:        "check",
		Description: "Check resource availability",
		SubCommands: []SubCommand{
			{
				Name:        "channel",
				Description: "Check whether a channel is enabled and available",
				ArgsUsage:   "<name>",
				Handler:     checkChannelHandler(),
			},
		},
	}
}

func checkChannelHandler() Handler {
	return func(ctx context.Context, req Request, rt *Runtime) error {
		value := strings.TrimSpace(nthToken(req.Text, 2))
		if value == "" {
			return req.Reply("Usage: /check channel <name>")
		}
		if rt == nil {
			return req.Reply(unavailableMsg)
		}
		if rt.DiscoveryCommand != nil {
			content, err := rt.DiscoveryCommand(ctx, DiscoveryCommandRequest{
				Domain:    "check",
				Operation: "channel",
				Argument:  value,
			})
			if err != nil {
				return req.Reply("Channel check failed: " + err.Error())
			}
			if content != nil {
				return req.replyStructured(*content)
			}
		}
		// Compatibility for narrow runtimes: still use the explicit read-only
		// primitive. Never call SwitchChannel to answer a status question.
		if rt.CheckChannel == nil {
			return req.Reply(unavailableMsg)
		}
		status, err := rt.CheckChannel(value)
		if err != nil {
			return req.Reply("Channel check failed: " + err.Error())
		}
		return req.replyStructured(channelStatusContent(status))
	}
}

func channelStatusContent(status ChannelStatus) bus.StructuredContent {
	rows := [][]string{
		{"Channel", status.Name},
		{"Enabled", yesNo(status.Enabled)},
		{"Available", yesNo(status.Available)},
	}
	if reason := strings.TrimSpace(status.Reason); reason != "" {
		rows = append(rows, []string{"Reason", reason})
	}
	fallback := fmt.Sprintf(
		"Channel Status\nChannel: %s\nEnabled: %s\nAvailable: %s",
		status.Name,
		yesNo(status.Enabled),
		yesNo(status.Available),
	)
	if reason := strings.TrimSpace(status.Reason); reason != "" {
		fallback += "\nReason: " + reason
	}
	return tableContent("Channel Status", []string{"Properti", "Nilai"}, rows, fallback)
}
