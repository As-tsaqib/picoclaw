package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mymmrac/telego"
	ta "github.com/mymmrac/telego/telegoapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

func newTestPollChannel(
	t *testing.T,
	callFn func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error),
) *TelegramChannel {
	t.Helper()
	caller := &stubCaller{callFn: callFn}
	ch := newTestChannelWithConstructor(t, caller, &ta.DefaultConstructor{})
	ch.pollRegistry = make(map[string]telegramPollEntry)
	ch.pollByTgID = make(map[string]string)
	return ch
}

func readRequestBody(data *ta.RequestData) []byte {
	if data == nil {
		return nil
	}
	if len(data.BodyRaw) > 0 {
		return data.BodyRaw
	}
	if data.BodyStream != nil {
		b, _ := io.ReadAll(data.BodyStream)
		return b
	}
	return nil
}

func TestTelegramChannel_SendPoll_RegistersLocalHandleAndLifecycle(t *testing.T) {
	var sentPollParams telego.SendPollParams
	var stoppedPollParams telego.StopPollParams

	ch := newTestPollChannel(t, func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
		body := readRequestBody(data)
		if strings.Contains(url, "sendPoll") {
			if err := json.Unmarshal(body, &sentPollParams); err != nil {
				return nil, err
			}
			msg := telego.Message{
				MessageID: 101,
				Chat:      telego.Chat{ID: 12345},
				Poll: &telego.Poll{
					ID:       "tg-poll-999",
					Question: sentPollParams.Question,
				},
			}
			msgBytes, _ := json.Marshal(msg)
			return &ta.Response{
				Ok:     true,
				Result: msgBytes,
			}, nil
		}
		if strings.Contains(url, "stopPoll") {
			if err := json.Unmarshal(body, &stoppedPollParams); err != nil {
				return nil, err
			}
			poll := telego.Poll{ID: "tg-poll-999", IsClosed: true}
			pollBytes, _ := json.Marshal(poll)
			return &ta.Response{
				Ok:     true,
				Result: pollBytes,
			}, nil
		}
		return &ta.Response{Ok: true, Result: []byte("{}")}, nil
	})

	localHandle := "local-handle-abc-123"
	outbound := bus.OutboundMessage{
		ChatID:     "12345",
		AgentID:    "main",
		SessionKey: "sess-1",
		Context: bus.InboundContext{
			SenderID: "user-42",
		},
		Poll: &bus.PollPayload{
			ID:          localHandle,
			Mode:        "regular",
			Question:    "What is your favorite editor?",
			Options:     []string{"Neovim", "Emacs", "VSCode"},
			IsAnonymous: true,
		},
	}

	msgIDs, err := ch.Send(context.Background(), outbound)
	require.NoError(t, err)
	require.Equal(t, []string{"101"}, msgIDs)
	assert.Equal(t, "What is your favorite editor?", sentPollParams.Question)

	// Verify local handle is mapped in registry
	entry, ok := ch.resolvePollByLocalHandle(localHandle)
	require.True(t, ok)
	assert.Equal(t, localHandle, entry.LocalHandle)
	assert.Equal(t, "tg-poll-999", entry.TelegramPollID)
	assert.Equal(t, int64(12345), entry.ChatID)
	assert.Equal(t, 101, entry.MessageID)

	// Reverse lookup by TG poll ID works
	byTg, ok := ch.resolvePollByTgPollID("tg-poll-999")
	require.True(t, ok)
	assert.Equal(t, localHandle, byTg.LocalHandle)

	// Now stop the poll using local handle
	stopErr := ch.StopPoll(context.Background(), localHandle, "main", "sess-1", "user-42")
	require.NoError(t, stopErr)
	assert.Equal(t, 101, stoppedPollParams.MessageID)

	// Verify entry is cleaned up after stop
	_, ok = ch.resolvePollByLocalHandle(localHandle)
	assert.False(t, ok)
}

func TestTelegramChannel_StopPoll_OwnershipIsolation(t *testing.T) {
	ch := newTestPollChannel(t, func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
		return &ta.Response{Ok: true, Result: []byte("{}")}, nil
	})

	localHandle := "poll-secure-1"
	ch.registerPollEntry(telegramPollEntry{
		LocalHandle:    localHandle,
		TelegramPollID: "tg-100",
		ChatID:         555,
		MessageID:      200,
		AgentID:        "main-agent",
		SessionKey:     "session-alpha",
		SenderID:       "alice",
		CreatedAt:      time.Now(),
	})

	// Different agent
	err := ch.StopPoll(context.Background(), localHandle, "other-agent", "session-alpha", "alice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "caller agent \"other-agent\" not authorized")

	// Different session
	err = ch.StopPoll(context.Background(), localHandle, "main-agent", "session-beta", "alice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "caller session \"session-beta\" not authorized")

	// Different user
	err = ch.StopPoll(context.Background(), localHandle, "main-agent", "session-alpha", "bob")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "caller sender \"bob\" not authorized")

	// Authorized caller succeeds
	err = ch.StopPoll(context.Background(), localHandle, "main-agent", "session-alpha", "alice")
	require.NoError(t, err)
}

func TestTelegramChannel_SendQuiz_MultipleCorrectAndModernFields(t *testing.T) {
	var captured telego.SendPollParams

	ch := newTestPollChannel(t, func(ctx context.Context, url string, data *ta.RequestData) (*ta.Response, error) {
		if strings.Contains(url, "sendPoll") {
			body := readRequestBody(data)
			if err := json.Unmarshal(body, &captured); err != nil {
				return nil, err
			}
			msg := telego.Message{
				MessageID: 202,
				Chat:      telego.Chat{ID: -100123},
				Poll: &telego.Poll{
					ID:       "tg-quiz-multiple",
					Question: captured.Question,
				},
			}
			msgBytes, _ := json.Marshal(msg)
			return &ta.Response{
				Ok:     true,
				Result: msgBytes,
			}, nil
		}
		return &ta.Response{Ok: true, Result: []byte("{}")}, nil
	})

	outbound := bus.OutboundMessage{
		ChatID:  "-100123/42",
		AgentID: "quiz-agent",
		Poll: &bus.PollPayload{
			ID:                     "quiz-multi-1",
			Mode:                   "quiz",
			Question:               "Which are programming languages?",
			Options:                []string{"Go", "Rust", "HTML", "CSS"},
			CorrectOptionIDs:       []int{0, 1},
			Explanation:            "Go and Rust are programming languages, HTML and CSS are markup/styling.",
			AllowsRevoting:         false,
			ShuffleOptions:         true,
			HideResultsUntilCloses: true,
		},
	}

	msgIDs, err := ch.Send(context.Background(), outbound)
	require.NoError(t, err)
	require.Equal(t, []string{"202"}, msgIDs)

	assert.Equal(t, telego.PollTypeQuiz, captured.Type)
	assert.Equal(t, []int{0, 1}, captured.CorrectOptionIDs)
	assert.True(t, captured.AllowsMultipleAnswers)
	assert.False(t, captured.AllowsRevoting)
	assert.True(t, captured.ShuffleOptions)
	assert.True(t, captured.HideResultsUntilCloses)
	assert.Equal(t, "Which are programming languages?", captured.Question)
	assert.Equal(t, "Go and Rust are programming languages, HTML and CSS are markup/styling.", captured.Explanation)
	assert.Equal(t, 42, captured.MessageThreadID)
}

func TestTelegramChannel_PollFallback_PreservesQuizSemantics(t *testing.T) {
	payload := &bus.PollPayload{
		Mode:             "quiz",
		Question:         "What is 2+2?",
		Options:          []string{"3", "4", "5"},
		CorrectOptionIDs: []int{1},
		Explanation:      "Basic arithmetic.",
	}
	fallbackText := formatPollFallbackText(payload)

	// Initial fallback must NOT leak correct answer or explanation
	assert.Contains(t, fallbackText, "📝 Quiz: What is 2+2?")
	assert.Contains(t, fallbackText, "1. 3")
	assert.Contains(t, fallbackText, "2. 4")
	assert.Contains(t, fallbackText, "3. 5")
	assert.NotContains(t, fallbackText, "✅ Correct")
	assert.NotContains(t, fallbackText, "💡 Explanation")

	// Reveal response contains answer and explanation
	revealText := formatQuizRevealText(payload)
	assert.Contains(t, revealText, "📝 Quiz: What is 2+2?")
	assert.Contains(t, revealText, "✅ Correct answer: 2. 4")
	assert.Contains(t, revealText, "💡 Explanation: Basic arithmetic.")
}

func TestTelegramChannel_PollRegistry_BoundedEvictionAndTTL(t *testing.T) {
	ch := newTestPollChannel(t, nil)

	// Add 1050 items (exceeding maxPollRegistryEntries 1000)
	for i := 0; i < 1050; i++ {
		ch.registerPollEntry(telegramPollEntry{
			LocalHandle:    fmt.Sprintf("handle-%d", i),
			TelegramPollID: fmt.Sprintf("tg-%d", i),
			CreatedAt:      time.Now().Add(time.Duration(i) * time.Millisecond),
		})
	}

	ch.pollRegistryMu.Lock()
	count := len(ch.pollRegistry)
	ch.pollRegistryMu.Unlock()

	// Should be strictly capped to max capacity
	assert.LessOrEqual(t, count, maxPollRegistryEntries)
}

func TestTelegramChannel_PollRegistry_Concurrency(t *testing.T) {
	ch := newTestPollChannel(t, nil)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			handle := fmt.Sprintf("conc-handle-%d", idx)
			ch.registerPollEntry(telegramPollEntry{
				LocalHandle:    handle,
				TelegramPollID: fmt.Sprintf("tg-conc-%d", idx),
				CreatedAt:      time.Now(),
			})
			_, _ = ch.resolvePollByLocalHandle(handle)
			ch.updatePollStateByTgPollID(fmt.Sprintf("tg-conc-%d", idx), true)
		}(i)
	}
	wg.Wait()
}

func TestTelegramChannel_StopPoll_OwnershipAndErrors(t *testing.T) {
	ch := newTestPollChannel(t, nil)

	// Not found
	err := ch.StopPoll(context.Background(), "non-existent", "agent-1", "sess-1", "user-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Register a poll
	handle := "test-stop-handle"
	ch.registerPollEntry(telegramPollEntry{
		LocalHandle:    handle,
		TelegramPollID: "tg-poll-123",
		Account:        "default",
		ChatID:         12345,
		MessageID:      99,
		AgentID:        "agent-1",
		SessionKey:     "sess-1",
		SenderID:       "user-1",
		CreatedAt:      time.Now(),
	})

	// Unauthorized agent
	err = ch.StopPoll(context.Background(), handle, "agent-2", "sess-1", "user-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authorized")

	// Unauthorized session
	err = ch.StopPoll(context.Background(), handle, "agent-1", "sess-2", "user-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authorized")

	// Unauthorized sender
	err = ch.StopPoll(context.Background(), handle, "agent-1", "sess-1", "user-2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not authorized")

	// Entry still preserved after authorization failure
	_, found := ch.resolvePollByLocalHandle(handle)
	assert.True(t, found)
}
