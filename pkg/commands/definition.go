package commands

import (
	"fmt"
	"strings"
)

// SubCommand defines a single sub-command within a parent command.
type SubCommand struct {
	Name        string
	Description string
	ArgsUsage   string // optional, e.g. "<session-id>"
	Aliases     []string
	Deprecated  bool
	Replacement string
	Examples    []string
	Handler     Handler
}

// Definition is the single-source metadata and behavior contract for a slash command.
//
// Design notes:
//   - Every channel reads command shape from this type instead of keeping local copies.
//   - Visibility is global: all definitions are considered available to all channels.
//   - Platform menu registration (for example Telegram BotCommand) also derives from this
//     same definition so UI labels and runtime behavior stay aligned.
//   - Discovery metadata lives beside runtime behavior so /help never needs a second catalog.
type Definition struct {
	Name        string
	Description string
	Usage       string // for simple commands; ignored when SubCommands is set
	Aliases     []string
	Category    string
	Deprecated  bool
	Replacement string
	Examples    []string
	SubCommands []SubCommand // optional; when set, Executor routes to sub-command handlers
	Handler     Handler      // for simple commands without sub-commands
}

// EffectiveUsage returns the compact usage string retained for compatibility with
// callers that need one line. Rich help renders SubCommands individually instead.
func (d Definition) EffectiveUsage() string {
	if len(d.SubCommands) == 0 {
		return d.Usage
	}
	names := make([]string, 0, len(d.SubCommands))
	for _, sc := range d.SubCommands {
		name := sc.Name
		if sc.ArgsUsage != "" {
			name += " " + sc.ArgsUsage
		}
		names = append(names, name)
	}
	return fmt.Sprintf("/%s [%s]", d.Name, strings.Join(names, "|"))
}

func (s SubCommand) matches(name string) bool {
	if strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(name)) {
		return true
	}
	for _, alias := range s.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), strings.TrimSpace(name)) {
			return true
		}
	}
	return false
}
