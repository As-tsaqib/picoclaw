package telegram

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestPollAnswerUpdateModernIdentityAndPersistentIDs(t *testing.T) {
	ch := newTestPollChannel(t, nil)
	ch.registerPollEntry(telegramPollEntry{LocalHandle: "h", TelegramPollID: "tg", CreatedAt: time.Now()})

	user := &telego.User{ID: 42}
	require.NoError(t, ch.handlePollAnswerUpdate(nil, &telego.PollAnswer{
		PollID:              "tg",
		User:                user,
		OptionIDs:           []int{1},
		OptionPersistentIDs: []string{"persistent-b"},
	}))
	entry, ok := ch.resolvePollByLocalHandle("h")
	require.True(t, ok)
	vote, ok := entry.Votes["user:42"]
	require.True(t, ok)
	assert.Equal(t, []int{1}, vote.OptionIDs)
	assert.Equal(t, []string{"persistent-b"}, vote.OptionPersistentIDs)

	voterChat := &telego.Chat{ID: -10077, Type: telego.ChatTypeChannel}
	require.NoError(t, ch.handlePollAnswerUpdate(nil, &telego.PollAnswer{
		PollID:              "tg",
		VoterChat:           voterChat,
		OptionIDs:           []int{0},
		OptionPersistentIDs: []string{"persistent-a"},
	}))
	entry, _ = ch.resolvePollByLocalHandle("h")
	_, ok = entry.Votes["chat:-10077"]
	assert.True(t, ok)

	require.NoError(t, ch.handlePollAnswerUpdate(nil, &telego.PollAnswer{
		PollID:              "tg",
		User:                user,
		OptionIDs:           []int{2},
		OptionPersistentIDs: []string{"persistent-c"},
	}))
	entry, _ = ch.resolvePollByLocalHandle("h")
	assert.Equal(t, []int{2}, entry.Votes["user:42"].OptionIDs)

	require.NoError(t, ch.handlePollAnswerUpdate(nil, &telego.PollAnswer{PollID: "tg", User: user}))
	entry, _ = ch.resolvePollByLocalHandle("h")
	_, ok = entry.Votes["user:42"]
	assert.False(t, ok)

	require.NoError(t, ch.handlePollAnswerUpdate(nil, &telego.PollAnswer{PollID: "tg", OptionIDs: []int{0}}))
	require.NoError(t, ch.handlePollAnswerUpdate(nil, &telego.PollAnswer{
		PollID:    "unknown",
		User:      user,
		OptionIDs: []int{0},
	}))
}

func TestPollRouteAuthorizationAllDimensions(t *testing.T) {
	entry := telegramPollEntry{
		Account:    "account-a",
		ChatID:     -1001,
		ThreadID:   17,
		AgentID:    "agent-a",
		SessionKey: "session-a",
		SenderID:   "42",
	}
	good := telegramPollRoute{
		Account:    "account-a",
		ChatID:     -1001,
		ThreadID:   17,
		AgentID:    "agent-a",
		SessionKey: "session-a",
		SenderID:   "42",
	}
	require.NoError(t, pollRouteAuthorized(entry, good))

	cases := []struct {
		name   string
		mutate func(*telegramPollRoute)
	}{
		{"account", func(r *telegramPollRoute) { r.Account = "account-b" }},
		{"chat", func(r *telegramPollRoute) { r.ChatID = -1002 }},
		{"topic", func(r *telegramPollRoute) { r.ThreadID = 18 }},
		{"agent", func(r *telegramPollRoute) { r.AgentID = "agent-b" }},
		{"session", func(r *telegramPollRoute) { r.SessionKey = "session-b" }},
		{"sender", func(r *telegramPollRoute) { r.SenderID = "43" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			route := good
			tc.mutate(&route)
			require.Error(t, pollRouteAuthorized(entry, route))
		})
	}
}

func TestStopPollTransientFailureRetainsRegistryAndSuccessCleans(t *testing.T) {
	var mu sync.Mutex
	fail := true
	ch := newTestPollChannel(t, func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
		if !strings.Contains(url, "stopPoll") {
			return &ta.Response{Ok: true, Result: []byte("{}")}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		if fail {
			return nil, fmt.Errorf("temporary transport failure")
		}
		payload, _ := json.Marshal(telego.Poll{ID: "tg", IsClosed: true})
		return &ta.Response{Ok: true, Result: payload}, nil
	})
	entry := telegramPollEntry{
		LocalHandle:    "h",
		TelegramPollID: "tg",
		Account:        ch.Name(),
		ChatID:         9,
		ThreadID:       3,
		MessageID:      7,
		AgentID:        "a",
		SessionKey:     "s",
		SenderID:       "42",
	}
	ch.registerPollEntry(entry)
	route := telegramPollRoute{
		Account:    ch.Name(),
		ChatID:     9,
		ThreadID:   3,
		AgentID:    "a",
		SessionKey: "s",
		SenderID:   "42",
	}

	require.Error(t, ch.StopPollForRoute(context.Background(), "h", route))
	_, ok := ch.resolvePollByLocalHandle("h")
	assert.True(t, ok, "transient StopPoll failure must keep retryable registry state")

	mu.Lock()
	fail = false
	mu.Unlock()
	require.NoError(t, ch.StopPollForRoute(context.Background(), "h", route))
	_, ok = ch.resolvePollByLocalHandle("h")
	assert.False(t, ok)
}

func TestQuizRevealClaimScopeReplayAndConcurrency(t *testing.T) {
	ch := newTestPollChannel(t, nil)
	entry := telegramPollEntry{
		LocalHandle: "quiz",
		Account:     ch.Name(),
		ChatID:      -1001,
		ThreadID:    5,
		MessageID:   99,
		AgentID:     "agent",
		SessionKey:  "session",
		SenderID:    "42",
		PollPayload: &bus.PollPayload{
			Mode:             "quiz",
			Question:         "Q",
			Options:          []string{"A"},
			CorrectOptionIDs: []int{0},
		},
	}
	ch.registerPollEntry(entry)
	good := func() *telego.CallbackQuery {
		return &telego.CallbackQuery{
			ID:   "cb",
			From: telego.User{ID: 42},
			Data: "quiz_reveal:quiz",
			Message: &telego.Message{
				MessageID:       99,
				MessageThreadID: 5,
				Chat:            telego.Chat{ID: -1001, Type: telego.ChatTypeSupergroup},
			},
		}
	}
	_, err := ch.claimQuizReveal(good(), "quiz")
	require.NoError(t, err)
	ch.finishQuizReveal("quiz", false)

	wrongSender := good()
	wrongSender.From.ID = 43
	_, err = ch.claimQuizReveal(wrongSender, "quiz")
	require.Error(t, err)
	wrongChat := good()
	wrongChat.Message.Message().Chat.ID = -1002
	_, err = ch.claimQuizReveal(wrongChat, "quiz")
	require.Error(t, err)
	wrongTopic := good()
	wrongTopic.Message.Message().MessageThreadID = 6
	_, err = ch.claimQuizReveal(wrongTopic, "quiz")
	require.Error(t, err)
	wrongMessage := good()
	wrongMessage.Message.Message().MessageID = 100
	_, err = ch.claimQuizReveal(wrongMessage, "quiz")
	require.Error(t, err)
	_, err = ch.claimQuizReveal(good(), "stale")
	require.Error(t, err)

	badOwnership := entry
	badOwnership.LocalHandle = "bad-owner"
	badOwnership.SessionKey = ""
	ch.registerPollEntry(badOwnership)
	_, err = ch.claimQuizReveal(good(), "bad-owner")
	require.Error(t, err)

	ch.finishQuizReveal("quiz", false)
	const workers = 16
	var wg sync.WaitGroup
	var wins int
	var winsMu sync.Mutex
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, claimErr := ch.claimQuizReveal(good(), "quiz"); claimErr == nil {
				winsMu.Lock()
				wins++
				winsMu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, wins, "only one concurrent callback may claim reveal")
	ch.finishQuizReveal("quiz", true)
	_, err = ch.claimQuizReveal(good(), "quiz")
	require.Error(t, err, "successful reveal must be replay-safe")
}

func TestStopPollForRouteAcceptsBoundTokenAndRejectsWrongRoute(t *testing.T) {
	ch := newTestChannel(
		t,
		&stubCaller{callFn: func(_ context.Context, url string, _ *ta.RequestData) (*ta.Response, error) {
			require.Contains(t, url, "stopPoll")
			return successResponse(t), nil
		}},
	)
	entry := telegramPollEntry{
		LocalHandle: "route-bound", Account: ch.Name(), ChatID: -1001, ThreadID: 42,
		MessageID: 7, AgentID: "main", SenderID: "alice", SessionKey: "session-a",
	}
	ch.registerPollEntry(entry)
	token := bus.NewPollStopRouteToken(
		entry.LocalHandle,
		entry.Account,
		"-1001/42",
		"42",
		entry.AgentID,
		"",
		entry.SessionKey,
	)
	wrong := telegramPollRoute{
		Account:    entry.Account,
		ChatID:     -1002,
		ThreadID:   42,
		AgentID:    "main",
		SenderID:   "alice",
		SessionKey: "session-a",
	}
	require.Error(t, ch.StopPollForRoute(context.Background(), token, wrong))
	if _, ok := ch.resolvePollByLocalHandle(entry.LocalHandle); !ok {
		t.Fatal("failed authorization consumed poll state")
	}
	good := telegramPollRoute{
		Account:    entry.Account,
		ChatID:     -1001,
		ThreadID:   42,
		AgentID:    "main",
		SenderID:   "alice",
		SessionKey: "session-a",
	}
	require.NoError(t, ch.StopPollForRoute(context.Background(), token, good))
	if _, ok := ch.resolvePollByLocalHandle(entry.LocalHandle); ok {
		t.Fatal("successful stop retained poll state")
	}
}
