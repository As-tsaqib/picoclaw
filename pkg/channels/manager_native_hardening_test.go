package channels

import (
	"context"
	"testing"

	"golang.org/x/time/rate"

	"github.com/As-tsaqib/picoclaw/pkg/bus"
)

type preferredTestChannel struct {
	mockSessionStreamingChannel
	preferredCalled bool
	legacyCalled    bool
}

func (c *preferredTestChannel) BeginPreferredStreamForSession(_ context.Context, _, _ string) (Streamer, error) {
	c.preferredCalled = true
	return c.streamer, nil
}

func (c *preferredTestChannel) BeginStreamForSession(ctx context.Context, chatID, sessionKey string) (Streamer, error) {
	c.legacyCalled = true
	return c.mockSessionStreamingChannel.BeginStreamForSession(ctx, chatID, sessionKey)
}

func TestManagerPrefersPreferredSessionStreamer(t *testing.T) {
	ch := &preferredTestChannel{}
	ch.streamer = &mockStreamer{}
	m := newTestManager()
	m.channels["telegram"] = ch
	streamer, ok := m.GetStreamer(context.Background(), "telegram", "1", "session")
	if !ok || streamer == nil || !ch.preferredCalled || ch.legacyCalled {
		t.Fatalf("preferred=%v legacy=%v ok=%v streamer=%T", ch.preferredCalled, ch.legacyCalled, ok, streamer)
	}
}

type semanticMediaTestChannel struct {
	mockMediaChannel
	semanticHandled bool
	semanticErr     error
	semanticCalls   int
}

func (c *semanticMediaTestChannel) SendSemanticMedia(_ context.Context, _ bus.OutboundMediaMessage) ([]string, bool, error) {
	c.semanticCalls++
	return []string{"semantic"}, c.semanticHandled, c.semanticErr
}

func TestManagerSemanticMediaPrecedesOrdinaryMedia(t *testing.T) {
	ch := &semanticMediaTestChannel{semanticHandled: true}
	ch.sendMediaFn = func(context.Context, bus.OutboundMediaMessage) ([]string, error) {
		t.Fatal("ordinary media path called after semantic handler accepted payload")
		return nil, nil
	}
	m := newTestManager()
	w := &channelWorker{ch: ch, limiter: rate.NewLimiter(rate.Inf, 1)}
	ids, err := m.sendMediaWithRetry(context.Background(), "telegram", w, bus.OutboundMediaMessage{
		Context: bus.InboundContext{Channel: "telegram"}, Parts: []bus.MediaPart{{Ref: "semantic"}},
	})
	if err != nil || len(ids) != 1 || ids[0] != "semantic" || ch.semanticCalls != 1 {
		t.Fatalf("ids=%v err=%v semanticCalls=%d", ids, err, ch.semanticCalls)
	}
}

func TestManagerSemanticMediaDelegatesWhenUnhandled(t *testing.T) {
	ch := &semanticMediaTestChannel{}
	ch.sendMediaFn = func(context.Context, bus.OutboundMediaMessage) ([]string, error) {
		return []string{"ordinary"}, nil
	}
	m := newTestManager()
	w := &channelWorker{ch: ch, limiter: rate.NewLimiter(rate.Inf, 1)}
	ids, err := m.sendMediaWithRetry(context.Background(), "telegram", w, bus.OutboundMediaMessage{
		Context: bus.InboundContext{Channel: "telegram"}, Parts: []bus.MediaPart{{Ref: "media://ordinary"}},
	})
	if err != nil || len(ids) != 1 || ids[0] != "ordinary" || ch.semanticCalls != 1 || len(ch.sentMediaMessages) != 1 {
		t.Fatalf("ids=%v err=%v semanticCalls=%d ordinaryCalls=%d", ids, err, ch.semanticCalls, len(ch.sentMediaMessages))
	}
}
