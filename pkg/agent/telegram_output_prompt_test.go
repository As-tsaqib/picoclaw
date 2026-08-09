package agent

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTelegramOutputPromptContributor(t *testing.T) {
	t.Parallel()

	registry := NewPromptRegistry()
	parts, err := registry.Collect(t.Context(), PromptBuildRequest{Channel: " Telegram "})
	require.NoError(t, err)
	require.Len(t, parts, 1)
	assert.Contains(t, parts[0].Content, "Prefer a compact Markdown table")
	assert.Contains(t, parts[0].Content, "|---|---|")

	parts, err = registry.Collect(t.Context(), PromptBuildRequest{Channel: "discord"})
	require.NoError(t, err)
	assert.Empty(t, parts)
}

func TestBuildMessagesAddsFormattingRulesOnlyForTelegram(t *testing.T) {
	t.Parallel()

	cb := NewContextBuilder(t.TempDir())
	telegramMessages := cb.BuildMessagesFromPrompt(PromptBuildRequest{
		CurrentMessage: "show metrics",
		Channel:        "telegram",
		ChatID:         "12345",
	})
	require.NotEmpty(t, telegramMessages)
	require.Equal(t, "system", telegramMessages[0].Role)
	assert.Contains(t, telegramMessages[0].Content, "# Telegram response formatting")

	discordMessages := cb.BuildMessagesFromPrompt(PromptBuildRequest{
		CurrentMessage: "show metrics",
		Channel:        "discord",
		ChatID:         "12345",
	})
	require.NotEmpty(t, discordMessages)
	require.Equal(t, "system", discordMessages[0].Role)
	assert.False(t, strings.Contains(discordMessages[0].Content, "# Telegram response formatting"))
}
