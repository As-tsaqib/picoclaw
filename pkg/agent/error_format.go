package agent

import "github.com/As-tsaqib/picoclaw/pkg/providers"

func formatProcessingError(err error) string {
	if err == nil {
		return ""
	}
	if kind, ok := providers.ClassifyAuthError(err); ok {
		return "Error processing message: " + authErrorFriendlyMessage(kind)
	}
	return "Error processing message: an internal service failed. Please try again."
}

func authErrorFriendlyMessage(kind providers.AuthErrorKind) string {
	switch kind {
	case providers.AuthErrorInvalidAPIKey:
		return "Authentication failed: the configured API key was rejected. Check the credentials for this model or provider."
	case providers.AuthErrorMissingAPIKey:
		return "Authentication failed: this model or provider has no usable API key configured."
	case providers.AuthErrorExpiredToken:
		return "Authentication failed: the saved login or token appears to be expired. Re-authenticate the provider."
	default:
		return "Authentication failed: check the provider credentials or permissions for this model."
	}
}
