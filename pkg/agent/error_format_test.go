package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/providers/common"
)

func TestFormatProcessingError_InvalidAPIKey(t *testing.T) {
	err := errors.New(
		`LLM call failed after retries: API request failed: Status: 401 Body: {"error":{"message":"Incorrect API key provided: sk-secret"}}`,
	)

	got := formatProcessingError(err)
	if !strings.Contains(got, "API key appears to be invalid") {
		t.Fatalf("formatted error missing friendly API key hint: %q", got)
	}
	if strings.Contains(got, "Original error:") || strings.Contains(got, "sk-secret") || strings.Contains(got, err.Error()) {
		t.Fatalf("formatted auth error leaked raw provider details: %q", got)
	}
}

func TestFormatProcessingError_GenericAuthHTTPError(t *testing.T) {
	err := &common.HTTPError{
		StatusCode:  401,
		BodyPreview: `{"error":"unauthorized","token":"private-token"}`,
		ContentType: "application/json",
		APIBase:     "https://user:secret@api.example.com/private?token=abc",
	}

	got := formatProcessingError(err)
	if !strings.Contains(got, "check the API key, token, OAuth login, or provider permissions") {
		t.Fatalf("formatted error missing generic auth hint: %q", got)
	}
	for _, secret := range []string{"Original error:", "private-token", "user:secret", "token=abc"} {
		if strings.Contains(got, secret) {
			t.Fatalf("formatted auth error leaked %q: %q", secret, got)
		}
	}
}

func TestFormatProcessingError_NonAuthIsSanitized(t *testing.T) {
	err := errors.New("dial /home/private/.config/picoclaw/token.sock: connection reset by peer")
	got := formatProcessingError(err)
	want := "Error processing message: an internal service failed. Please try again."
	if got != want {
		t.Fatalf("formatted error = %q, want %q", got, want)
	}
	if strings.Contains(got, "/home/private") || strings.Contains(got, err.Error()) {
		t.Fatalf("formatted error leaked infrastructure details: %q", got)
	}
}
