package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestFilterSkillNamesIsCaseInsensitiveAndBounded(t *testing.T) {
	names := make([]string, 0, 80)
	for i := 0; i < 80; i++ {
		names = append(names, "Tool-Match")
	}
	got := filterSkillNames(names, "mAtCh")
	assert.Len(t, got, skillInteractiveSearchMax)
	assert.Empty(t, filterSkillNames([]string{"alpha", "beta"}, "zzz"))
}

func TestCheckpointInteractiveDetailKeepsRawIDAndImportantContextServerSide(t *testing.T) {
	cp := memory.TaskCheckpoint{
		ID: "cp_secret_raw_id", Title: "Deploy release", Objective: "Ship safely", Status: memory.CheckpointStatusSuspended,
		CurrentStep: "verify CI", NextStep: "deploy", ImportantContext: "private context that must not be rendered",
		CompletedItems: []string{"tests"}, UpdatedAt: time.Now(),
	}
	inbound := &bus.InboundContext{Channel: "telegram", Account: "default", ChatID: "42", ChatType: "direct", SenderID: "42"}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "default"}
	content := buildCheckpointDetail(&AgentInstance{ID: "main"}, cp, "si_v1_bound", scope, inbound)
	require.NotNil(t, content.Interaction)
	assert.Equal(t, "si_v1_bound", content.Interaction.SessionKey)
	assert.NotContains(t, content.FallbackText(), cp.ID)
	assert.NotContains(t, content.FallbackText(), cp.ImportantContext)
	foundRawServerSide := false
	for _, entry := range content.Interaction.Entries {
		if entry.Value == cp.ID {
			foundRawServerSide = true
		}
	}
	assert.True(t, foundRawServerSide)
}

func TestBoundInteractionMenuCarriesExplicitSessionAndNoCallbackData(t *testing.T) {
	inbound := &bus.InboundContext{Channel: "telegram", Account: "default", ChatID: "42", ChatType: "direct", SenderID: "42"}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "default"}
	menu := newBoundInteractionMenu("skill", "main", "si_v1_secret", scope, inbound, 0, 1, "query", "", []bus.InteractionEntry{{
		Label: "1", Action: "detail", Value: "private-skill",
	}})
	assert.Equal(t, "si_v1_secret", menu.SessionKey)
	assert.Equal(t, "query", menu.Query)
	assert.False(t, strings.Contains(menu.Scope, "si_v1_secret"))
}
