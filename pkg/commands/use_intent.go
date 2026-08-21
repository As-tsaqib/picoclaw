package commands

import "strings"

type UseIntentKind string

const (
	UseIntentPicker     UseIntentKind = "picker"
	UseIntentArm        UseIntentKind = "arm"
	UseIntentForcedTurn UseIntentKind = "forced_turn"
	UseIntentClear      UseIntentKind = "clear"
)

type UseIntent struct {
	Kind    UseIntentKind
	Skill   string
	Message string
}

// ParseUseIntent owns the surface grammar for /use. Skill resolution remains
// an agent-domain concern because it depends on the current skill catalog.
func ParseUseIntent(raw string) (UseIntent, bool) {
	name, ok := CommandName(raw)
	if !ok || !strings.EqualFold(name, "use") {
		return UseIntent{}, false
	}
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 1 {
		return UseIntent{Kind: UseIntentPicker}, true
	}
	skill := strings.TrimSpace(parts[1])
	if strings.EqualFold(skill, "clear") || strings.EqualFold(skill, "off") {
		return UseIntent{Kind: UseIntentClear}, true
	}
	if len(parts) == 2 {
		return UseIntent{Kind: UseIntentArm, Skill: skill}, true
	}
	message := strings.TrimSpace(strings.Join(parts[2:], " "))
	if message == "" {
		return UseIntent{Kind: UseIntentArm, Skill: skill}, true
	}
	return UseIntent{Kind: UseIntentForcedTurn, Skill: skill, Message: message}, true
}
