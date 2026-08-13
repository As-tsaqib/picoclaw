package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
	"github.com/As-tsaqib/picoclaw/pkg/config"
)

const testMarkdownTable = "**Metrics**\n\n" +
	"- Scope: system\n" +
	"- Source: `picoclaw status`\n\n" +
	"| Name | Value |\n" +
	"|------|------:|\n" +
	"| CPU  | 42%   |"

func TestSendMarkdownTableUsesNativeRichMessage(t *testing.T) {
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			require.Contains(t, url, "sendRichMessage")
			return successResponseWithMessageID(t, 91), nil
		},
	}
	ch := newTestChannel(t, caller)

	ids, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:           "-1001234567890/42",
		ReplyToMessageID: "77",
		Content:          testMarkdownTable,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"91"}, ids)
	require.Len(t, caller.calls, 1)

	var payload struct {
		ChatID          int64 `json:"chat_id"`
		MessageThreadID int   `json:"message_thread_id"`
		RichMessage     struct {
			Markdown string `json:"markdown"`
		} `json:"rich_message"`
		ReplyParameters struct {
			MessageID int `json:"message_id"`
		} `json:"reply_parameters"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, int64(-1001234567890), payload.ChatID)
	assert.Equal(t, 42, payload.MessageThreadID)
	assert.Equal(t, testMarkdownTable, payload.RichMessage.Markdown)
	assert.Equal(t, 77, payload.ReplyParameters.MessageID)
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), `"parse_mode"`)
	assert.NotContains(t, string(caller.calls[0].Data.BodyRaw), `"entities"`)
}

func TestSendHTMLTableUsesNativeRichMessage(t *testing.T) {
	content := `<h2>Metrics</h2><table><tr><th>Name</th><th>Value</th></tr><tr><td>CPU</td><td>42%</td></tr></table>`
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			require.Contains(t, url, "sendRichMessage")
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)
	ch.tgCfg.UseMarkdownV2 = true

	_, err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "12345", Content: content})
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)

	var payload struct {
		RichMessage telego.InputRichMessage `json:"rich_message"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, content, payload.RichMessage.Markdown)
}

func TestSendTableInsideCodeBlockUsesRegularMessage(t *testing.T) {
	content := "```markdown\n| Name | Value |\n|---|---|\n| CPU | 42% |\n```"
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			assert.Contains(t, url, "sendMessage")
			assert.NotContains(t, url, "sendRichMessage")
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)

	_, err := ch.Send(context.Background(), bus.OutboundMessage{ChatID: "12345", Content: content})
	require.NoError(t, err)
	require.Len(t, caller.calls, 1)
}

func TestSendNativeTableFailureFallsBackToPreformattedMessage(t *testing.T) {
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			if strings.Contains(url, "sendRichMessage") {
				return nil, errors.New("Bad Request: rich messages are unsupported")
			}
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)

	ids, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "12345",
		Content: testMarkdownTable,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"1"}, ids)
	require.Len(t, caller.calls, 2)
	assert.Contains(t, caller.calls[0].URL, "sendRichMessage")
	assert.Contains(t, caller.calls[1].URL, "sendMessage")

	var payload struct {
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &payload))
	assert.Equal(t, telego.ModeHTML, payload.ParseMode)
	assert.Contains(t, payload.Text, "<pre><code>")
	assert.Contains(t, payload.Text, "| Name")
	assert.NotContains(t, payload.Text, "|------|------:|")
}

func TestSendTableFallbackEndsWithAlignedPlainText(t *testing.T) {
	call := 0
	caller := &stubCaller{
		callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
			call++
			if call < 3 {
				return nil, errors.New("Bad Request: rejected")
			}
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)

	_, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "12345",
		Content: testMarkdownTable,
	})
	require.NoError(t, err)
	require.Len(t, caller.calls, 3)

	var payload struct {
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[2].Data.BodyRaw, &payload))
	assert.Empty(t, payload.ParseMode)
	assert.NotContains(t, payload.Text, "```")
	assert.Contains(t, payload.Text, "| Name")
	assert.NotContains(t, payload.Text, "|------|------:|")
}

func TestSendTableFallbackHonorsMarkdownV2Mode(t *testing.T) {
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			if strings.Contains(url, "sendRichMessage") {
				return nil, errors.New("Bad Request: rich messages are unsupported")
			}
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)
	ch.tgCfg.UseMarkdownV2 = true

	_, err := ch.Send(context.Background(), bus.OutboundMessage{
		ChatID:  "12345",
		Content: testMarkdownTable,
	})
	require.NoError(t, err)
	require.Len(t, caller.calls, 2)

	var payload struct {
		Text      string `json:"text"`
		ParseMode string `json:"parse_mode"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &payload))
	assert.Equal(t, telego.ModeMarkdownV2, payload.ParseMode)
	assert.Contains(t, payload.Text, "```")
	assert.Contains(t, payload.Text, "| Name")
}

func TestEditMessageTableUsesNativeRichMessage(t *testing.T) {
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			require.Contains(t, url, "editMessageText")
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)

	require.NoError(t, ch.EditMessage(context.Background(), "12345", "88", testMarkdownTable))
	require.Len(t, caller.calls, 1)

	var payload struct {
		Text        string                   `json:"text"`
		RichMessage *telego.InputRichMessage `json:"rich_message"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Empty(t, payload.Text)
	require.NotNil(t, payload.RichMessage)
	assert.Equal(t, testMarkdownTable, payload.RichMessage.Markdown)
}

func TestEditMessageNativeTableFailureFallsBackToPreformatted(t *testing.T) {
	call := 0
	caller := &stubCaller{
		callFn: func(_ context.Context, _ string, _ *ta.RequestData) (*ta.Response, error) {
			call++
			if call == 1 {
				return nil, errors.New("Bad Request: rich messages are unsupported")
			}
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)

	require.NoError(t, ch.EditMessage(context.Background(), "12345", "88", testMarkdownTable))
	require.Len(t, caller.calls, 2)

	var payload struct {
		Text        string                   `json:"text"`
		ParseMode   string                   `json:"parse_mode"`
		RichMessage *telego.InputRichMessage `json:"rich_message"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[1].Data.BodyRaw, &payload))
	assert.Nil(t, payload.RichMessage)
	assert.Equal(t, telego.ModeHTML, payload.ParseMode)
	assert.Contains(t, payload.Text, "<pre><code>")
}

func TestStreamFinalizeTableUsesNativeRichMessage(t *testing.T) {
	caller := &stubCaller{
		callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			require.Contains(t, url, "sendRichMessage")
			return successResponse(t), nil
		},
	}
	ch := newTestChannel(t, caller)
	ch.tgCfg.Streaming = config.StreamingConfig{Enabled: true}

	streamer, err := ch.BeginStream(context.Background(), "12345")
	require.NoError(t, err)
	require.NoError(t, streamer.Finalize(context.Background(), testMarkdownTable))
	require.Len(t, caller.calls, 1)

	var payload struct {
		RichMessage telego.InputRichMessage `json:"rich_message"`
	}
	require.NoError(t, json.Unmarshal(caller.calls[0].Data.BodyRaw, &payload))
	assert.Equal(t, testMarkdownTable, payload.RichMessage.Markdown)
}
