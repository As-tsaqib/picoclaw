package providers

import (
	"context"
	"net/http"

	oauthprovider "github.com/As-tsaqib/picoclaw/pkg/providers/oauth"
)

// FetchAntigravityModelsContext exposes cancellable model discovery without
// changing the existing compatibility helper used by older callers.
func FetchAntigravityModelsContext(
	ctx context.Context,
	accessToken, projectID string,
) ([]AntigravityModelInfo, error) {
	return oauthprovider.FetchAntigravityModelsContext(ctx, accessToken, projectID)
}

// FetchAntigravityModelsWithClientContext is used by configured model
// discovery so proxy and transport policy stay consistent with other sources.
func FetchAntigravityModelsWithClientContext(
	ctx context.Context,
	client *http.Client,
	accessToken, projectID string,
	customHeaders map[string]string,
	userAgent string,
) ([]AntigravityModelInfo, error) {
	return oauthprovider.FetchAntigravityModelsWithClientContext(
		ctx,
		client,
		accessToken,
		projectID,
		customHeaders,
		userAgent,
	)
}

// FetchAntigravityModelsAtBaseURLWithClientContext allows model discovery to
// honor an explicitly configured Antigravity endpoint while retaining the
// production endpoint when baseURL is empty.
func FetchAntigravityModelsAtBaseURLWithClientContext(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	accessToken, projectID string,
	customHeaders map[string]string,
	userAgent string,
) ([]AntigravityModelInfo, error) {
	return oauthprovider.FetchAntigravityModelsAtBaseURLWithClientContext(
		ctx, client, baseURL, accessToken, projectID, customHeaders, userAgent,
	)
}

// IsAntigravityUnauthorized reports whether err is an Antigravity HTTP 401.
func IsAntigravityUnauthorized(err error) bool {
	return oauthprovider.IsAntigravityUnauthorized(err)
}
