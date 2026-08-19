# 01 — Current State, Scope, and Product Goals

## 1. Status of prior work

The previous personal-memory feature is no longer hypothetical. At spec-authoring time, repository `main` was observed at:

`9cdb96ee63bce5ba9ff7aff9f3e6ef6445922339`

with merge commit message:

`feat(memory): add durable personal agent memory (#11)`

This SHA is **audit evidence only**, not a branch base contract. The coding agent must fetch latest `main` when implementation starts.

The new work must therefore **harden and extend the current implementation**, not recreate the personal-memory subsystem from the old design document.

## 2. Verified useful primitives already present

At the observed baseline, PicoClaw contains mature primitives that must be reused where sound:

### Personal memory

- `pkg/memory/CuratedStore`
- structured curated entries and evidence semantics
- `preference_key` / `preference_value`
- supersession/reconciliation for structured preferences
- compiled `UserProfileSnapshot`
- bounded curated retrieval
- recall/history store
- task checkpoints
- background reviewer/review state
- migration support
- current-user management API/dashboard

### Session / identity

- opaque session keys
- structured session scope
- `session.identity_links`
- `CanonicalSessionIdentityID`
- `CanonicalUserScopeKey`
- multi-session and topic-aware routing

### Telegram

- Telego SDK integration
- `SendMessage`
- `SendMessageDraft` streaming
- `SendRichMessage`
- photo/video/audio/document/voice
- media groups
- native inline keyboard
- structured `/session` and `/model` interactions
- ephemeral message send/edit/delete paths
- callback ownership/scope state machinery

### Channel abstraction

- `Channel`
- `StructuredContentCapable`
- streaming interfaces
- media delivery paths
- `OutboundMessage` / `OutboundMediaMessage`

The upgrade should fit these primitives instead of introducing parallel implementations.

## 3. Verified gaps that motivate v2

### 3.1 Runtime capability is not declarative

PicoClaw has interface checks for individual channel behaviors, but no general per-route capability contract that the LLM/tool layer can inspect.

Consequences:

- the model may know Telegram supports a feature but cannot tell whether PicoClaw exposes it;
- the model may describe raw API calls instead of invoking a runtime tool;
- custom/older Bot API servers are hard to distinguish from full official API capability;
- user preferences cannot safely choose “best native representation” without a capability resolver.

### 3.2 Native Poll / Quiz is missing end-to-end

At the observed baseline, there is no source use of:

- `SendPoll`
- `StopPoll`
- outbound structured poll/quiz payload
- agent `send_poll`/`send_quiz` tool
- poll-answer lifecycle mapping

Therefore a user can ask for Native Quiz and the LLM can only explain or simulate it using text.

### 3.3 Rich Message coverage is partial

PicoClaw already calls `SendRichMessage`, but its structured builder primarily covers:

- section heading/title
- paragraph
- table
- inline keyboard attached to the message

Telegram Bot API 10.2 has a much larger native rich-block surface. The upgrade should expand it in a controlled, channel-neutral way and preserve text fallback.

### 3.4 Rich streaming is not yet native-rich

PicoClaw already streams plain text using `SendMessageDraft`.

The missing upgrade is `SendRichMessageDraft` for routes/content that are genuinely rich. The final persistent message must still be sent after the temporary draft.

### 3.5 Dashboard current-user identity is dashboard-local

At the observed baseline, `dashboardCurrentUserCaller()` resolves the authenticated Pico dashboard user using:

- channel `pico`
- raw identity `pico-user`
- default account

Even if `identity_links` maps both a Telegram identity and the Pico identity to the same canonical alias, `CanonicalUserScopeKey()` still includes channel/account in the storage key. Thus Telegram and dashboard can remain separate current-user stores.

This is safe but does not yet satisfy the desired “one person, one personal profile across trusted linked channels” UX.

### 3.6 Prompt source duplication can occur

The compiled current-user profile is derived from curated entries, then current-user curated retrieval runs separately in the same turn. The profile's `SourceIDs` are not automatically excluded from Tier-1 retrieval. The same logical preference/fact can therefore appear twice in prompt context.

### 3.7 Memory update notification is event-oriented

A memory change currently schedules its own `Memory updated` outbound notification. Multiple foreground/reviewer changes associated with one logical turn can therefore create multiple notifications instead of one coalesced user-visible summary.

### 3.8 Preference keys are syntactically normalized, not centrally canonicalized

The current normalization lowercases/trims keys and validates syntax. It does not define a central alias vocabulary. Semantically identical keys such as old/new quiz-format names can remain separate unless callers happen to choose the same spelling.

## 4. Product goal

PicoClaw should become a **capability-aware personal agent**.

Example desired flow:

```text
User, Telegram DM:
"Kalau saya minta quiz, pakai Telegram Native Quiz."

    ↓ trusted personal-memory capture
presentation.quiz.mode = native

Later, different session/topic:
"Buatkan 10 soal Ajurrumiyah."

    ↓ compiled personal profile says native quiz preferred
    ↓ route capability resolver says poll.quiz=supported
    ↓ policy says allowed
    ↓ model invokes send_quiz
    ↓ channel-neutral quiz payload
    ↓ Telegram SendPoll(type=quiz)
    ↓ Telegram native grading UI
```

If the same request runs on a channel without native quiz:

```text
preference = native
capability = unsupported
policy = fallback allowed
→ use declared quiz text/interactive fallback
→ do not claim a native poll was sent
```

## 5. Scope — mandatory

### Memory/personalization hardening

- trusted cross-channel person scope
- safe migration/consolidation from existing channel-scoped personal memory
- dashboard owner/profile binding and truthful UI labels
- canonical preference-key aliases/vocabulary
- Tier-0/Tier-1 same-source prompt deduplication
- per-turn memory notification coalescing
- preservation of existing capture, evidence, privacy, retrieval, checkpoint, recall, and reviewer semantics

### Capability architecture

- declarative capability IDs/state
- channel + route-specific resolution
- capability exposure to agent/tool context
- bounded runtime downgrade for custom/old Bot API endpoints
- deterministic fallback

### Telegram mandatory native capability work

- regular poll
- quiz semantic wrapper
- current poll fields and validation
- stop poll
- inbound poll / poll-answer state handling where needed
- expanded rich blocks
- rich-message draft streaming
- safe outbound-native parity listed in `04-telegram-native-delivery.md`

### Regression preservation

No regression to:

- `/session`
- `/model`
- model/session isolation
- callback security
- ephemeral messages
- existing message streaming
- normal text/Markdown delivery
- existing media sending
- existing memory privacy/isolation
- non-Telegram channels

## 6. Out of scope unless a separate privileged design is approved

The following must not become generic LLM abilities as part of this upgrade:

- payment/invoice creation
- Telegram Stars spending
- `allow_paid_broadcast`
- paid media
- gifts
- managed-bot token creation/export/replacement
- arbitrary moderation/admin operations
- destructive chat management
- arbitrary message forwarding/copying across chats
- raw arbitrary Bot API method invocation
- arbitrary business-account selection
- arbitrary canonical-person selection from HTTP/tool input

These may be listed as upstream capabilities in an audit matrix, but status must be `out_of_scope_privileged`, not “forgotten implementation”.

## 7. General implementation principle

Do not maximize API coverage at the expense of architecture.

The preferred order is:

1. model a capability safely;
2. model a channel-neutral payload/action;
3. validate against trusted route context;
4. map it to Telego/Telegram;
5. provide deterministic fallback;
6. expose a semantic tool to the LLM only when policy permits;
7. test all scope/security boundaries.
