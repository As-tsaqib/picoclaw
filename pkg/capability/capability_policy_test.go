package capability

import "testing"

func TestResolveRouteCapabilitiesAppliesAdminPolicy(t *testing.T) {
	set := ResolveRouteCapabilities(RouteContext{
		Channel: "telegram",
		DisabledFeatures: map[Feature]bool{
			FeatureMediaLivePhoto:      true,
			FeaturePollQuiz:            true,
			FeaturePollMultipleCorrect: true,
		},
	}, nil)
	for _, feature := range []Feature{FeatureMediaLivePhoto, FeaturePollQuiz, FeaturePollMultipleCorrect} {
		info := set[feature]
		if info.State != StateUnsupported || info.Condition != "disabled_by_policy" {
			t.Fatalf("%s = %#v, want unsupported:disabled_by_policy", feature, info)
		}
	}
	if !set.IsSupported(FeaturePollRegular) {
		t.Fatal("unrelated capability was disabled")
	}
}
