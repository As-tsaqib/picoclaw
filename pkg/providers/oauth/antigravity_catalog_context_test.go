package oauthprovider

import (
	"context"
	"errors"
	"testing"
)

func TestFetchAntigravityModelsContextHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := FetchAntigravityModelsContext(ctx, "test-token", "test-project")
	if err == nil {
		t.Fatal("expected canceled discovery request to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
