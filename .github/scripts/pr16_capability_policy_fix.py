from pathlib import Path


def replace_once(path: str, old: str, new: str) -> None:
    p = Path(path)
    text = p.read_text()
    if old not in text:
        if new in text:
            return
        raise SystemExit(f"{path}: expected snippet not found: {old[:120]!r}")
    if text.count(old) != 1:
        raise SystemExit(f"{path}: expected exactly one match, found {text.count(old)}")
    p.write_text(text.replace(old, new, 1))


# Explicit tool policy knobs for every new safe semantic Telegram tool.
replace_once(
    "pkg/config/config.go",
    '''\tSendFile        ToolConfig         `json:"send_file"         yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_FILE_"`
\tSendPoll        ToolConfig         `json:"send_poll"         yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_POLL_"`
''',
    '''\tSendFile        ToolConfig         `json:"send_file"         yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_FILE_"`
\tSendAnimation   ToolConfig         `json:"send_animation"    yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_ANIMATION_"`
\tSendSticker     ToolConfig         `json:"send_sticker"      yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_STICKER_"`
\tSendVideoNote   ToolConfig         `json:"send_video_note"   yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_VIDEO_NOTE_"`
\tSendLivePhoto   ToolConfig         `json:"send_live_photo"   yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_LIVE_PHOTO_"`
\tSendLocation    ToolConfig         `json:"send_location"     yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_LOCATION_"`
\tSendContact     ToolConfig         `json:"send_contact"      yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_CONTACT_"`
\tSendDice        ToolConfig         `json:"send_dice"         yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_DICE_"`
\tSendPoll        ToolConfig         `json:"send_poll"         yaml:"-"                                                       envPrefix:"PICOCLAW_TOOLS_SEND_POLL_"`
''')
replace_once(
    "pkg/config/config.go",
    '''\tcase "send_file":
\t\treturn t.SendFile.Enabled
\tcase "send_poll":
''',
    '''\tcase "send_file":
\t\treturn t.SendFile.Enabled
\tcase "send_animation":
\t\treturn t.SendAnimation.Enabled
\tcase "send_sticker":
\t\treturn t.SendSticker.Enabled
\tcase "send_video_note":
\t\treturn t.SendVideoNote.Enabled
\tcase "send_live_photo":
\t\treturn t.SendLivePhoto.Enabled
\tcase "send_location":
\t\treturn t.SendLocation.Enabled
\tcase "send_contact":
\t\treturn t.SendContact.Enabled
\tcase "send_dice":
\t\treturn t.SendDice.Enabled
\tcase "send_poll":
''')

replace_once(
    "pkg/config/defaults.go",
    '''\t\t\tSendFile: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendPoll: ToolConfig{
''',
    '''\t\t\tSendFile: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendAnimation: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendSticker: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendVideoNote: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendLivePhoto: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendLocation: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendContact: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendDice: ToolConfig{
\t\t\t\tEnabled: true,
\t\t\t},
\t\t\tSendPoll: ToolConfig{
''')

# Capability resolver accepts runtime policy input without importing config.
replace_once(
    "pkg/capability/capability.go",
    '''\tHasBusinessContext bool
\tIsEphemeral        bool
}
''',
    '''\tHasBusinessContext bool
\tIsEphemeral        bool
\tDisabledFeatures   map[Feature]bool
}
''')
replace_once(
    "pkg/capability/capability.go",
    '''\t\tset[FeatureKeyboardReply] = CapabilityInfo{State: StateUnsupported, Condition: "not_implemented"}
\t\tset[FeatureChecklistNative] = CapabilityInfo{State: StateConditional, Condition: "context_unavailable"}
\t\tset[FeaturePollMedia] = CapabilityInfo{State: StateUnsupported, Condition: "not_implemented"}
\t} else if channel == "pico" {
''',
    '''\t\tset[FeatureKeyboardReply] = CapabilityInfo{State: StateUnsupported, Condition: "not_implemented"}
\t\tset[FeatureChecklistNative] = CapabilityInfo{State: StateConditional, Condition: "context_unavailable"}
\t\tset[FeaturePollMedia] = CapabilityInfo{State: StateUnsupported, Condition: "not_implemented"}

\t\tfor feature, disabled := range route.DisabledFeatures {
\t\t\tif !disabled {
\t\t\t\tcontinue
\t\t\t}
\t\t\tif _, declared := set[feature]; declared {
\t\t\t\tset[feature] = CapabilityInfo{State: StateUnsupported, Condition: "disabled_by_policy"}
\t\t\t}
\t\t}
\t} else if channel == "pico" {
''')

# Central mapping from admin tool policy to semantic capabilities.
replace_once(
    "pkg/agent/pipeline_llm.go",
    '''func filterToolsByRouteCapabilities(
''',
    '''func capabilityPolicyDisabledFeatures(cfg *config.Config) map[capability.Feature]bool {
\tif cfg == nil {
\t\treturn nil
\t}
\tdisabled := make(map[capability.Feature]bool)
\tdisable := func(tool string, features ...capability.Feature) {
\t\tif cfg.Tools.IsToolEnabled(tool) {
\t\t\treturn
\t\t}
\t\tfor _, feature := range features {
\t\t\tdisabled[feature] = true
\t\t}
\t}
\tdisable("send_poll", capability.FeaturePollRegular)
\tdisable("send_quiz", capability.FeaturePollQuiz, capability.FeaturePollMultipleCorrect)
\tdisable("stop_poll", capability.FeaturePollStop)
\tdisable("send_location", capability.FeatureLocationPoint, capability.FeatureLocationVenue)
\tdisable("send_contact", capability.FeatureContactCard)
\tdisable("send_dice", capability.FeatureDiceAnimated)
\tdisable("send_live_photo", capability.FeatureMediaLivePhoto)
\tdisable("send_animation", capability.FeatureMediaAnimation)
\tdisable("send_sticker", capability.FeatureMediaSticker)
\tdisable("send_video_note", capability.FeatureMediaVideoNote)
\tif len(disabled) == 0 {
\t\treturn nil
\t}
\treturn disabled
}

func filterToolsByRouteCapabilities(
''')
replace_once(
    "pkg/agent/pipeline_llm.go",
    '''\t\tSenderID:    ts.opts.Dispatch.SenderID(),
\t\tIsEphemeral: turnStateIsPrivate(ts),
\t}
''',
    '''\t\tSenderID:         ts.opts.Dispatch.SenderID(),
\t\tIsEphemeral:      turnStateIsPrivate(ts),
\t\tDisabledFeatures: capabilityPolicyDisabledFeatures(al.cfg),
\t}
''')
# The same route literal appears twice; after the first replacement, patch the remaining copy.
replace_once(
    "pkg/agent/pipeline_llm.go",
    '''\t\tSenderID:    ts.opts.Dispatch.SenderID(),
\t\tIsEphemeral: turnStateIsPrivate(ts),
\t}
''',
    '''\t\tSenderID:         ts.opts.Dispatch.SenderID(),
\t\tIsEphemeral:      turnStateIsPrivate(ts),
\t\tDisabledFeatures: capabilityPolicyDisabledFeatures(al.cfg),
\t}
''')

# Carry the same policy into the capability prompt.
replace_once(
    "pkg/agent/prompt.go",
    '''\t"sync"

\t"github.com/As-tsaqib/picoclaw/pkg/logger"
''',
    '''\t"sync"

\t"github.com/As-tsaqib/picoclaw/pkg/capability"
\t"github.com/As-tsaqib/picoclaw/pkg/logger"
''')
replace_once(
    "pkg/agent/prompt.go",
    '''\tServerID          string
\tSenderDisplayName string
''',
    '''\tServerID             string
\tDisabledCapabilities map[capability.Feature]bool
\tSenderDisplayName    string
''')
replace_once(
    "pkg/agent/prompt_turn.go",
    '''\treq.PrivateContext = private
''',
    '''\treq.PrivateContext = private
\treq.DisabledCapabilities = capabilityPolicyDisabledFeatures(cfg)
''')
replace_once(
    "pkg/agent/prompt_contributors.go",
    '''\t\tServerID:    req.ServerID,
\t\tIsEphemeral: req.PrivateContext,
''',
    '''\t\tServerID:         req.ServerID,
\t\tIsEphemeral:      req.PrivateContext,
\t\tDisabledFeatures: req.DisabledCapabilities,
''')

# Targeted capability-policy tests.
p = Path("pkg/config/native_telegram_tools_test.go")
p.write_text('''package config

import "testing"

func TestDefaultConfigEnablesSafeTelegramSemanticTools(t *testing.T) {
\tcfg := DefaultConfig()
\ttools := []string{
\t\t"send_animation", "send_sticker", "send_video_note", "send_live_photo",
\t\t"send_location", "send_contact", "send_dice",
\t}
\tfor _, name := range tools {
\t\tif !cfg.Tools.IsToolEnabled(name) {
\t\t\tt.Fatalf("%s should default enabled", name)
\t\t}
\t}
\tcfg.Tools.SendLivePhoto.Enabled = false
\tif cfg.Tools.IsToolEnabled("send_live_photo") {
\t\tt.Fatal("send_live_photo policy toggle was ignored")
\t}
}
''')

p = Path("pkg/capability/capability_policy_test.go")
p.write_text('''package capability

import "testing"

func TestResolveRouteCapabilitiesAppliesAdminPolicy(t *testing.T) {
\tset := ResolveRouteCapabilities(RouteContext{
\t\tChannel: "telegram",
\t\tDisabledFeatures: map[Feature]bool{
\t\t\tFeatureMediaLivePhoto:      true,
\t\t\tFeaturePollQuiz:           true,
\t\t\tFeaturePollMultipleCorrect: true,
\t\t},
\t}, nil)
\tfor _, feature := range []Feature{FeatureMediaLivePhoto, FeaturePollQuiz, FeaturePollMultipleCorrect} {
\t\tinfo := set[feature]
\t\tif info.State != StateUnsupported || info.Condition != "disabled_by_policy" {
\t\t\tt.Fatalf("%s = %#v, want unsupported:disabled_by_policy", feature, info)
\t\t}
\t}
\tif !set.IsSupported(FeaturePollRegular) {
\t\tt.Fatal("unrelated capability was disabled")
\t}
}
''')

p = Path("pkg/agent/capability_policy_test.go")
p.write_text('''package agent

import (
\t"testing"

\t"github.com/As-tsaqib/picoclaw/pkg/capability"
\t"github.com/As-tsaqib/picoclaw/pkg/config"
)

func TestCapabilityPolicyDisabledFeaturesMatchesToolConfig(t *testing.T) {
\tcfg := config.DefaultConfig()
\tcfg.Tools.SendLivePhoto.Enabled = false
\tcfg.Tools.SendQuiz.Enabled = false
\tdisabled := capabilityPolicyDisabledFeatures(cfg)
\tfor _, feature := range []capability.Feature{
\t\tcapability.FeatureMediaLivePhoto,
\t\tcapability.FeaturePollQuiz,
\t\tcapability.FeaturePollMultipleCorrect,
\t} {
\t\tif !disabled[feature] {
\t\t\tt.Fatalf("%s was not disabled by tool policy", feature)
\t\t}
\t}
\tif disabled[capability.FeaturePollRegular] {
\t\tt.Fatal("regular poll should remain enabled")
\t}
}
''')
