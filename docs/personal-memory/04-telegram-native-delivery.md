# 04 — Telegram Native Delivery

## 1. Upstream contract to target

At spec-authoring time the official Telegram Bot API current release is **10.2** (2026-07-14).

The implementation agent must re-verify current official Bot API and the Telego version used by the repository before coding. If Telegram is newer, incorporate compatible safe changes when they fit this design instead of pinning behavior to stale assumptions.

Important compatibility facts for this project:

- Native quiz is **not** a separate Telegram method. It is `sendPoll` with `type="quiz"`.
- Since Bot API 9.6 the correct-answer field/parameter is `correct_option_ids` (plural), supporting multiple correct answers.
- PicoClaw semantic tool `send_quiz` is therefore a convenience capability mapped to `SendPoll`.

## 2. P0 — Native Poll and Native Quiz

### 2.1 Channel-neutral poll payload

Add a channel-neutral poll representation. Exact struct placement may follow current architecture, but it must not import Telegram SDK types into generic bus/tool packages.

Minimum semantic fields:

```text
mode: regular | quiz
question
options[]
is_anonymous
allows_multiple_answers
allows_revoting
shuffle_options
allow_adding_options
hide_results_until_closes
members_only
country_codes[]
correct_option_ids[]
explanation
open_period / close_date
is_closed (if supported at send time)
description
optional media / option media / explanation media where supported
fallback text
```

Only include upstream fields that are actually supported by the verified SDK/API. Keep the model semantic, not raw JSON-oriented.

### 2.2 Validation before Telegram call

Validate in PicoClaw before SDK invocation.

Current Bot API 10.2 constraints to re-verify include:

- question: 1–300 chars after entity parsing;
- answer options: 1–12;
- option text: 1–100 chars;
- `correct_option_ids`: monotonically increasing, unique, 0-based, in range, required for quiz;
- explanation: 0–200 chars and current line-feed constraint;
- poll description: current API limit;
- `allow_adding_options` not supported for anonymous polls or quizzes;
- `members_only` and country restrictions only where upstream route semantics allow;
- poll cannot be sent to route classes Telegram explicitly disallows.

Do not rely on Telegram returning 400 for routine input validation that PicoClaw can catch locally.

### 2.3 Regular poll semantics

`send_poll` should default to regular poll unless mode is explicitly quiz.

Do not accept `correct_option_ids` as a hidden grading mechanism in regular mode unless current Bot API semantics explicitly allow/define it.

### 2.4 Quiz semantics

`send_quiz` must:

- force semantic mode `quiz`;
- require at least one correct option;
- support multiple correct options where current API supports it;
- sort/canonicalize correct IDs or reject unsorted input consistently;
- never serialize legacy `correct_option_id`;
- validate explanation before send;
- preserve user-selected anonymous/revoting/multiple-answer behavior only where valid for quiz.

### 2.5 Native grading UX

PicoClaw should let Telegram own the native quiz presentation/grading behavior.

Do not simulate a native quiz using inline buttons when `poll.quiz` is available.

A text/inline fallback is acceptable only when native capability is unavailable, and the fallback must clearly preserve answer correctness semantics.

## 3. P0 — Poll lifecycle

### 3.1 `stopPoll`

Implement a safe poll stop action mapped to Telegram `stopPoll`.

The model must not stop arbitrary poll messages by guessing chat/message IDs.

Preferred authorization:

- poll created by PicoClaw is registered server-side with opaque/internal identity;
- stop action resolves to the original trusted route and sent message ID;
- caller/session ownership is validated;
- stale/missing poll produces a bounded error.

### 3.2 Outbound poll state registry

If inbound answer handling or stop-by-semantic-ID is implemented, maintain a bounded registry containing only necessary state, for example:

```text
poll_id / outbound handle
channel/account
chat/topic
agent/session scope
creator/owner identity if relevant
message ID
ephemeral/native route metadata if applicable
created/closed timestamps
question/options metadata necessary for score mapping
correct option IDs for quiz if needed
```

Do not persist raw credentials or full unrelated conversation data.

Registry lifecycle must prevent unbounded growth. Polls may outlive process restarts; decide explicitly whether persistence is required by the desired user feature. If score/continuation must survive restart, use a safe durable state store. If not, document that answer tracking is best-effort while Telegram's own native grading continues independently.

### 3.3 Inbound `poll` updates

Handle Bot API poll updates when needed to refresh:

- closed/open state;
- option set changes (including modern open-answer option additions/deletions if exposed);
- total vote metadata when relevant.

Do not inject vote data into the LLM by default if no user task requires it.

### 3.4 Inbound `poll_answer`

For non-anonymous polls, map answers to the correct registered poll/session where needed.

Security requirements:

- match by trusted poll ID;
- validate registered channel/account/session/agent scope;
- do not trust model-supplied user IDs;
- do not cross-contaminate answers from two active quizzes;
- handle retracted/revoted answers;
- support `option_persistent_ids` when relevant to modern dynamic polls, while preserving normal `option_ids` compatibility;
- no data race under concurrent answers.

### 3.5 Anonymous polls

Do not promise per-user scoring/tracking for anonymous polls when the platform does not provide a reliable voter identity.

Telegram's own quiz result UI can still function. PicoClaw must distinguish platform-native grading from server-side user score tracking.

## 4. P0 — Rich Message expansion

### 4.1 Preserve existing builder

Existing title/paragraph/table rendering and fallback must continue to work.

### 4.2 Add channel-neutral rich blocks

Support a typed block model capable of representing the safe current rich structures, at minimum:

- paragraph
- section heading
- preformatted/code
- footer
- divider
- mathematical expression
- anchor
- list
- block quotation
- pull quotation
- details/disclosure
- table

Where SDK support is verified, extend to:

- map
- animation
- audio
- photo
- video
- voice note
- collage
- slideshow

`thinking` is draft-only and must never be emitted in a final persisted Rich Message if upstream forbids it.

### 4.3 Backward compatibility

Existing `StructuredContent.Title`, `Paragraphs`, and `Tables` must remain usable.

A migration-compatible approach may translate legacy fields into new block primitives internally.

Do not force every existing caller to construct low-level rich blocks.

### 4.4 Limits

Enforce upstream rich-message limits before send/fallback. At Bot API 10.2 these include limits such as total UTF-8 content, block count/nesting, media attachments, and table columns. Re-verify exact current limits at implementation time.

If a structure exceeds native limits:

- split only when semantics remain valid;
- otherwise fall back to current text/table renderer;
- never panic or silently truncate correctness-critical content.

## 5. P0 — Rich streaming

### 5.1 Preserve current text draft streaming

`SendMessageDraft` support already exists. Do not regress it.

### 5.2 Add `SendRichMessageDraft`

When the response is being streamed as rich content and route capability allows it:

```text
partial rich content
→ SendRichMessageDraft(draft_id)
→ update same draft ID as stream progresses
→ final complete content
→ SendRichMessage
```

The draft is temporary; final send is mandatory.

### 5.3 Thinking block

If implementing Telegram's rich `thinking` block:

- draft only;
- no private chain-of-thought content;
- use only a generic status label/placeholder, not internal reasoning tokens;
- final message must not contain the thinking block.

### 5.4 Fallback

If rich draft is unavailable but text draft is supported:

- stream readable text draft;
- send final native Rich Message if available.

If all draft streaming is unavailable:

- continue final message delivery without failing the turn.

A draft failure must not prevent final response unless the final send also fails.

## 6. P1 — Safe native outbound parity

These user-facing native message types are mandatory P1 targets because Telegram/Telego can represent them and PicoClaw currently lacks explicit delivery paths.

### 6.1 Animation

Map GIF/animation-like media to native `SendAnimation` when semantically correct.

Preserve existing generic document/video fallback where native animation is unsupported.

### 6.2 Sticker

Support `SendSticker` for trusted media refs/file IDs/URLs according to current media policy.

Do not add sticker-set management/admin APIs as part of this feature.

### 6.3 Video note

Support rounded video-note delivery through `SendVideoNote`.

Validate file/media type/size/duration according to current API before send where feasible.

### 6.4 Live photo

Support `SendLivePhoto` through a typed paired photo/video representation.

Do not guess pairing from arbitrary two attachments without explicit payload semantics.

### 6.5 Location

Support native point location through `SendLocation`.

Coordinates must pass range validation. Live-location editing can be audited as a safe follow-up capability, but do not force it if it creates unrelated complexity.

### 6.6 Venue

Support native venue through `SendVenue` with validated coordinates and required title/address.

### 6.7 Contact

Support native contact cards through `SendContact`, subject to the privacy/authorization rules in `06-security-privacy-fallback.md`.

### 6.8 Dice

Support `SendDice` with an allowlist of Telegram-supported dice emoji verified against the current API.

The result value is platform-generated; the model must not claim a predetermined result before send.

## 7. P1 — Common delivery options

Introduce channel-neutral delivery options where safely reusable:

- silent notification (`disable_notification`)
- content protection (`protect_content`)
- reply targeting
- thread/topic targeting from trusted route

Do **not** expose `allow_paid_broadcast` to the LLM or default it on.

Message effects and suggested-post controls should remain unexposed unless a later scoped feature has a strong use case and policy.

## 8. Reply UI / keyboards

Existing inline keyboards must remain.

Audit and, where cleanly supportable, add channel-neutral control for:

- basic reply keyboard with text choices;
- reply keyboard removal;
- force reply.

Sensitive request buttons such as request-contact/request-location require explicit consent/policy and are not automatically included just because the API supports them.

WebApp/URL buttons require URL scheme/domain validation per security doc.

## 9. Conditional — Native Checklist

Telegram `sendChecklist` currently requires a `business_connection_id` for a connected business account.

Therefore:

- capability state is `conditional`, not globally supported;
- trusted business connection context must come from runtime/session/channel metadata;
- LLM cannot set or invent the business connection ID;
- tool is hidden/rejected when trusted business context is absent;
- do not fall back by impersonating a business account;
- a text checklist fallback may be used only as a clear degraded representation.

## 10. Existing Telegram features that must not regress

- normal `SendMessage`
- Markdown/HTML fallback path
- `SendMessageDraft`
- `SendRichMessage`
- existing table rendering/fallback
- photo/video/audio/document/voice
- media group
- ephemeral send/edit/delete
- callback query handling
- command registration
- `/session` native dashboard
- `/model` native dashboard
- callback token size/scope security
- topic/private-topic routing
- custom BaseURL/proxy behavior

## 11. Features explicitly not exposed by this upgrade

Even if the SDK/API supports them, do not give the LLM generic access to:

- invoice/payment/Stars spending;
- paid media;
- paid broadcast;
- gifts;
- managed-bot token operations;
- chat ownership/admin/moderation;
- delete arbitrary user messages;
- arbitrary forward/copy to a different chat;
- business impersonation without trusted context;
- raw method name + JSON request execution.

## 12. Error behavior

Tool/channel errors should be classified enough to distinguish:

- validation error;
- unsupported capability;
- conditional capability missing trusted context;
- transient Telegram/network failure;
- rate limit;
- authorization failure;
- delivery rejected due route semantics.

Do not leak raw sensitive upstream bodies.

For an unsupported native feature with defined fallback, adapter/policy may use fallback. For a validation/security failure, do not silently fallback in a way that bypasses the rejected constraint.
