# 07 — Normative Invariants

These invariants are the final architectural gate. A green test suite is insufficient if the implementation violates them.

## A. Personal identity and memory

`M-01` Personal memory is owned by a trusted canonical identity, never by ChatID, TopicID, or SessionKey.

`M-02` Cross-channel person unification occurs only when trusted explicit identity-link configuration proves the mapping.

`M-03` Missing, ambiguous, or invalid cross-channel identity mapping fails closed to the existing safe channel/account-scoped user identity; it never falls through to another person.

`M-04` Dashboard HTTP input cannot select arbitrary personal-memory identity.

`M-05` Dashboard UI truthfully distinguishes dashboard-local profile from a configured canonical-person profile.

`M-06` User A cannot list/read/update/delete/use User B personal memory.

`M-07` Group/topic behavioral preferences may influence only the current authenticated sender's turn.

`M-08` Private personal facts are not injected or disclosed in shared context merely because identity is linked.

`M-09` Curated memory remains source of truth; compiled profile is a derived view, not a second writable database.

`M-10` Migration to person scope is idempotent and cannot lose the only copy of durable memory.

## B. Preferences and prompt construction

`P-01` A canonical structured preference key has at most one effective active value.

`P-02` Legacy preference aliases normalize/migrate deterministically to canonical keys without creating parallel active states.

`P-03` Same-key same-value reaffirmation does not create duplicate effective memory.

`P-04` A newer higher-authority correction supersedes the old effective state according to existing evidence/recency policy.

`P-05` Tier-0 compiled profile and Tier-1 retrieval do not render the same source fact/preference redundantly in one prompt unless extra detail is materially required.

`P-06` Profile source dedup does not create a retrieval feedback loop or unbounded usage-score inflation.

`P-07` Prompt memory remains bounded and treats memory content as data, not privileged instructions.

## C. Memory notification/reviewer

`N-01` One logical turn produces at most one user-visible memory-change notification.

`N-02` Foreground and background reviewer updates associated with one turn are coalesced safely.

`N-03` Failed/no-op changes do not claim successful memory update.

`N-04` Notification previews preserve privacy/redaction.

`N-05` Internal memory notifications do not re-enter memory capture/reviewer/recall as user content.

`N-06` Notification aggregation is bounded, race-safe, and does not spawn unbounded goroutines.

## D. Capability truthfulness

`C-01` The LLM never has to infer executable capabilities from platform prior knowledge alone.

`C-02` Current route has a declarative capability view.

`C-03` Capability resolution can distinguish supported, unsupported, and conditional behavior.

`C-04` Route-specific conditions are respected; channel-level support does not imply every chat route supports it.

`C-05` Custom/older Telegram server downgrade is scoped to the exact capability and safe server/account identity.

`C-06` Transient network/auth/rate-limit errors do not become false “unsupported capability” evidence.

`C-07` Capability caches contain no plaintext credential and are race-safe/bounded.

`C-08` A disabled/unsupported capability does not break ordinary text response delivery.

## E. Tool authority

`T-01` No generic raw Telegram method/JSON passthrough is exposed to the LLM.

`T-02` Semantic tools reuse trusted current-route metadata by default.

`T-03` Model cannot set/override trusted ephemeral receiver ID, callback query ID, business connection ID, canonical person key, or session capability token.

`T-04` Cross-chat sends do not gain broader authority than existing message-tool policy.

`T-05` Tool success reflects actual delivery mode. A text fallback is not reported as native quiz success.

`T-06` Native tool sends integrate with per-round final-response suppression/acknowledgement without suppressing unrelated target responses.

## F. Poll and quiz correctness

`QZ-01` `send_quiz` maps to Telegram `sendPoll(type=quiz)`; there is no fabricated upstream `sendQuiz` method.

`QZ-02` Modern plural `correct_option_ids` semantics are used. Legacy `correct_option_id` is absent from production send logic, tool docs, and new tests except explicit migration/regression assertions.

`QZ-03` Quiz requires at least one valid correct option.

`QZ-04` Correct option IDs are unique, in range, and represented in the ordering required by the verified upstream API.

`QZ-05` Regular poll and quiz-only fields cannot form an invalid silent hybrid.

`QZ-06` Native quiz unavailability never silently degrades into a regular poll that discards correctness.

`QZ-07` Poll option/question/explanation limits are validated before Telegram call.

`QZ-08` Poll/quiz respects topic/thread and trusted route scope.

`QZ-09` Two concurrent polls/quizzes in different sessions/topics cannot cross-map answers or stop actions.

`QZ-10` Anonymous polls do not promise per-user score tracking that the platform cannot provide.

`QZ-11` Stop-poll action can only affect a poll PicoClaw is authorized to operate in the validated route/scope.

## G. Rich message / streaming

`R-01` Existing Rich Message title/paragraph/table behavior remains functional.

`R-02` New block model has deterministic plain/text fallback.

`R-03` Rich-message upstream limits are checked or safely handled; malformed/oversize content does not panic.

`R-04` `SendMessageDraft` behavior does not regress.

`R-05` Rich streaming uses `SendRichMessageDraft` only where route/server supports it.

`R-06` A rich draft is temporary; finalized output is persisted by final send.

`R-07` Draft-only `thinking` content never contains private model chain-of-thought and does not appear in final persisted message if upstream forbids it.

`R-08` Draft failure does not prevent final response if final delivery remains possible.

## H. Safe native media/content

`D-01` Animation/sticker/video-note/live-photo delivery reuses safe media path/ref handling.

`D-02` Location coordinates are validated and are not inferred/stored carelessly.

`D-03` Contact/vCard data is treated as sensitive and is not sourced from private memory into shared output without explicit authorization.

`D-04` Dice result is platform-generated; model does not preclaim the result.

`D-05` Checklist remains conditional on trusted business context; model cannot fabricate business identity.

`D-06` Native delivery options never enable paid broadcast/Stars spending through model input.

## I. Callback and platform state

`I-01` Callback data remains within Telegram byte limit.

`I-02` Sensitive/raw session/model/person/poll state is not embedded in callback data when server-side opaque state can be used.

`I-03` Callback ownership validation remains sender/chat/topic/account/agent/session safe.

`I-04` Stale callback/state is rejected safely.

## J. Fallback

`F-01` Every native capability defines equivalent/degraded/no-fallback semantics.

`F-02` Security/validation failures are not bypassed by fallback.

`F-03` Unsupported-method fallback is isolated per capability and bounded.

`F-04` Native-to-text fallback preserves essential content and correctness.

`F-05` Non-Telegram channels remain usable and are not forced to understand Telegram SDK types.

## K. Concurrency and lifecycle

`X-01` No data race in person-scope migration, notification aggregation, capability cache, poll registry, or poll-answer handling.

`X-02` Concurrent same-key memory updates cannot create two effective active states.

`X-03` Poll state registry is bounded/cleaned or intentionally persisted with lifecycle rules.

`X-04` Network calls are context-aware and bounded.

`X-05` New background work has explicit lifecycle ownership and cancellation.

`X-06` Existing provider/session/channel lifecycle remains intact.

## L. Security exclusions

`S-01` No bot token/credential leakage in storage/logs/errors/tool results/prompt/callbacks/tests.

`S-02` No payment/Stars/paid broadcast capability is exposed by these tools.

`S-03` No managed-bot token operation is exposed.

`S-04` No generic admin/moderation/destructive operation is exposed.

`S-05` No arbitrary raw Bot API passthrough exists.

`S-06` Business/ephemeral/platform authority is derived only from trusted runtime state.

## M. Regression

`G-01` `/session` remains functional and isolated.

`G-02` `/model` remains functional and isolated.

`G-03` Existing inline keyboard callback security is not weakened.

`G-04` Existing Telegram ephemeral delivery is not weakened.

`G-05` Existing normal media delivery and media groups remain functional.

`G-06` Existing memory capture/retrieval/checkpoint/recall behavior does not regress outside intended hardening.

`G-07` Existing non-Telegram channels continue to compile and deliver fallbacks.

## N. Evidence / completion

`E-01` All claimed tests were actually run on the source being reported.

`E-02` Required CI is green on exact final HEAD.

`E-03` No unresolved actionable review thread remains before Ready for Review.

`E-04` No `t.Skip`, dummy assertion, workflow weakening, or unjustified lint bypass hides a failure.

`E-05` PR is not merged without explicit user approval.
