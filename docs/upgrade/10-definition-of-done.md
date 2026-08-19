# 10 — Definition of Done

The work is complete only when every applicable item is proven on the final branch/PR state.

## A. Source audit / architecture

- [ ] Latest repository state was audited before coding.
- [ ] Current Telegram Bot API and Telego capability were verified from primary/current source.
- [ ] Existing correct personal-memory implementation was reused, not unnecessarily rewritten.
- [ ] Existing channel/session/media primitives were reused where sound.
- [ ] No raw generic Telegram Bot API passthrough was added.
- [ ] Channel-neutral packages do not import Telegram SDK types unnecessarily.
- [ ] Capability architecture has supported/unsupported/conditional semantics.
- [ ] Capability resolution is route-aware where needed.
- [ ] Unsupported-method runtime downgrade is bounded/isolated if implemented.

## B. Personal identity / dashboard

- [ ] Trusted cross-channel person scope exists for explicitly linked identities.
- [ ] Unlinked identities remain safely separate.
- [ ] Missing/ambiguous identity fails closed.
- [ ] Dashboard cannot select arbitrary user identity via request fields.
- [ ] Dashboard clearly labels dashboard-local vs canonical-person profile.
- [ ] Configured owner binding uses trusted canonical identity configuration, not arbitrary raw user input.
- [ ] Legacy/channel-scoped memory migration is deterministic and idempotent.
- [ ] Migration preserves provenance and conflicting data safely.
- [ ] Migration is race-safe with live writes.
- [ ] User A/B isolation remains proven.

## C. Personalization quality

- [ ] Canonical preference-key vocabulary/alias handling is implemented.
- [ ] Legacy quiz-format preference can resolve to canonical quiz preference.
- [ ] One canonical preference key has one effective active state.
- [ ] Tier-0 profile and Tier-1 retrieval do not redundantly render same source.
- [ ] Prompt budget remains bounded.
- [ ] Same-source dedup does not break usage accounting/retrieval.
- [ ] Foreground/reviewer dedup remains correct.
- [ ] One logical turn produces at most one memory notification.
- [ ] Notification aggregation is privacy-safe and race-safe.
- [ ] No-op/failed memory operations do not emit false success notification.

## D. Capability-aware agent

- [ ] Agent receives truthful compact current-route capabilities.
- [ ] Native tools are used based on capability + policy + explicit intent/preference.
- [ ] LLM is not instructed to fabricate raw Telegram API calls.
- [ ] Durable `presentation.quiz.mode` behavior works across session/topic.
- [ ] Per-turn explicit user instruction overrides durable presentation preference without silently rewriting it.
- [ ] Unsupported native feature chooses declared fallback or truthful failure.
- [ ] Successful native send integrates correctly with final-response suppression/acknowledgement.

## E. Native Poll

- [ ] `send_poll` exists as semantic runtime capability/tool.
- [ ] Regular poll sends through Telegram `SendPoll` when supported.
- [ ] Current question/option limits are validated.
- [ ] Current regular-poll fields implemented according to verified scope.
- [ ] Thread/topic/reply routing is correct.
- [ ] Poll fallback does not falsely claim vote aggregation/native success.

## F. Native Quiz

- [ ] `send_quiz` exists as semantic PicoClaw capability/tool.
- [ ] It maps to Telegram `SendPoll(type=quiz)`.
- [ ] Production send logic uses `correct_option_ids` plural.
- [ ] No new stale `correct_option_id` usage except explicit compatibility test/comment if needed.
- [ ] One correct answer works.
- [ ] Multiple correct answers work when current API supports them.
- [ ] Correct IDs are validated, unique, in range, and ordered as required.
- [ ] Quiz explanation works and is validated.
- [ ] Quiz revoting/multiple-answer/shuffle fields follow current API semantics.
- [ ] Native unavailable never silently becomes correctness-less regular poll.
- [ ] Text/structured quiz fallback preserves answer correctness.

## G. Poll lifecycle

- [ ] `stopPoll` is exposed through a scoped safe action.
- [ ] Poll handles/state do not let model stop arbitrary messages.
- [ ] Inbound `poll` updates are handled as required by implemented lifecycle.
- [ ] Inbound `poll_answer` mapping is safe for non-anonymous polls when tracking is enabled.
- [ ] Anonymous poll behavior is accurately documented.
- [ ] Concurrent poll answers/state updates are race-free.
- [ ] Poll registry has bounded cleanup or safe persistence strategy.
- [ ] Cross-session/topic poll isolation is proven.

## H. Rich Message v2

- [ ] Existing Rich Message title/paragraph/table behavior remains.
- [ ] Safe rich structural block model is implemented.
- [ ] Paragraph/heading/pre/footer/divider/math/anchor/list/quotation/details/table are covered or any non-applicable block is explicitly justified.
- [ ] Implemented media-rich blocks have safe URL/media rules.
- [ ] Rich-message upstream limits are validated/handled.
- [ ] Deterministic text fallback exists.
- [ ] `SendMessageDraft` text streaming does not regress.
- [ ] `SendRichMessageDraft` is used where rich streaming is supported.
- [ ] Final rich message is persisted after draft streaming.
- [ ] Draft-only thinking block contains no chain-of-thought and is absent from final output.

## I. Safe native outbound parity

- [ ] Native animation implemented.
- [ ] Native sticker implemented.
- [ ] Native video note implemented.
- [ ] Native live photo implemented.
- [ ] Native location implemented.
- [ ] Native venue implemented.
- [ ] Native contact implemented with privacy constraints.
- [ ] Native dice implemented with verified emoji allowlist.
- [ ] Common safe delivery options are modeled where appropriate.
- [ ] Existing media store/path/size security remains intact.
- [ ] Existing media group and normal media regressions pass.

## J. Conditional / excluded capabilities

- [ ] Checklist is `conditional`, not falsely advertised globally.
- [ ] Checklist uses trusted runtime business context only if available.
- [ ] Model cannot fabricate `business_connection_id`.
- [ ] Payments/invoices/Stars spending are not exposed.
- [ ] `allow_paid_broadcast` is not model-controlled.
- [ ] Paid media/gifts are not exposed.
- [ ] Managed-bot token operations are not exposed.
- [ ] Generic moderation/admin/destructive actions are not exposed.
- [ ] Arbitrary cross-chat forward/copy API is not exposed by this feature.

## K. Security/privacy

- [ ] No bot token/API credential in logs/errors/storage/tool results/callbacks/prompts/tests.
- [ ] Ephemeral receiver/callback IDs remain trusted-runtime derived.
- [ ] Callback data limits/opaque-state rules remain.
- [ ] Wrong sender/chat/topic/account/agent/session callback tests pass.
- [ ] Contact/vCard privacy tests pass.
- [ ] Location privacy/scope tests pass.
- [ ] Personal memory does not leak into shared native payloads.
- [ ] Capability cache keys contain no plaintext secret.
- [ ] Unsupported-method detection does not misclassify transient/auth failures.

## L. Regression

- [ ] `/session` behavior passes.
- [ ] `/model` behavior passes.
- [ ] session/model callback security passes.
- [ ] Telegram ephemeral behavior passes.
- [ ] normal text/Markdown delivery passes.
- [ ] text draft streaming passes.
- [ ] Rich Message table fallback passes.
- [ ] non-Telegram channel compile/fallback passes.
- [ ] memory capture/retrieval/profile/checkpoint/recall regressions pass.
- [ ] user A/B memory isolation passes.

## M. Concurrency

- [ ] person-scope migration race tests pass.
- [ ] preference same-key concurrency tests pass.
- [ ] notification aggregation race tests pass.
- [ ] capability cache race tests pass.
- [ ] poll registry/answer race tests pass.
- [ ] Telegram streaming/native state race tests pass.
- [ ] no goroutine/resource leak found in touched paths.

## N. Validation

- [ ] targeted unit tests pass on final source.
- [ ] targeted integration tests pass.
- [ ] required `-race` suites pass.
- [ ] `go generate ./...` or current repository equivalent passes.
- [ ] full Go tests with current required build tags pass.
- [ ] golangci-lint/current lint gate passes.
- [ ] govulncheck/current security gate passes.
- [ ] frontend tests/typecheck/lint/build relevant to dashboard pass.
- [ ] backend API tests pass.
- [ ] repository integration workflow passes.
- [ ] standalone/distribution validation passes if required.
- [ ] action/workflow syntax validation passes if required.
- [ ] exact final HEAD required GitHub CI is green.

## O. Final diff audit

- [ ] no unrelated source rewrite.
- [ ] no debug artifacts.
- [ ] no temporary workflow left unintentionally.
- [ ] no accidental TODO/FIXME.
- [ ] no `t.Skip` shortcut.
- [ ] no dummy tests/assertions.
- [ ] no unjustified `nolint`.
- [ ] no stale raw API examples misleading the model.
- [ ] no legacy singular quiz field in active send path.
- [ ] no duplicated route/security implementation.
- [ ] no unbounded map/cache/goroutine.
- [ ] no silent semantic fallback.

## P. PR state

- [ ] PR targets current `main`.
- [ ] PR title/body accurately describe integrated upgrade.
- [ ] PR body contains actual targeted/race/full validation evidence.
- [ ] PR body documents intentional conditional/out-of-scope capabilities.
- [ ] no unresolved actionable review thread.
- [ ] PR is mergeable.
- [ ] PR is Ready for Review only after all required exact-head gates are green.
- [ ] `main` was not directly modified by implementation work.
- [ ] PR has **NOT** been merged without explicit user approval.
- [ ] No release was created.

Only after all applicable items are proven may the branch/PR be called production-ready / ready for review.
