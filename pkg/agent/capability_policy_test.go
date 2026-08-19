package agent

import (
	"testing"

	"github.com/As-tsaqib/picoclaw/pkg/capability"
	"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestCapabilityPolicyDisabledFeaturesMatchesToolConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Tools.SendLivePhoto.Enabled = false
	cfg.Tools.SendQuiz.Enabled = false
	disabled := capabilityPolicyDisabledFeatures(cfg)
	for _, feature := range []capability.Feature{
		capability.FeatureMediaLivePhoto,
		capability.FeaturePollQuiz,
		capability.FeaturePollMultipleCorrect,
	} {
		if !disabled[feature] {
			t.Fatalf("%s was not disabled by tool policy", feature)
		}
	}
	if disabled[capability.FeaturePollRegular] {
		t.Fatal("regular poll should remain enabled")
	}
}
