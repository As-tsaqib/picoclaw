package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/memory"
	"github.com/As-tsaqib/picoclaw/pkg/session"
)

func TestInternalCallbackRouterRejectsUnknownKind(t *testing.T) {
	al := &AgentLoop{}
	_, err := al.handleInternalCallback(context.Background(), bus.InternalCallbackRequest{Kind: "command"})
	require.Error(t, err)
}

func TestPendingSkillStateIsBoundToExactSessionAndSingleUse(t *testing.T) {
	al := &AgentLoop{}
	al.setPendingSkills("session-a", []string{"alpha"})
	al.setPendingSkills("session-b", []string{"beta"})

	assert.Equal(t, "alpha", al.pendingSkillForSession("session-a"))
	assert.Equal(t, "beta", al.pendingSkillForSession("session-b"))

	al.clearPendingSkills("session-a")
	assert.Empty(t, al.pendingSkillForSession("session-a"))
	assert.Equal(t, "beta", al.pendingSkillForSession("session-b"))

	assert.Equal(t, []string{"beta"}, al.takePendingSkills("session-b"))
	assert.Empty(t, al.takePendingSkills("session-b"))
}

func TestCheckpointInteractivePrivateRoutePolicy(t *testing.T) {
	cases := []struct {
		name    string
		inbound *bus.InboundContext
		want    bool
	}{
		{
			name: "direct",
			inbound: &bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "42", ChatType: "direct", SenderID: "42",
			},
			want: true,
		},
		{
			name: "verified private group route",
			inbound: &bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "-1001", ChatType: "group", SenderID: "42",
				PrivateResponse: true, PrivateRouteToken: "verified-route",
			},
			want: true,
		},
		{
			name: "unverified private flag",
			inbound: &bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "-1001", ChatType: "group", SenderID: "42",
				PrivateResponse: true,
			},
			want: false,
		},
		{
			name: "public group",
			inbound: &bus.InboundContext{
				Channel: "telegram", Account: "telegram", ChatID: "-1001", ChatType: "group", SenderID: "42",
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, memoryInteractionRouteIsPrivate(tc.inbound))
		})
	}
}

func TestCheckpointArchiveConfirmationIsExplicitAndKeepsPrivateStateServerSide(t *testing.T) {
	checkpoint := memory.TaskCheckpoint{
		ID:               "cp_private_identifier",
		Title:            "Deploy release",
		Status:           memory.CheckpointStatusSuspended,
		ImportantContext: "private persistence context",
	}
	inbound := &bus.InboundContext{
		Channel: "telegram", Account: "telegram", ChatID: "42", ChatType: "direct", SenderID: "42",
	}
	scope := &session.SessionScope{Version: 1, AgentID: "main", Channel: "telegram", Account: "telegram"}
	content := buildCheckpointArchiveConfirm(
		&AgentInstance{ID: "main"}, checkpoint, "si_v1_bound", scope, inbound,
	)

	require.NotNil(t, content.Interaction)
	assert.NotContains(t, content.FallbackText(), checkpoint.ID)
	assert.NotContains(t, content.FallbackText(), checkpoint.ImportantContext)
	require.Len(t, content.Interaction.Entries, 2)
	assert.Equal(t, "archive_confirm", content.Interaction.Entries[0].Action)
	assert.Equal(t, checkpoint.ID, content.Interaction.Entries[0].Value)
	assert.Equal(t, "detail", content.Interaction.Entries[1].Action)
	assert.Equal(t, checkpoint.ID, content.Interaction.Entries[1].Value)
}
