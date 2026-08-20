package commands

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

func helpCommand() Definition {
	return Definition{
		Name:        "help",
		Description: "Show command help from the canonical registry",
		Usage:       "/help [command]",
		Category:    "General",
		Examples:    []string{"/help", "/help model"},
		Handler: func(_ context.Context, req Request, rt *Runtime) error {
			defs := commandDefinitions(rt)
			args := strings.Fields(req.Text)
			if len(args) > 1 {
				target := strings.TrimPrefix(args[1], "/")
				if def, ok := findHelpDefinition(defs, target); ok {
					return req.Reply(formatCommandHelp(def))
				}
				return req.Reply(fmt.Sprintf("Command /%s not found. Use /help to see available commands.", target))
			}

			fallback := formatHelpMessage(defs)
			rows := make([][]string, 0, len(defs))
			for _, def := range sortedHelpDefinitions(defs) {
				detail := strings.TrimSpace(def.Description)
				if detail == "" {
					detail = "No description"
				}
				if def.Deprecated && strings.TrimSpace(def.Replacement) != "" {
					detail += " (deprecated; use " + def.Replacement + ")"
				}
				rows = append(rows, []string{"/" + def.Name, detail})
			}
			return req.replyStructured(tableContent("Commands", inventoryHeaderColumns(), rows, fallback))
		},
	}
}

func commandDefinitions(rt *Runtime) []Definition {
	if rt != nil && rt.ListDefinitions != nil {
		return rt.ListDefinitions()
	}
	return BuiltinDefinitions()
}

func findHelpDefinition(defs []Definition, target string) (Definition, bool) {
	target = strings.TrimSpace(target)
	for _, def := range defs {
		if strings.EqualFold(def.Name, target) {
			return def, true
		}
		for _, alias := range def.Aliases {
			if strings.EqualFold(strings.TrimSpace(alias), target) {
				return def, true
			}
		}
	}
	return Definition{}, false
}

func sortedHelpDefinitions(defs []Definition) []Definition {
	out := append([]Definition(nil), defs...)
	sort.SliceStable(out, func(i, j int) bool {
		ci := helpCategory(out[i])
		cj := helpCategory(out[j])
		if ci != cj {
			return ci < cj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func helpCategory(def Definition) string {
	category := strings.TrimSpace(def.Category)
	if category == "" {
		return "General"
	}
	return category
}

func formatHelpMessage(defs []Definition) string {
	if len(defs) == 0 {
		return "No commands available."
	}
	var lines []string
	currentCategory := ""
	for _, def := range sortedHelpDefinitions(defs) {
		category := helpCategory(def)
		if category != currentCategory {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, category+":")
			currentCategory = category
		}
		detail := strings.TrimSpace(def.Description)
		if detail == "" {
			detail = "No description"
		}
		line := "/" + def.Name + " — " + detail
		if len(def.Aliases) > 0 {
			line += " (aliases: /" + strings.Join(def.Aliases, ", /") + ")"
		}
		if def.Deprecated {
			line += " [deprecated"
			if replacement := strings.TrimSpace(def.Replacement); replacement != "" {
				line += "; use " + replacement
			}
			line += "]"
		}
		lines = append(lines, line)
	}
	lines = append(lines, "", "Use /help <command> for subcommands and examples.")
	return strings.Join(lines, "\n")
}

func formatCommandHelp(def Definition) string {
	lines := []string{"/" + def.Name + " — " + strings.TrimSpace(def.Description)}
	if len(def.Aliases) > 0 {
		lines = append(lines, "Aliases: /"+strings.Join(def.Aliases, ", /"))
	}
	if def.Deprecated {
		line := "Deprecated."
		if replacement := strings.TrimSpace(def.Replacement); replacement != "" {
			line += " Use " + replacement + "."
		}
		lines = append(lines, line)
	}
	if len(def.SubCommands) == 0 {
		usage := strings.TrimSpace(def.Usage)
		if usage == "" {
			usage = "/" + def.Name
		}
		lines = append(lines, "", "Usage:", "  "+usage)
	} else {
		lines = append(lines, "", "Subcommands:")
		for _, sub := range def.SubCommands {
			usage := "/" + def.Name + " " + sub.Name
			if strings.TrimSpace(sub.ArgsUsage) != "" {
				usage += " " + strings.TrimSpace(sub.ArgsUsage)
			}
			detail := strings.TrimSpace(sub.Description)
			if detail == "" {
				detail = "No description"
			}
			line := "  " + usage + " — " + detail
			if len(sub.Aliases) > 0 {
				line += " (aliases: " + strings.Join(sub.Aliases, ", ") + ")"
			}
			if sub.Deprecated {
				line += " [deprecated"
				if replacement := strings.TrimSpace(sub.Replacement); replacement != "" {
					line += "; use " + replacement
				}
				line += "]"
			}
			lines = append(lines, line)
		}
	}
	examples := append([]string(nil), def.Examples...)
	for _, sub := range def.SubCommands {
		examples = append(examples, sub.Examples...)
	}
	if len(examples) > 0 {
		lines = append(lines, "", "Examples:")
		for _, example := range examples {
			if example = strings.TrimSpace(example); example != "" {
				lines = append(lines, "  "+example)
			}
		}
	}
	return strings.Join(lines, "\n")
}
