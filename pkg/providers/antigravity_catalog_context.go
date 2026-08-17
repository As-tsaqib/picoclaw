package providers

import (
	"context"

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
