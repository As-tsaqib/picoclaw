# 03 — Channel Capability Architecture

## 1. Problem statement

PicoClaw currently learns channel behavior through a mixture of:

- concrete adapter code;
- Go interface assertions such as streaming/structured-content capability;
- tool availability;
- model prior knowledge.

That is insufficient for a rapidly evolving platform API.

The LLM must not answer “Telegram supports X” and then assume PicoClaw can execute X. Runtime capability must be an explicit system fact.

## 2. Required abstraction

Introduce a declarative capability model that can answer:

```text
For channel C, account A, route R, and current runtime/server state:
- Is capability X supported?
- Is it unsupported?
- Is it conditional, and what trusted condition is missing/present?
```

### 2.1 Minimum capability states

At minimum support:

- `supported`
- `unsupported`
- `conditional`

An implementation may additionally distinguish:

- `temporarily_unavailable`
- `unknown`

but fallback behavior must remain deterministic.

### 2.2 Capability IDs

Capability IDs should be stable, semantic, and channel-neutral where possible.

Recommended namespace:

```text
message.text
message.edit
message.stream.text
message.structured.rich
message.stream.rich
keyboard.inline
keyboard.reply
poll.regular
poll.quiz
poll.multiple_correct
poll.media
poll.stop
poll.answers
message.ephemeral
media.image
media.video
media.audio
media.voice
media.document
media.group
media.animation
media.sticker
media.video_note
media.live_photo
location.point
location.venue
contact.card
dice.animated
checklist.native
```

Platform-specific capabilities are allowed only when there is no meaningful cross-channel semantic equivalent.

## 3. Per-channel vs per-route capability

A capability must not be assumed to be globally identical for every Telegram route.

Examples:

- rich message draft may be limited to specific chat type;
- polls cannot be sent to some direct-message route classes;
- ephemeral delivery depends on callback/receiver route metadata;
- checklist requires a connected business account;
- message effects may be private-chat-only;
- country/member poll restrictions may be channel-specific.

Therefore expose a resolver conceptually similar to:

```text
Capabilities(channel, account, trusted outbound context) → CapabilitySet
```

The exact Go interface may differ.

## 4. Trusted capability inputs

Capability resolution may use:

- channel adapter type;
- configuration;
- current chat/route type;
- trusted inbound metadata;
- trusted account/business context;
- SDK/build capability;
- bounded runtime evidence from method failures.

It must **not** trust:

- arbitrary model-provided channel IDs;
- arbitrary user-provided “business_connection_id”;
- memory text claiming a feature is available;
- an LLM's remembered Bot API version.

## 5. Upstream API version is not the runtime contract

Do not model Telegram capability as simply:

```text
bot_api_version >= 10.2
```

PicoClaw supports configurable `BaseURL`, so a self-hosted/custom Bot API endpoint may lag official Telegram behavior or differ operationally.

There is no requirement in this spec to invent a fragile version-probe endpoint.

Preferred behavior:

1. compile-time/SDK says the method/field is representable;
2. adapter declares optimistic support where appropriate;
3. on a clear platform “method not found/unsupported field” class of response, mark the specific capability unavailable for a bounded TTL for that Telegram account/base-URL identity;
4. use fallback while downgraded;
5. allow retry after TTL or explicit refresh/restart;
6. do not downgrade unrelated capabilities.

The implementation must distinguish a genuine unsupported-method response from transient network/auth/rate-limit errors. A 401/403/429/timeout is **not** evidence that the capability does not exist.

## 6. Capability cache isolation

If runtime capability evidence is cached, isolate it by all semantically relevant factors, at least:

- channel type;
- account/config identity;
- normalized BaseURL/server identity;
- feature ID.

Do not include bot token plaintext in the key.

If secret/config identity affects isolation, use the same safe stable config-ref/digest patterns already accepted in PicoClaw; never log the secret input.

The cache must be:

- bounded in size or lifecycle;
- race-safe;
- TTL-bounded;
- testable with fake clock/server where feasible;
- resettable/refreshed without restarting unrelated accounts.

## 7. Capability exposure to tools

The tool registry/model context must not advertise a tool as executable when the route cannot support it and no meaningful fallback is implemented.

Two acceptable patterns:

### Pattern A — conditional tool schema/context

Register tools but inject route capability metadata and have the tool itself reject/fallback deterministically.

### Pattern B — per-turn capability-aware tool filtering

Only expose tools that are supported/conditionally usable for the current route.

Either is acceptable if:

- the model receives a truthful capability statement;
- a tool cannot bypass policy by supplying alternate raw IDs;
- unsupported calls return a concise structured reason;
- fallback does not silently change semantics.

## 8. Capability-aware system prompt

Provide a compact runtime section, generated from the actual route, conceptually:

```text
# Delivery capabilities
channel=telegram
native_quiz=supported
native_poll=supported
rich_message=supported
rich_stream=supported
inline_keyboard=supported
checklist=conditional:business_connection
```

Avoid dumping hundreds of raw API names into the prompt.

The model should be told:

- use semantic tools when available;
- never invent raw Telegram API calls;
- native rendering is preferred when user preference requests it and capability is supported;
- if capability is unavailable, use declared fallback and state limitations only when material to the user.

## 9. Capability + memory + policy resolution

A rendering decision should conceptually be:

```text
user preference
    ∩
route capability
    ∩
security/policy
    ∩
content requirements
    =
selected delivery mode
```

Examples:

### Quiz

```text
presentation.quiz.mode=native
poll.quiz=supported
policy=allowed
→ send_quiz
```

### Native quiz requested but unavailable

```text
presentation.quiz.mode=native
poll.quiz=unsupported
fallback=text_quiz
→ structured/text fallback, no fake native success
```

### Contact

```text
contact.card=supported
but phone data not explicitly authorized for this target
→ do not send contact card
```

## 10. Fallback declaration

Each capability implementation must define a fallback class:

- `equivalent` — semantic meaning preserved;
- `degraded` — usable but user experience is reduced;
- `none` — operation must fail rather than lie/change semantics.

Examples:

- rich table → aligned text: degraded but acceptable;
- quiz → text quiz with answer reveal: degraded, if product policy permits;
- quiz → regular poll without correct answer: **not equivalent** and must never occur silently;
- contact card → plain phone text: may be privacy-sensitive; not automatic unless explicitly allowed;
- checklist requiring business context → no automatic impersonation fallback.

## 11. Observability

Log capability downgrade/restore using sanitized structured fields:

- channel/account config ref or safe digest;
- base URL host/safe identity;
- capability ID;
- error class;
- TTL/next probe time.

Never log:

- bot token;
- Authorization header;
- full sensitive request body;
- contact/vCard contents;
- arbitrary poll answer user data unless needed and redacted.

## 12. Compatibility

Existing channel interfaces may remain for strongly typed call sites. The new capability system does not have to replace every Go interface.

It should provide a single truthful **declarative view** over those implementation facts so the agent/policy layer can reason about delivery without importing Telegram SDK types.
