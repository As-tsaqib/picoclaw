package commands

import (
	"context"
	"fmt"
	"strings"
)

func helpCommand() Definition {
	return Definition{
		Name:        "help",
		Description: "Show this help message",
		Usage:       "/help",
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			var defs []Definition
			if rt != nil && rt.ListDefinitions != nil {
				defs = rt.ListDefinitions()
			} else {
				defs = BuiltinDefinitions()
			}
			fallback := formatHelpMessage(defs)
			rows := make([][]string, 0, len(defs))
			for _, def := range defs {
				usage := def.EffectiveUsage()
				if usage == "" {
					usage = "/" + def.Name
				}
				description := def.Description
				if description == "" {
					description = "No description"
				}
				rows = append(rows, []string{usage, description})
			}
			return req.replyStructured(tableContent("Perintah", []string{"Command", "Deskripsi"}, rows, fallback))
		},
	}
}

func formatHelpMessage(defs []Definition) string {
	if len(defs) == 0 {
		return "No commands available."
	}

	lines := make([]string, 0, len(defs))
	for _, def := range defs {
		usage := def.EffectiveUsage()
		if usage == "" {
			usage = "/" + def.Name
		}
		desc := def.Description
		if desc == "" {
			desc = "No description"
		}
		lines = append(lines, fmt.Sprintf("%s - %s", usage, desc))
	}
	return strings.Join(lines, "\n")
}
