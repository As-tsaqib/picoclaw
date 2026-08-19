# 06 — Security, Privacy, Authorization, and Fallback

## 1. Security model

Native UX must not widen authority.

Every new capability must preserve the same or stronger boundaries as normal PicoClaw message delivery.

The primary rule is:

> Platform identifiers and authority come from trusted runtime context; content intent may come from the model.

## 2. Secrets

Never expose or persist:

- Telegram bot token;
- Authorization headers;
- OAuth/API keys;
- callback secrets;
- private route tokens;
- business-account secrets;
- raw credential-bearing error bodies.

Audit leakage through:

- logs;
- errors;
- tool results;
- prompt capability metadata;
- callback data;
- poll registry;
- memory store;
- dashboard responses;
- tests/fixtures;
- CI output.

## 3. Raw Bot API passthrough is prohibited

Do not add a tool like:

```text
telegram_api(method, json_body)
```

or let the model construct arbitrary Bot API URLs.

Reasons:

- bypasses route validation;
- bypasses capability policy;
- creates privilege escalation surface;
- can expose spending/admin/payment methods;
- turns upstream API drift into prompt-level correctness risk.

Only semantic, validated actions are in scope.

## 4. Route identity

For current-conversation delivery, resolve from trusted inbound/outbound context:

- channel/account;
- chat ID;
- topic/thread ID;
- session scope;
- agent;
- private/ephemeral route metadata.

Model-provided overrides must never bypass existing authorization.

## 5. Ephemeral fields

Telegram 10.2 supports `receiver_user_id` and `callback_query_id` for several outgoing ephemeral message types.

PicoClaw already has trusted ephemeral routing logic. Reuse it.

The LLM must **not** set:

- `receiver_user_id`;
- `callback_query_id`;
- ephemeral message ID.

When extending animation/sticker/video note/live photo/location/venue/contact to ephemeral routes, use the same server-side route token/ownership checks as existing ephemeral message/media delivery.

If an API method does not support ephemeral delivery, report capability/fallback accurately rather than spoofing a public send.

## 6. Business connection

`business_connection_id` is trusted platform context.

Rules:

- never model-selected;
- never request-query selected solely for convenience;
- never guessed from memory;
- only attached if current authenticated channel runtime proves it;
- conditional capabilities such as checklist remain unavailable otherwise.

## 7. Contact/vCard privacy

Native contacts can disclose phone numbers and personal identity information.

`send_contact`/contact payload is allowed only when at least one is true under implemented policy:

- the user explicitly provided the contact data in the current trusted turn and requested sharing/sending it;
- a trusted connected tool returned the data for this explicit task and policy allows delivery;
- the user explicitly asks to send their own stored contact and current target visibility is appropriate.

Do not silently pull private memory phone numbers into a shared/group contact card merely because the model thinks it is useful.

Do not include full vCard in logs or error messages.

## 8. Location privacy

Coordinates may be sensitive.

Rules:

- do not infer a user's live/current location from unrelated data;
- use coordinates explicitly provided or obtained from a trusted tool for the current task;
- sharing location to a different chat follows existing cross-target authorization;
- do not store transient coordinates as durable personal memory automatically;
- logs should avoid unnecessary precision if location values are not required for debugging.

## 9. Poll privacy

### Anonymous vs non-anonymous

Respect explicit choice. Do not silently set non-anonymous simply to enable tracking.

### Answer registry

If storing answer events:

- store only what the feature requires;
- bind to correct poll/session/owner;
- set retention/lifecycle;
- avoid surfacing individual answers in group output unless task/user intent requires it;
- do not persist answer data as personal memory by default.

### Quiz correctness

Correct-answer IDs are not credentials, but they may be task-sensitive before a quiz is answered. Do not unnecessarily print them in user-visible status/logs.

## 10. Callback and opaque state

Existing `/session` and `/model` callbacks use short opaque server-side state. Preserve that design for new interactive actions.

Telegram `callback_data` must remain within the platform byte limit.

Do not embed:

- raw session key;
- raw model ID when oversized/sensitive;
- canonical person key;
- poll answer key containing private data;
- credentials;
- business connection ID.

Validate sender/chat/topic/account/agent/session ownership on callback handling.

## 11. URLs and Web Apps

For URL buttons/rich links:

- validate scheme;
- reject dangerous local/file/javascript-like schemes;
- preserve current link sanitization if present.

For WebApp buttons/features:

- use configured trusted domain allowlist;
- do not allow arbitrary model-generated WebApp origins;
- account for Telegram's current Mini App origin/security rules.

WebApp expansion is not a mandatory deliverable unless already required by existing UX.

## 12. Media safety

Reuse existing MediaStore/workspace restrictions.

For local files:

- validate allowed path/workspace policy;
- enforce max size;
- avoid directory traversal;
- inspect/determine media type using existing safe mechanisms;
- do not let native-media support bypass media store lifecycle.

For HTTP media URLs:

- apply existing/private-host/SSRF policy if PicoClaw fetches the URL itself;
- if Telegram fetches directly, still validate scheme and avoid exposing local/internal URLs.

## 13. Spend-bearing fields are denied

Do not expose:

- `allow_paid_broadcast`;
- paid media;
- invoices/payment links created by generic tool;
- gifts/Stars spending.

Even if a boolean exists in Telego params, keep its zero/default value and prevent model/tool args from enabling it.

## 14. Admin/destructive operations are denied

This upgrade is a delivery capability project, not a moderation bot project.

Do not expose generic:

- ban/restrict/promote;
- delete arbitrary messages;
- delete chat history;
- change ownership;
- managed-bot token actions;
- chat configuration mutation.

Existing specific safe operations required by `/session`/`/model` remain unaffected.

## 15. Fallback policy

Fallback must be defined per capability.

### Native quiz

Allowed degraded fallback:

- structured/text quiz preserving all options and correctness semantics.

Forbidden silent fallback:

- convert quiz to regular poll and discard correct answers.

### Regular poll

Possible degraded fallback:

- text list or inline-button poll-like interaction only if result semantics are understood.

If vote aggregation cannot be preserved, the tool should say native poll is unavailable rather than claim equivalent voting.

### Rich message

Allowed fallback:

- Markdown/HTML/plain text;
- aligned text table.

### Rich draft

Allowed fallback:

- text draft;
- or no draft, final message only.

### Animation/sticker/video note/live photo

Allowed fallback depends on content:

- animation → video/document when readable;
- sticker → image/document if user asked only “send this file”, but not if explicitly asked for sticker semantics without notice;
- video note → normal video as degraded fallback if allowed;
- live photo → normal photo/video pair if explicit native semantics unavailable, with truthful tool result.

### Location/venue/contact

Do not automatically convert sensitive structured data to public text unless current task/user intent permits disclosure.

### Checklist

Can degrade to text checklist if user intent is satisfied, but must never fabricate business context.

## 16. Unsupported-method detection

A custom/older Telegram API server may reject a method.

Negative capability evidence must be based on an explicit unsupported/method-not-found class.

Do not negative-cache capability because of:

- network timeout;
- DNS failure;
- 429;
- 401/403 auth failure;
- transient 5xx;
- malformed payload due a PicoClaw bug.

A malformed payload is an implementation bug and must be fixed, not interpreted as unsupported API.

## 17. Context and cancellation

Every new send/stop/probe action must use the caller context.

If a bounded fallback/probe needs its own deadline, derive it from the caller rather than replacing an active request with unbounded `context.Background()`.

No goroutine may outlive the turn merely to wait for a Telegram request unless it has an explicit lifecycle owner and bounded cancellation.

## 18. Personal-memory privacy during native delivery

Native capabilities do not weaken personal-memory rules.

Examples:

- behavioral preference `presentation.quiz.mode=native` may influence a group answer silently;
- private fact such as phone number must not be inserted into a native contact card in group unless user explicitly requests it;
- dashboard person linkage must not allow arbitrary profile selection;
- capability status/error must never reveal another account's config identity or private memory.
