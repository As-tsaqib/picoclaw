# 08 — Testing, Acceptance, and CI

## 1. Testing philosophy

Do not test only whether an SDK method was called.

Tests must prove:

- payload semantics;
- scope/identity;
- privacy;
- fallback;
- lifecycle;
- concurrency;
- persistence/migration where required;
- agent tool behavior;
- regression of existing features.

Prefer fakes/httptest servers around Telegram adapter requests where existing Telego test hooks make this feasible. Do not hit production Telegram in normal CI.

## 2. Baseline audit before modifying tests

Before coding:

1. inspect repository workflows;
2. identify current Go build tags;
3. identify frontend test/build commands;
4. identify Telegram adapter test helpers/fake Bot API server patterns;
5. identify memory migration fixtures;
6. identify integration/standalone workflow gates.

Use current repository commands, not stale commands from this spec if they changed.

## 3. Memory identity and migration tests

Minimum cases:

### 3.1 Unlinked identities remain separate

- Telegram user current-user store
- Pico dashboard user store
- no identity link

Expected: separate personal stores; dashboard label says dashboard-local.

### 3.2 Explicit trusted link unifies person

Config links both trusted runtime identities to canonical `owner`.

Expected:

- same person-scope key after resolver;
- cross-session/cross-channel stable profile;
- no arbitrary request parameter is required.

### 3.3 User A/User B isolation

A and B use same chat/group.

Expected:

- A personal preference does not appear in B profile/prompt;
- B cannot list/update/delete A;
- person-link resolver cannot collide them.

### 3.4 Dashboard impersonation rejection

Attempt to pass raw `user_id`, Telegram ID, `UserKey`, or canonical alias through management request.

Expected: ignored/rejected according to API contract; identity remains trusted config/auth-derived.

### 3.5 Migration merge

Create legacy/current channel-scoped stores for identities now linked to one person.

Cover:

- unique facts merge;
- exact duplicates dedup;
- same preference same value;
- same preference conflicting values with different evidence authority;
- same preference conflicting values with recency tie-break;
- unknown/legacy preference alias;
- restart/re-run migration idempotency;
- crash/interrupted migration recovery if storage layer supports simulation.

## 4. Preference canonicalization tests

Cover:

- recognized canonical key remains stable;
- legacy quiz-format alias maps to canonical `presentation.quiz.mode`;
- equivalent values map to `native|auto|text` as designed;
- unknown valid keys follow custom/legacy compatibility policy;
- cross-key `supersedes` remains rejected when invalid;
- one effective active state after alias migration;
- explicit evidence outranks inferred conflict.

## 5. Prompt dedup tests

Construct curated current-user entries that are selected into compiled profile.

Expected:

- profile is present;
- same source IDs are not redundantly emitted in Tier-1 current-user block;
- a separate relevant current-user fact not represented in profile can still appear in Tier-1;
- total prompt remains bounded;
- group privacy behavior remains unchanged;
- usage accounting does not count always-on profile every turn as Tier-1 retrieval presentation.

Add regression around real preference:

`presentation.quiz.mode=native`

It should appear once in effective prompt context.

## 6. Notification aggregation tests

Minimum:

- one foreground memory mutation → one notification;
- multi-mutation batch → one notification;
- foreground + background reviewer same turn → one notification;
- delayed reviewer correlated to same turn → no duplicate notification;
- independent later turn → separate notification;
- same session concurrent users/routes → no cross-coalescing;
- no-op/reaffirmation behavior per policy;
- failed write → no false success;
- pending approval → correct single pending summary;
- verbose preview limit/redaction;
- group private-memory update does not reveal sensitive content;
- aggregator flush/cancel has no goroutine leak.

Run with `-race`.

## 7. Capability resolver tests

Cover:

- Telegram official-like server → expected supported capabilities;
- non-Telegram channel → only its capabilities, no Telegram leakage;
- route-specific poll unsupported condition;
- checklist conditional without business context;
- checklist supported only with trusted business context;
- old/custom server returns method-not-found for `sendPoll` → `poll.quiz`/`poll.regular` downgrade only;
- network timeout → no unsupported negative cache;
- 401/403 → no unsupported negative cache;
- 429 → no unsupported negative cache;
- malformed request fixture → treated as bug/error, not capability downgrade;
- capability cache is isolated between BaseURL/account identities;
- TTL expiry permits re-probe/recovery;
- explicit cache reset/refresh if implemented;
- concurrent capability reads/writes race-free.

## 8. `send_poll` tests

### Validation

- question empty rejected;
- current max question length boundary;
- 0 options rejected;
- 1 option accepted if current API still permits;
- current max 12 options accepted;
- 13 rejected;
- option empty/too long rejected;
- invalid open period/close date rejected;
- invalid country code shape rejected where implemented;
- `allow_adding_options` invalid with anonymous poll;
- route restrictions validated.

### Serialization/mapping

- mode regular;
- anonymous flag;
- multiple answers;
- revoting;
- shuffle;
- hide results;
- description;
- thread/topic mapping;
- reply mapping;
- media mapping where implemented;
- no paid-broadcast field from tool input.

### Fallback

- unsupported native poll uses documented fallback or returns explicit unsupported result;
- fallback does not claim native vote aggregation if it cannot provide it.

## 9. `send_quiz` tests

Mandatory regressions:

- maps to `sendPoll` with `type=quiz`;
- uses `correct_option_ids` plural;
- new production code does not send legacy `correct_option_id`;
- no correct answer rejected;
- one correct answer;
- multiple correct answers;
- duplicate correct ID rejected/canonicalized;
- negative/out-of-range ID rejected;
- monotonic ordering requirement satisfied;
- explanation limits;
- multiple-answer quiz semantics according to current API;
- revoting according to current API;
- shuffle;
- option mapping after shuffle remains upstream-controlled and correct IDs refer to original option positions as API expects;
- topic/private-topic route;
- user preference `native` triggers `send_quiz` when capability supported;
- explicit per-turn text request overrides durable native preference;
- native unavailable → text quiz fallback preserves answer correctness;
- native unavailable → never regular-poll silent fallback.

## 10. Poll lifecycle tests

### Stop

- stop own registered poll;
- stop already closed poll behavior;
- unknown handle;
- stale handle;
- wrong sender;
- wrong chat;
- wrong topic;
- wrong account;
- wrong agent;
- wrong session;
- concurrent stop exactly-once/benign handling.

### Answers

- non-anonymous answer mapped to correct poll;
- answer retraction;
- revote;
- multiple selection;
- `option_persistent_ids` handling if implemented;
- two active polls same chat different sessions do not cross;
- same poll many concurrent answers race-safe;
- anonymous poll does not fabricate user identity;
- restart behavior matches documented persistence design.

## 11. Rich Message block tests

For each supported block type:

- map channel-neutral block → Telego type;
- fallback text is readable;
- nested limits validated;
- unsupported block on old server downgrades correctly;
- table regression remains intact.

At minimum test:

- paragraph
- heading
- pre/code
- footer
- divider
- math
- anchor/link
- list
- blockquote
- pull quote
- details
- table

For media-rich blocks that are implemented:

- media scheme/ref validation;
- total media limit;
- collage/slideshow constraints;
- map coordinates/zoom.

## 12. Rich streaming tests

- existing plain `SendMessageDraft` still used for plain streaming;
- rich content uses `SendRichMessageDraft` when supported;
- same nonzero draft ID reused for updates;
- final `SendRichMessage` persists final output;
- rich draft failure falls back to text draft/final send;
- final send failure is surfaced;
- `thinking` block draft-only;
- no chain-of-thought text emitted in thinking block;
- cancellation stops further draft updates;
- no stream goroutine leak.

## 13. Safe native parity tests

### Animation

- compatible media sends as animation;
- fallback when unsupported;
- ephemeral route if supported by current API/adapter.

### Sticker

- valid sticker ref/file;
- invalid file/type;
- no sticker-set mutation capability.

### Video note

- valid video note;
- invalid type/constraints;
- fallback normal video where policy allows.

### Live photo

- explicit photo/video pair;
- missing one side rejected;
- no arbitrary attachment guessing.

### Location

- valid latitude/longitude;
- out-of-range rejected;
- route/topic mapping;
- privacy-aware logging.

### Venue

- required title/address;
- coordinate validation.

### Contact

- explicit current-turn contact allowed;
- hidden private-memory contact to shared group blocked without explicit user intent;
- vCard/log redaction;
- route mapping.

### Dice

- supported emoji allowlist;
- unsupported emoji rejected;
- returned platform result recorded truthfully;
- model cannot preselect result.

### Checklist

- no business context → capability conditional/unavailable;
- trusted business context → send works;
- model-supplied fake business ID ignored/rejected;
- text fallback optional and truthful.

## 14. Existing Telegram regression suite

Explicitly rerun/extend tests for:

- `/session` menu callbacks;
- `/model` menu callbacks;
- callback <= Telegram limit;
- stale callback;
- wrong sender/chat/topic/account/agent/session;
- Rich Message table fallback;
- ephemeral send/edit/delete;
- normal send and Markdown parsing;
- media group;
- text streaming;
- command registration;
- custom BaseURL/proxy if fixtures exist.

## 15. Non-Telegram regression

Compile/test channel-neutral packages and representative other channels.

Verify:

- new bus structures do not force Telegram SDK import;
- fallback works when structured/native capability absent;
- tool registry does not advertise unusable native action as guaranteed;
- normal message tool behavior unchanged.

## 16. Targeted race suites

Use current build tags/workflows. At the observed repository baseline, expected commands are approximately:

```bash
go test -count=1 -race -tags=goolm,stdjson ./pkg/memory/...
go test -count=1 -race -tags=goolm,stdjson ./pkg/session/...
go test -count=1 -race -tags=goolm,stdjson ./pkg/bus/...
go test -count=1 -race -tags=goolm,stdjson ./pkg/tools/...
go test -count=1 -race -tags=goolm,stdjson ./pkg/agent/...
go test -count=1 -race -tags=goolm,stdjson ./pkg/channels/telegram/...
```

Add packages touched by the implementation.

If a package requires different tags/platform constraints, adapt based on current source; document the exact commands actually run.

## 17. Full Go validation

After targeted tests pass, inspect current workflows and run equivalent current commands. Expected baseline:

```bash
go generate ./...
go test -count=1 -tags=goolm,stdjson ./...
golangci-lint run --build-tags=goolm,stdjson
govulncheck -C . -format text ./...
```

Do not claim a command passed if it was unavailable and not run. If local tooling is missing, install safely or rely on official CI and say which evidence came from CI.

## 18. Frontend/dashboard validation

Because dashboard identity/labels/config may change, run current frontend validation, including as applicable:

- unit tests;
- typecheck;
- lint;
- production build;
- backend API tests.

Use repository package manager/scripts as source of truth.

## 19. Integration/distribution validation

Run all required repository gates, including when present:

- integration tests;
- Docker-backed integration;
- actionlint/workflow syntax;
- standalone distribution validation;
- GoReleaser/config validation;
- security workflow.

## 20. Exact-final-HEAD CI

After final push:

1. record final HEAD SHA;
2. wait for all required workflows for that exact SHA;
3. inspect job/step results, not only a badge;
4. if any fails, inspect logs and fix root cause;
5. push new HEAD and repeat;
6. do not call the PR production-ready until exact current HEAD is green.

## 21. Forbidden test shortcuts

Do not:

- add `t.Skip` to avoid a failure;
- weaken an existing meaningful assertion;
- remove a regression test because implementation conflicts with it;
- mock away the security boundary under test;
- add dummy compile-only tests instead of behavior tests;
- disable race/security/lint workflow;
- alter workflow to exclude failing packages without architectural justification.
