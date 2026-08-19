# 09 — Execution Phases and PR Workflow

This file defines the recommended execution order. It is not permission to skip source audit or acceptance gates.

## Phase 0 — Repository and upstream audit

Before coding:

### Repository

Verify:

- `git status`
- current branch
- remotes
- current HEAD
- latest `main`
- divergence
- open PRs relevant to this feature
- changed files/commit history if continuing an existing branch
- required workflows/checks
- unresolved review threads

Read current implementations for:

- `pkg/bus`
- `pkg/channels` interfaces/manager
- `pkg/channels/telegram`
- `pkg/tools/integration/message.go`
- agent tool registration/final-response suppression
- memory prompt/notifications
- person/session identity functions
- dashboard current-user API/UI
- config patch/update code

### Upstream

Verify from primary sources:

- current Telegram Bot API release and `sendPoll` semantics;
- current `correct_option_ids` behavior;
- rich messages/drafts;
- safe native methods in scope;
- current Telego version and whether required types/methods exist.

Do not code from the reference matrix alone.

## Phase 1 — Write an audit delta

Before changing architecture, write a short internal checklist comparing:

```text
spec expected state
vs
actual main state
```

Mark each requirement:

- already implemented correctly;
- partially implemented;
- missing;
- superseded by newer upstream behavior;
- not applicable.

Avoid reimplementing already-correct features.

## Phase 2 — Memory/person-scope hardening

Implement first because capability-aware personal preference depends on trusted identity.

Recommended order:

1. person-scope resolver;
2. dashboard trusted owner binding/label;
3. safe migration/consolidation;
4. canonical preference-key alias registry;
5. prompt source dedup;
6. notification aggregation;
7. memory targeted + race tests.

Keep each step reviewable.

## Phase 3 — Declarative capability core

Implement channel-neutral capability IDs/states/resolver.

Add:

- Telegram capability declaration;
- other-channel safe defaults;
- route-aware conditions;
- optional bounded runtime unsupported-method cache;
- tests.

Do not expose new LLM tools yet until capability/policy layer is trustworthy.

## Phase 4 — Poll/Quiz P0

Implement end-to-end:

```text
semantic tool schema
→ tool execution
→ bus/native payload
→ channel manager
→ Telegram adapter
→ Telego SendPoll
→ sent message/poll handle
→ fallback/status
```

Then implement:

- stop poll;
- poll state registry;
- inbound `poll`/`poll_answer` handling required by feature;
- cancellation/lifecycle;
- security tests;
- race tests.

Re-read `04`, `06`, `07` before this phase.

## Phase 5 — Rich Message v2

Expand channel-neutral rich block model while retaining legacy fields.

Implement:

- block mapping;
- limits;
- fallback;
- rich draft streaming;
- draft-only thinking placeholder if useful;
- tests for native + fallback.

Do not leak internal reasoning.

## Phase 6 — Safe native outbound parity P1

Implement, ideally in coherent groups:

### Media group

- animation
- sticker
- video note
- live photo

### Structured data group

- location
- venue
- contact
- dice

### Shared delivery options

- silent/protect content
- controlled reply UI improvements if clean

Reuse the same route/policy/media primitives.

Do not duplicate callback, media-store, or target-resolution logic.

## Phase 7 — Conditional capability

Implement checklist only if current Telego/API/source makes it reasonable and the trusted business context can be modeled correctly.

If current PicoClaw has no business-channel runtime context at all:

- capability may remain `conditional: context_unavailable`;
- implement the abstraction/tests that prevent false exposure;
- do not invent a fake business connection architecture merely to check a box.

Document the exact blocker/condition.

## Phase 8 — Agent capability awareness

Update tool/prompt exposure so model sees actual runtime abilities.

Add user-preference resolution.

Run core scenario:

1. store `presentation.quiz.mode=native`;
2. new session/topic;
3. request quiz;
4. verify tool chosen and native payload sent;
5. same preference on unsupported route → correct fallback.

Audit final-response suppression/acknowledgement.

## Phase 9 — Full validation and source audit

Run:

- targeted tests;
- race suites;
- full Go validation;
- frontend validation;
- security validation;
- integration/distribution validation.

Then audit full diff against current `main` for:

- unrelated rewrites;
- generated junk;
- debug code;
- TODO/FIXME temporary notes;
- `t.Skip`;
- unjustified `nolint`;
- credential/contact/location leakage;
- duplicated logic;
- raw Bot API passthrough;
- model-controlled trusted IDs;
- paid/spending fields;
- context.Background misuse;
- unbounded goroutines/maps;
- stale legacy `correct_option_id` production usage;
- inaccurate docs/tool descriptions.

## Phase 10 — Commit/push/CI loop

### Commit strategy

Use structured commits while developing. Examples only:

1. `fix(memory): unify trusted personal identity scopes`
2. `fix(memory): deduplicate prompt sources and notifications`
3. `feat(channels): add declarative delivery capabilities`
4. `feat(telegram): add native poll and quiz delivery`
5. `feat(telegram): expand rich and native message support`
6. `feat(agent): expose capability-aware delivery tools`
7. `test: cover native delivery and personal-agent invariants`

Do not split artificially when changes are tightly coupled.

If the branch is already published, do not force-push merely to make history prettier unless the user/spec explicitly requires a final squash and it is safe.

### Push policy

- push only feature branch;
- never force-update `main`;
- never create a release;
- after each meaningful push inspect CI.

### CI failure policy

For every failure:

```text
open failing job
→ read failing step/log
→ identify root cause
→ patch source/test correctly
→ rerun targeted test
→ push
→ inspect exact new HEAD
```

Do not stop because “most checks are green”.

## Phase 11 — PR creation/finalization

If no PR exists for this branch, create one to current `main`.

PR title should represent the integrated feature, e.g.:

`feat: add capability-aware native Telegram delivery`

PR body must explain:

- prior memory baseline and residual hardening;
- trusted person-scope/dashboard identity behavior;
- prompt/notification dedup;
- capability architecture;
- poll/quiz implementation;
- `correct_option_ids` modern semantics;
- poll answer/stop lifecycle;
- Rich Message/Rich Draft expansion;
- safe native media/location/contact/dice additions;
- conditional checklist behavior;
- privileged capability exclusions;
- fallback/custom server behavior;
- security/privacy controls;
- tests/race/full CI evidence;
- known limitations.

Do not claim a feature is implemented if capability remains intentionally conditional/unavailable.

## Phase 12 — Ready-for-review gate

Only mark Ready for Review when:

- source/diff audit complete;
- all applicable `10-definition-of-done.md` items proven;
- exact current HEAD required checks green;
- review threads clean;
- mergeable;
- no unresolved security/correctness blocker.

## Merge policy

**Do not merge.**

The user's authorization to implement/create a PR is not authorization to merge.

Wait for an explicit later instruction identifying the PR/merge action.

Do not create a release after completion.
