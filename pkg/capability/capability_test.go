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

	// Checklist is conditional on business context
	assert.False(t, set.IsSupported(FeatureChecklistNative))
	assert.Equal(t, StateConditional, set[FeatureChecklistNative].State)
	assert.Equal(t, "business_connection", set[FeatureChecklistNative].Condition)

	// Rich stream is unsupported/text-only by default
	assert.False(t, set.IsSupported(FeatureMessageStreamRich))
	assert.Equal(t, StateUnsupported, set[FeatureMessageStreamRich].State)
}

func TestResolveRouteCapabilities_ChecklistWithBusinessContext(t *testing.T) {
	route := RouteContext{
		Channel:            "telegram",
		Account:            "biz-1",
		HasBusinessContext: true,
	}
	set := ResolveRouteCapabilities(route, nil)
	assert.True(t, set.IsSupported(FeatureChecklistNative))
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
	assert.Contains(t, prompt, "rich_stream=unsupported")
	assert.Contains(t, prompt, "checklist=conditional:business_connection")
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
