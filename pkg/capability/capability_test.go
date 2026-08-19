package capability

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestResolveRouteCapabilities_TelegramDefaults(t *testing.T) {
	route := RouteContext{
		Channel: "telegram",
		Account: "default",
	}
	set := ResolveRouteCapabilities(route, nil)

	assert.True(t, set.IsSupported(FeaturePollQuiz))
	assert.True(t, set.IsSupported(FeaturePollRegular))
	assert.True(t, set.IsSupported(FeatureMessageStructuredRich))
	assert.True(t, set.IsSupported(FeatureKeyboardInline))
	assert.True(t, set.IsSupported(FeatureMediaAnimation))
	assert.True(t, set.IsSupported(FeatureDiceAnimated))
	assert.True(t, set.IsSupported(FeatureMessageStreamRich))
	assert.True(t, set.IsSupported(FeatureMediaLivePhoto))

	// Checklist stays conditional until trusted business authority is wired end-to-end.
	assert.False(t, set.IsSupported(FeatureChecklistNative))
	assert.Equal(t, StateConditional, set[FeatureChecklistNative].State)
	assert.Equal(t, "context_unavailable", set[FeatureChecklistNative].Condition)
}

func TestResolveRouteCapabilities_ChecklistWithBusinessContext(t *testing.T) {
	route := RouteContext{
		Channel:            "telegram",
		Account:            "biz-1",
		HasBusinessContext: true,
	}
	set := ResolveRouteCapabilities(route, nil)
	assert.Equal(t, StateConditional, set[FeatureChecklistNative].State)
	assert.Equal(t, "context_unavailable", set[FeatureChecklistNative].Condition)
}

func TestResolveRouteCapabilities_NegativeCacheDowngrade(t *testing.T) {
	cache := NewNegativeCapabilityCache(1 * time.Minute)

	route := RouteContext{
		Channel:  "telegram",
		Account:  "custom-server",
		ServerID: "https://custom-tg.internal",
	}

	// Initially supported
	set := ResolveRouteCapabilities(route, cache)
	assert.True(t, set.IsSupported(FeaturePollQuiz))

	// Server returns method not found
	downgraded := cache.RecordFailure(
		route.Channel,
		route.Account,
		route.ServerID,
		FeaturePollQuiz,
		errors.New("api: 400 Bad Request: METHOD_NOT_FOUND sendPoll"),
	)
	assert.True(t, downgraded)

	// Now should be downgraded
	setAfter := ResolveRouteCapabilities(route, cache)
	assert.False(t, setAfter.IsSupported(FeaturePollQuiz))
	assert.Equal(t, StateUnsupported, setAfter[FeaturePollQuiz].State)

	// Unrelated feature (e.g. rich_message, inline_keyboard) remains supported!
	assert.True(t, setAfter.IsSupported(FeatureMessageStructuredRich))
	assert.True(t, setAfter.IsSupported(FeatureKeyboardInline))
}

func TestResolveRouteCapabilities_NegativeCacheIgnoresTransientErrors(t *testing.T) {
	cache := NewNegativeCapabilityCache(1 * time.Minute)

	transientErrors := []error{
		errors.New("api: 429 Too Many Requests: retry after 5"),
		errors.New("api: 401 Unauthorized"),
		errors.New("api: 403 Forbidden: bot was blocked by the user"),
		errors.New("dial tcp: i/o timeout"),
		errors.New("connection refused"),
		errors.New("api: 500 Internal Server Error: method not implemented"),
		errors.New("api: 502 Bad Gateway: unsupported method"),
		errors.New("network is unreachable"),
		errors.New("lookup api.telegram.local: no such host"),
	}

	for _, err := range transientErrors {
		downgraded := cache.RecordFailure("telegram", "default", "", FeaturePollQuiz, err)
		assert.False(t, downgraded, "transient error should not cause downgrade: %v", err)
	}

	assert.False(t, cache.IsDowngraded("telegram", "default", "", FeaturePollQuiz))
}

func TestFormatCapabilityPrompt_MatchesRoute(t *testing.T) {
	route := RouteContext{Channel: "telegram"}
	set := ResolveRouteCapabilities(route, nil)
	prompt := FormatCapabilityPrompt(set, "telegram")

	assert.Contains(t, prompt, "channel=telegram")
	assert.Contains(t, prompt, "native_quiz=supported")
	assert.Contains(t, prompt, "native_poll=supported")
	assert.Contains(t, prompt, "stop_poll=supported")
	assert.Contains(t, prompt, "rich_message=supported")
	assert.Contains(t, prompt, "rich_stream=supported")
	assert.Contains(t, prompt, "live_photo=supported")
	assert.Contains(t, prompt, "checklist=conditional:context_unavailable")
}

func TestNegativeCache_TTLAndEviction(t *testing.T) {
	cache := NewNegativeCapabilityCache(20 * time.Millisecond)

	cache.RecordFailure("telegram", "acc", "srv", FeaturePollQuiz, errors.New("method not found"))
	assert.True(t, cache.IsDowngraded("telegram", "acc", "srv", FeaturePollQuiz))

	time.Sleep(30 * time.Millisecond)
	assert.False(t, cache.IsDowngraded("telegram", "acc", "srv", FeaturePollQuiz))
}

func TestNegativeCache_CapacityBounding(t *testing.T) {
	cache := NewNegativeCapabilityCache(1 * time.Hour)

	for i := 0; i < 600; i++ {
		cache.RecordFailure(
			"telegram",
			fmt.Sprintf("acc-%d", i),
			"srv",
			FeaturePollQuiz,
			errors.New("method not found"),
		)
	}

	cache.mu.RLock()
	count := len(cache.entries)
	cache.mu.RUnlock()

	assert.LessOrEqual(t, count, maxNegativeCacheEntries)
}

func TestNegativeCache_Concurrency(t *testing.T) {
	cache := NewNegativeCapabilityCache(1 * time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			acc := fmt.Sprintf("acc-%d", idx%5)
			cache.RecordFailure("telegram", acc, "srv", FeaturePollQuiz, errors.New("method not found"))
			_ = cache.IsDowngraded("telegram", acc, "srv", FeaturePollQuiz)
		}(i)
	}
	wg.Wait()
}

func TestCapabilityCacheKeyConsistency(t *testing.T) {
	cache := NewNegativeCapabilityCache(10 * time.Minute)

	// Custom server with trailing slash vs no trailing slash normalizes consistently
	cache.RecordFailure(
		"telegram",
		"default",
		"http://custom-tg:8081/",
		FeaturePollQuiz,
		errors.New("method not found"),
	)
	assert.True(t, cache.IsDowngraded("telegram", "default", "http://custom-tg:8081", FeaturePollQuiz))

	// Official API server empty string vs https://api.telegram.org normalizes consistently
	cache.RecordFailure(
		"telegram",
		"personal",
		"https://api.telegram.org",
		FeaturePollRegular,
		errors.New("not supported"),
	)
	assert.True(t, cache.IsDowngraded("telegram", "personal", "", FeaturePollRegular))
	assert.True(t, cache.IsDowngraded("telegram", "personal", "official", FeaturePollRegular))
}

func TestResolveRouteCapabilities_ImplementedNativePathsAreTruthful(t *testing.T) {
	route := RouteContext{Channel: "telegram", Account: "default"}
	set := ResolveRouteCapabilities(route, nil)
	assert.True(t, set.IsSupported(FeatureMessageStreamRich))
	assert.True(t, set.IsSupported(FeatureMediaLivePhoto))
	assert.Equal(t, StateUnsupported, set[FeatureKeyboardReply].State)
	assert.Equal(t, "not_implemented", set[FeatureKeyboardReply].Condition)
	assert.Equal(t, StateConditional, set[FeatureChecklistNative].State)
	assert.Equal(t, "context_unavailable", set[FeatureChecklistNative].Condition)
}

func TestResolveRouteCapabilities_EphemeralDraftStreamingFailsClosed(t *testing.T) {
	set := ResolveRouteCapabilities(RouteContext{Channel: "telegram", IsEphemeral: true}, nil)
	assert.Equal(t, StateConditional, set[FeatureMessageStreamText].State)
	assert.Equal(t, "non_ephemeral_route", set[FeatureMessageStreamText].Condition)
	assert.Equal(t, StateConditional, set[FeatureMessageStreamRich].State)
	assert.Equal(t, "non_ephemeral_route", set[FeatureMessageStreamRich].Condition)
	assert.True(t, set.IsSupported(FeatureMessageEphemeral))
}

func TestResolveRouteCapabilities_RichAndLivePhotoServerDowngrade(t *testing.T) {
	cache := NewNegativeCapabilityCache(time.Minute)
	route := RouteContext{Channel: "telegram", Account: "legacy", ServerID: "http://legacy.example"}
	if !cache.RecordFailure(
		route.Channel,
		route.Account,
		route.ServerID,
		FeatureMessageStreamRich,
		errors.New("400 Bad Request: method not found"),
	) {
		t.Fatal("rich draft unsupported failure was not cached")
	}
	if !cache.RecordFailure(
		route.Channel,
		route.Account,
		route.ServerID,
		FeatureMediaLivePhoto,
		errors.New("400 Bad Request: method not found"),
	) {
		t.Fatal("live photo unsupported failure was not cached")
	}
	set := ResolveRouteCapabilities(route, cache)
	assert.Equal(t, StateUnsupported, set[FeatureMessageStreamRich].State)
	assert.Equal(t, "downgraded_by_server", set[FeatureMessageStreamRich].Condition)
	assert.Equal(t, StateUnsupported, set[FeatureMediaLivePhoto].State)
	assert.True(t, set.IsSupported(FeaturePollRegular))
}
