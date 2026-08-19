package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestPreferredRichStreamUsesStableDraftAndPersistentFinal(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendRichMessageDraft") || strings.Contains(url, "sendMessageDraft") {
			return &ta.Response{Ok: true, Result: []byte("true")}, nil
		}
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	ch.tgCfg.Streaming = config.StreamingConfig{Enabled: true}

	streamer, err := ch.BeginPreferredStreamForSession(context.Background(), "-1001234567890/42", "session-a")
	require.NoError(t, err)
	require.NoError(t, streamer.Update(context.Background(), "## Heading\n\n**partial**"))
	require.NoError(t, streamer.Finalize(context.Background(), "## Heading\n\n**complete**"))

	require.Len(t, caller.calls, 3)
	assert.Contains(t, caller.calls[0].URL, "sendRichMessageDraft")
	assert.Contains(t, caller.calls[1].URL, "sendRichMessage")
	assert.NotContains(t, caller.calls[1].URL, "Draft")
	assert.Contains(t, caller.calls[2].URL, "sendMessageDraft")

	var draft struct {
		ChatID          int64 `json:"chat_id"`
		MessageThreadID int   `json:"message_thread_id"`
		DraftID         int   `json:"draft_id"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &draft))
	assert.Equal(t, int64(-1001234567890), draft.ChatID)
	assert.Equal(t, 42, draft.MessageThreadID)
	assert.NotZero(t, draft.DraftID)

	var clearedDraft struct {
		DraftID int    `json:"draft_id"`
		Text    string `json:"text"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[2].Data.BodyRaw, &clearedDraft))
	assert.Equal(t, draft.DraftID, clearedDraft.DraftID)
	assert.Equal(t, " ", clearedDraft.Text)
}

func TestPreferredRichStreamFallsBackToTextDraftButKeepsRichFinal(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		switch {
		case strings.Contains(url, "sendRichMessageDraft"):
			return nil, errors.New("Bad Request: method not found")
		case strings.Contains(url, "sendMessageDraft"):
			return &ta.Response{Ok: true, Result: []byte("true")}, nil
		default:
			return successResponse(t), nil
		}
	}}
	ch := newTestChannel(t, caller)
	ch.tgCfg.Streaming = config.StreamingConfig{Enabled: true}

	streamer, err := ch.BeginPreferredStreamForSession(context.Background(), "12345", "session-a")
	require.NoError(t, err)
	require.NoError(t, streamer.Update(context.Background(), "## Heading\npartial"))
	require.NoError(t, streamer.Finalize(context.Background(), "## Heading\ncomplete"))

	require.GreaterOrEqual(t, len(caller.calls), 4)
	assert.Contains(t, caller.calls[0].URL, "sendRichMessageDraft")
	assert.Contains(t, caller.calls[1].URL, "sendMessageDraft")
	assert.Contains(t, caller.calls[2].URL, "sendRichMessage")
	assert.True(t, capability.GlobalNegativeCache.IsDowngraded(
		"telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMessageStreamRich,
	))
	assert.False(t, capability.GlobalNegativeCache.IsDowngraded(
		"telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMessageStructuredRich,
	))
}

func TestPreferredRichStreamDraftFailuresNeverPreventFinalDelivery(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "Draft") {
			return nil, errors.New("temporary draft failure")
		}
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	ch.tgCfg.Streaming = config.StreamingConfig{Enabled: true}

	streamer, err := ch.BeginPreferredStreamForSession(context.Background(), "12345", "session-a")
	require.NoError(t, err)
	require.NoError(t, streamer.Update(context.Background(), "## Heading\npartial"))
	require.NoError(t, streamer.Finalize(context.Background(), "## Heading\ncomplete"))

	foundFinal := false
	for _, call := range caller.calls {
		if strings.Contains(call.URL, "sendRichMessage") && !strings.Contains(call.URL, "Draft") {
			foundFinal = true
		}
	}
	assert.True(t, foundFinal, "persistent final send is mandatory after draft failure")
	assert.False(t, capability.GlobalNegativeCache.IsDowngraded(
		"telegram", ch.Name(), ch.tgCfg.BaseURL, capability.FeatureMessageStreamRich,
	), "transient draft failures must not become unsupported capability evidence")
}

func TestPreferredRichStreamPlainTextPreservesTextDraft(t *testing.T) {
	capability.GlobalNegativeCache.Clear()
	caller := &stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendMessageDraft") {
			return &ta.Response{Ok: true, Result: []byte("true")}, nil
		}
		return successResponse(t), nil
	}}
	ch := newTestChannel(t, caller)
	ch.tgCfg.Streaming = config.StreamingConfig{Enabled: true}

	streamer, err := ch.BeginPreferredStreamForSession(context.Background(), "12345", "session-a")
	require.NoError(t, err)
	require.NoError(t, streamer.Update(context.Background(), "plain partial text"))
	require.NoError(t, streamer.Finalize(context.Background(), "plain final text"))

	require.NotEmpty(t, caller.calls)
	assert.Contains(t, caller.calls[0].URL, "sendMessageDraft")
	assert.NotContains(t, caller.calls[0].URL, "sendRichMessageDraft")
}

func TestRichStreamCandidate(t *testing.T) {
	for _, value := range []string{"## heading", "**bold**", "- item", "> quote", "[link](https://example.com)"} {
		assert.True(t, telegramRichStreamCandidate(value), value)
	}
	assert.False(t, telegramRichStreamCandidate("plain text"))
}
