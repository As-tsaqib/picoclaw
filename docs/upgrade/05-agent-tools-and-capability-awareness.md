# 05 — Agent Tools and Capability-Aware Behavior

## 1. Principle

The LLM should operate on **semantic PicoClaw tools**, not raw platform HTTP APIs.

The model's knowledge of Telegram documentation is not an execution capability.

Correct behavior:

```text
model intent: "send a quiz"
→ send_quiz semantic tool
→ channel-neutral payload
→ policy + capability validation
→ Telegram adapter
→ SendPoll(type=quiz)
```

Incorrect behavior:

```text
model outputs:
POST https://api.telegram.org/bot<TOKEN>/sendPoll
{...}
```

and expects PicoClaw to execute it.

## 2. Required explicit tools

### 2.1 `send_poll`

Expose a semantic tool for regular/native polls.

Suggested schema concepts:

```text
question: string
options: array of option objects or strings
anonymous?: bool
multiple_answers?: bool
revoting?: bool
shuffle?: bool
allow_adding_options?: bool
hide_results_until_closes?: bool
members_only?: bool
country_codes?: string[]
description?: string
open_period_seconds?: int
close_at?: timestamp
media?: structured media (only if supported)
channel?: optional existing trusted target semantics
chat_id?: optional only if existing message-tool cross-target policy permits
reply_to_message_id?: optional
```

Do not expose Telegram-only raw field names if a cleaner semantic name exists, but map deterministically.

### 2.2 `send_quiz`

Expose a separate convenience tool because quiz has different correctness semantics and is a common personal-agent use case.

Suggested schema:

```text
question: string
options: array
correct_options: array of zero-based indices OR stable local option keys
explanation?: string
anonymous?: bool
multiple_answers?: bool
revoting?: bool
shuffle?: bool
description?: string
open_period_seconds?: int
close_at?: timestamp
media?: structured media
channel/chat target only under existing trusted targeting rules
```

Implementation must convert `correct_options` into modern `correct_option_ids`.

The LLM should not need to know the upstream JSON field name.

### 2.3 Stop/close poll action

Provide an action such as:

```text
stop_poll(poll_handle)
```

Prefer an opaque server-side poll handle rather than arbitrary `chat_id + message_id` supplied by the model.

If the user explicitly references a poll message, resolve it through trusted inbound/reply metadata when possible.

## 3. Tools for other native types

Do not create dozens of tiny tools unless that matches current PicoClaw tool architecture.

Two acceptable patterns:

### Pattern A — typed `message` extension

Extend the existing message tool with a typed `native`/`content_type` union for safe content:

```text
animation
sticker
video_note
live_photo
location
venue
contact
dice
```

### Pattern B — small semantic native tools

Use separate tools where validation or side effects are materially distinct, e.g. `send_location`, `send_contact`, `roll_dice`.

Selection rule:

- prefer schema clarity;
- avoid raw platform structs;
- avoid optional-field explosion;
- preserve existing `message` tool compatibility;
- do not duplicate routing/security logic.

## 4. Trusted default route

All outbound tools should default to the current verified inbound/session route.

The model must not be required to provide:

- Telegram receiver user ID;
- callback query ID;
- message-thread route ID if already known;
- business connection ID;
- canonical memory person key;
- session capability token.

These are populated from trusted runtime context.

## 5. Cross-chat targeting

The existing message tool may allow an explicit channel/chat target under configured semantics.

This upgrade must **not broaden** cross-chat authority simply because new native tools exist.

If `send_quiz` or `send_location` supports explicit target fields, it must reuse the same authorization/routing policy as current outbound message sending.

Do not introduce a second target-resolution implementation.

## 6. Capability-aware tool behavior

Before invoking an adapter-specific native send:

1. resolve current target route;
2. resolve capability set;
3. validate policy;
4. validate payload;
5. select native delivery or declared fallback;
6. execute with caller context/deadline;
7. return truthful result to LLM.

Tool success should report the semantic result, not expose raw token/server internals.

Example:

```text
Native quiz sent (poll_handle=...)
```

The opaque handle may be returned only if useful for later stop/score operations.

## 7. Agent system prompt behavior

Add a compact delivery policy section explaining:

- the current route's relevant capabilities;
- semantic tools to use;
- native-preference resolution;
- fallback behavior;
- prohibition on fabricating raw Telegram API calls.

Avoid permanent hardcoded claims like:

> Telegram supports API version 10.2.

Instead state runtime facts, e.g.:

```text
Current route capabilities: native quiz, native poll, rich message, inline buttons.
```

## 8. Memory preference integration

The agent should consult the compiled profile for durable presentation preferences.

Recommended keys:

```text
presentation.quiz.mode = auto|native|text
presentation.poll.mode = auto|native|text
presentation.rich_content = native_preferred|plain
interaction.buttons = native_preferred|plain
```

### 8.1 `auto`

Choose the best channel-native representation when available and appropriate.

### 8.2 `native`

Prefer native capability. If unavailable, use declared degraded fallback rather than pretending native success.

### 8.3 `text`

Respect a user's explicit preference for non-native text even when native capability exists, unless a task explicitly overrides it.

## 9. Intent precedence

Per-turn explicit user instruction overrides durable preference.

Example:

Stored:

`presentation.quiz.mode=native`

Current user:

> Buat soalnya dalam satu teks saja, jangan pakai poll.

Expected: text quiz for this turn; do not overwrite durable preference unless the user explicitly changes the preference.

## 10. Avoid capability hallucination

When the user asks:

> Bisa bikin native quiz?

If capability is actually supported on current route, the agent should say yes and may demonstrate it when asked.

If not supported, say the current runtime/route cannot send native quiz and explain fallback briefly.

Do not answer solely from LLM prior knowledge.

## 11. Safe automatic native rendering

The agent may automatically choose native rendering when all are true:

- the user requested content whose native representation is clearly better;
- capability is supported;
- policy permits it;
- user preference is `auto` or compatible;
- native action does not incur payment/spending/admin side effects.

Examples:

- quiz → native quiz;
- “share this location” → native location if coordinates known;
- “roll a dice” → Telegram dice;
- “send this as sticker” → native sticker if compatible media exists.

Do not automatically turn ordinary prose into unrelated polls or native objects just because capability exists.

## 12. Tool result and final-response suppression

Audit current “message tool sent in round” logic.

New native-send tools must integrate with response finalization so that:

- successful native quiz/poll does not cause the model to duplicate the entire quiz as a second text response unless useful;
- a native send to a different conversation does not suppress the final response in the current conversation;
- delivery acknowledgement/checkpoint state records what was actually sent;
- failed native send with fallback records fallback success, not native success.

Prefer a generalized per-round delivery record over adding independent boolean flags for every new native tool.

## 13. Tool configuration

Follow existing `cfg.Tools.IsToolEnabled` patterns where appropriate.

Recommended behavior:

- safe native delivery tools enabled by default only if that matches existing message-tool policy;
- allow explicit disabling per deployment;
- capability may still be unsupported even when tool is enabled;
- disabling `send_quiz` must not break normal text responses.

Do not require users to configure API URLs or Telegram method names manually.

## 14. Tool docs should be self-correcting

Tool descriptions should explain semantic behavior and constraints, including:

- quiz may have one or multiple correct answers if platform supports it;
- poll/quiz fields are validated;
- native support depends on current route;
- raw Telegram IDs are not needed for current-route sends.

Do not embed stale legacy snippets such as `correct_option_id` in system prompts, tool descriptions, tests, examples, or docs.
