import assert from "node:assert/strict"
import test from "node:test"

import {
  EMPTY_FORM,
  EVOLUTION_APPLY_POLICY_OPTIONS,
  EVOLUTION_COLD_PATH_TRIGGER_OPTIONS,
  EVOLUTION_MODE_OPTIONS,
  MEMORY_APPROVAL_OPTIONS,
  MEMORY_NOTIFICATION_OPTIONS,
  MEMORY_RECALL_OPTIONS,
  MEMORY_RETRIEVAL_ENGINE_OPTIONS,
  buildAdvancedAgentDefaultsPatch,
  buildEvolutionConfigPatch,
  buildFormFromConfig,
  buildMemoryConfigPatch,
  buildMemoryReviewOptions,
} from "../src/components/config/form-model.ts"

type JsonObject = Record<string, unknown>

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function mergePatch(target: JsonObject, patch: JsonObject): JsonObject {
  const result = clone(target)
  for (const [key, value] of Object.entries(patch)) {
    if (value === null) {
      delete result[key]
      continue
    }
    if (
      value &&
      typeof value === "object" &&
      !Array.isArray(value) &&
      result[key] &&
      typeof result[key] === "object" &&
      !Array.isArray(result[key])
    ) {
      result[key] = mergePatch(result[key] as JsonObject, value as JsonObject)
      continue
    }
    result[key] = clone(value)
  }
  return result
}

test("legacy dashboard config receives active memory defaults", () => {
  const form = buildFormFromConfig({
    version: 3,
    agents: { defaults: { workspace: "/srv/picoclaw" } },
  })

  assert.equal(form.memoryEnabled, true)
  assert.equal(form.memoryBackgroundReviewEnabled, true)
  assert.equal(form.memoryReviewInterval, "10")
  assert.equal(form.memoryReviewTimeoutSeconds, "30")
  assert.equal(form.memoryReviewMaxIterations, "3")
})

test("legacy parallel-turn values use the effective sequential default", () => {
  assert.equal(buildFormFromConfig({}).maxParallelTurns, "1")
  assert.equal(
    buildFormFromConfig({ agents: { defaults: {} } }).maxParallelTurns,
    "1",
  )
  assert.equal(
    buildFormFromConfig({
      agents: { defaults: { max_parallel_turns: 0 } },
    }).maxParallelTurns,
    "1",
  )
})

test("parallel-turn values round trip at the exact nested config path", () => {
  for (const value of ["1", "2", "3"]) {
    const original = {
      agents: {
        defaults: {
          workspace: "/srv/picoclaw",
          max_parallel_turns: Number(value),
          subturn: { max_concurrent: 7 },
        },
      },
      memory: { enabled: true, future_memory_field: "keep" },
      evolution: { enabled: false, future_evolution_field: "keep" },
      tools: { mcp: { enabled: true, future_mcp_field: "keep" } },
      launcher_extension: { enabled: true },
      unrelated: { preserved: true },
    }
    const form = buildFormFromConfig(original)
    const agentDefaultsPatch = buildAdvancedAgentDefaultsPatch(form)
    const merged = mergePatch(original, {
      agents: { defaults: agentDefaultsPatch },
    })

    assert.equal(form.maxParallelTurns, value)
    assert.deepEqual(agentDefaultsPatch, {
      max_parallel_turns: Number(value),
    })
    assert.equal(buildFormFromConfig(merged).maxParallelTurns, value)
    assert.deepEqual(merged.agents as JsonObject, {
      defaults: {
        workspace: "/srv/picoclaw",
        max_parallel_turns: Number(value),
        subturn: { max_concurrent: 7 },
      },
    })
    assert.deepEqual(merged.memory, original.memory)
    assert.deepEqual(merged.evolution, original.evolution)
    assert.deepEqual(merged.tools, original.tools)
    assert.deepEqual(merged.launcher_extension, original.launcher_extension)
    assert.deepEqual(merged.unrelated, original.unrelated)

    const memoryOnlyMerged = mergePatch(merged, {
      memory: { enabled: false },
    })
    assert.equal(
      ((memoryOnlyMerged.agents as JsonObject).defaults as JsonObject)
        .max_parallel_turns,
      Number(value),
    )
  }
})

test("parallel-turn validation rejects empty, fractional, and sub-one values", () => {
  for (const value of ["", "1.5", "0", "-1"]) {
    assert.throws(
      () =>
        buildAdvancedAgentDefaultsPatch({
          ...EMPTY_FORM,
          maxParallelTurns: value,
        }),
      /must be an integer|must be >= 1/,
    )
  }
})

test("memory dashboard values round trip through its merge patch", () => {
  const original = {
    version: 3,
    gateway: { port: 19444, log_level: "debug" },
    agents: {
      defaults: {
        workspace: "/srv/picoclaw",
        max_tokens: 8192,
        max_parallel_turns: 3,
      },
    },
    channel_list: {
      telegram: { enabled: false, settings: { proxy: "socks5://localhost" } },
    },
    memory: {
      enabled: false,
      workspace_char_limit: 24000,
      per_user_char_limit: 16000,
      profile: {
        enabled: false,
        max_chars: 2400,
        min_confidence: 0.8,
      },
      write_approval: true,
      approval_mode: "all_writes",
      notifications: "verbose",
      background_review: {
        enabled: false,
        interval: 17,
        provider: "openai",
        model: "review-model",
        timeout_seconds: 45,
        max_iterations: 4,
      },
      retrieval: {
        enabled: true,
        engine: "hybrid_lexical",
        max_workspace_results: 11,
        max_user_results: 12,
        max_total_chars: 8765,
        pinned_char_budget: 2100,
        minimum_relevance_score: 0.42,
        recency_weight: 0.33,
        recency_half_life_days: 45,
        fuzzy_weight: 0.91,
        recent_fallback_count: 4,
        user_share: 0.8,
      },
      lifecycle: {
        archived_retention_days: 730,
        stale_threshold_days: 75,
        auto_archive_expired: true,
      },
      recall: {
        mode: "group_recall",
        max_results: 9,
        max_chars: 9000,
        max_records: 777,
      },
      checkpoints: {
        enabled: true,
        max_count: 321,
        max_context_chars: 6789,
        completed_retention_days: 120,
      },
    },
    evolution: {
      enabled: true,
      mode: "draft",
      state_dir: "/srv/picoclaw/evolution",
      min_task_count: 4,
      min_success_ratio: 0.85,
      cold_path_trigger: "scheduled",
      cold_path_times: ["03:00", "15:30"],
      apply_policy: "approval_required",
      private_data_scrubbing: true,
      draft_timeout_seconds: 90,
      max_evidence_records: 120,
      max_draft_chars: 24000,
      rollback_retention: 20,
    },
  }

  const form = buildFormFromConfig(original)
  const memoryPatch = buildMemoryConfigPatch(form)
  const evolutionPatch = buildEvolutionConfigPatch(form)
  const merged = mergePatch(original, {
    memory: memoryPatch,
    evolution: evolutionPatch,
  })
  const roundTripped = buildFormFromConfig(merged)

  assert.deepEqual(roundTripped, form)
  assert.deepEqual(merged.gateway, original.gateway)
  assert.deepEqual(merged.agents, original.agents)
  assert.deepEqual(merged.channel_list, original.channel_list)
  assert.equal(
    ((merged.memory as JsonObject).recall as JsonObject).max_records,
    777,
  )
})

test("notification and cross-topic recall dropdown choices are preserved", () => {
  for (const notifications of MEMORY_NOTIFICATION_OPTIONS) {
    for (const recallMode of MEMORY_RECALL_OPTIONS) {
      const form = {
        ...EMPTY_FORM,
        memoryNotifications: notifications,
        memoryRecallMode: recallMode,
      }
      const patch = buildMemoryConfigPatch(form)
      assert.equal(patch.notifications, notifications)
      assert.equal((patch.recall as JsonObject).mode, recallMode)
    }
  }
})

test("memory retrieval engine dropdown choices are preserved", () => {
  for (const engine of MEMORY_RETRIEVAL_ENGINE_OPTIONS) {
    const form = { ...EMPTY_FORM, memoryRetrievalEngine: engine }
    const patch = buildMemoryConfigPatch(form)
    assert.equal((patch.retrieval as JsonObject).engine, engine)
    assert.equal(
      buildFormFromConfig({ memory: { retrieval: { engine } } })
        .memoryRetrievalEngine,
      engine,
    )
  }
})

test("memory approval dropdown choices and legacy boolean mapping are preserved", () => {
  assert.equal(
    buildFormFromConfig({ memory: { write_approval: true } })
      .memoryApprovalMode,
    "background_only",
  )
  assert.equal(
    buildFormFromConfig({
      memory: { write_approval: true, approval_mode: "off" },
    }).memoryApprovalMode,
    "off",
  )
  for (const approvalMode of MEMORY_APPROVAL_OPTIONS) {
    const form = { ...EMPTY_FORM, memoryApprovalMode: approvalMode }
    const patch = buildMemoryConfigPatch(form)
    assert.equal(patch.approval_mode, approvalMode)
    assert.equal(patch.write_approval, approvalMode !== "off")
  }
})

test("evolution dropdown choices are preserved", () => {
  for (const mode of EVOLUTION_MODE_OPTIONS) {
    for (const applyPolicy of EVOLUTION_APPLY_POLICY_OPTIONS) {
      const patch = buildEvolutionConfigPatch({
        ...EMPTY_FORM,
        evolutionMode: mode,
        evolutionApplyPolicy: applyPolicy,
      })
      assert.equal(patch.mode, mode)
      assert.equal(patch.apply_policy, applyPolicy)
    }
  }
  for (const trigger of EVOLUTION_COLD_PATH_TRIGGER_OPTIONS) {
    const patch = buildEvolutionConfigPatch({
      ...EMPTY_FORM,
      evolutionColdPathTrigger: trigger,
      evolutionColdPathTimesText: trigger === "scheduled" ? "03:00\n15:30" : "",
    })
    assert.equal(patch.cold_path_trigger, trigger)
  }
  assert.equal(
    buildFormFromConfig({ evolution: { cold_path_trigger: "none" } })
      .evolutionColdPathTrigger,
    "manual",
  )
})

test("memory numeric and boolean controls serialize without coercion loss", () => {
  const form = {
    ...EMPTY_FORM,
    memoryEnabled: false,
    memoryBackgroundReviewEnabled: false,
    memoryWriteApproval: true,
    memoryApprovalMode: "all_writes" as const,
    memoryCheckpointsEnabled: true,
    memoryReviewInterval: "23",
    memoryReviewTimeoutSeconds: "61",
    memoryReviewMaxIterations: "3",
    memoryWorkspaceCharLimit: "34567",
    memoryPerUserCharLimit: "23456",
    memoryProfileEnabled: false,
    memoryProfileMaxChars: "2500",
    memoryProfileMinConfidence: "0.75",
    memoryRecallMaxResults: "12",
    memoryRecallMaxChars: "12345",
    memoryRecallMaxRecords: "3456",
    memoryCheckpointMaxCount: "456",
    memoryCheckpointMaxContextChars: "7654",
    memoryCheckpointCompletedRetentionDays: "365",
    memoryRetrievalEnabled: false,
    memoryRetrievalMaxWorkspaceResults: "17",
    memoryRetrievalMaxUserResults: "18",
    memoryRetrievalMaxTotalChars: "15000",
    memoryRetrievalPinnedCharBudget: "5000",
    memoryRetrievalMinimumScore: "0.6",
    memoryRetrievalRecencyWeight: "0.8",
    memoryRetrievalRecencyHalfLifeDays: "120",
    memoryRetrievalFuzzyWeight: "1.2",
    memoryRetrievalRecentFallbackCount: "7",
    memoryRetrievalUserShare: "0.85",
    memoryArchivedRetentionDays: "720",
    memoryStaleThresholdDays: "240",
    memoryAutoArchiveExpired: true,
  }

  const patch = buildMemoryConfigPatch(form)
  const review = patch.background_review as JsonObject
  const profile = patch.profile as JsonObject
  const recall = patch.recall as JsonObject
  const checkpoints = patch.checkpoints as JsonObject
  const retrieval = patch.retrieval as JsonObject
  const lifecycle = patch.lifecycle as JsonObject

  assert.equal(patch.enabled, false)
  assert.equal(patch.write_approval, true)
  assert.equal(patch.approval_mode, "all_writes")
  assert.equal(patch.workspace_char_limit, 34567)
  assert.equal(patch.per_user_char_limit, 23456)
  assert.equal(review.enabled, false)
  assert.equal(review.interval, 23)
  assert.equal(review.timeout_seconds, 61)
  assert.equal(review.max_iterations, 3)
  assert.equal(profile.enabled, false)
  assert.equal(profile.max_chars, 2500)
  assert.equal(profile.min_confidence, 0.75)
  assert.equal(recall.max_results, 12)
  assert.equal(recall.max_chars, 12345)
  assert.equal(recall.max_records, 3456)
  assert.equal(checkpoints.enabled, true)
  assert.equal(checkpoints.max_count, 456)
  assert.equal(checkpoints.max_context_chars, 7654)
  assert.equal(checkpoints.completed_retention_days, 365)
  assert.equal(retrieval.enabled, false)
  assert.equal(retrieval.max_workspace_results, 17)
  assert.equal(retrieval.max_user_results, 18)
  assert.equal(retrieval.max_total_chars, 15000)
  assert.equal(retrieval.pinned_char_budget, 5000)
  assert.equal(retrieval.minimum_relevance_score, 0.6)
  assert.equal(retrieval.recency_weight, 0.8)
  assert.equal(retrieval.recency_half_life_days, 120)
  assert.equal(retrieval.fuzzy_weight, 1.2)
  assert.equal(retrieval.recent_fallback_count, 7)
  assert.equal(retrieval.user_share, 0.85)
  assert.equal(lifecycle.archived_retention_days, 720)
  assert.equal(lifecycle.stale_threshold_days, 240)
  assert.equal(lifecycle.auto_archive_expired, true)
})

test("evolution numeric and boolean controls serialize without coercion loss", () => {
  const patch = buildEvolutionConfigPatch({
    ...EMPTY_FORM,
    evolutionEnabled: true,
    evolutionMode: "apply",
    evolutionStateDir: "/srv/picoclaw/evolution",
    evolutionMinTaskCount: "7",
    evolutionMinSuccessRatio: "0.9",
    evolutionColdPathTrigger: "scheduled",
    evolutionColdPathTimesText: "03:00\n15:30",
    evolutionApplyPolicy: "automatic",
    evolutionPrivateDataScrubbing: true,
    evolutionDraftTimeoutSeconds: "120",
    evolutionMaxEvidenceRecords: "250",
    evolutionMaxDraftChars: "42000",
    evolutionRollbackRetention: "50",
  })

  assert.equal(patch.enabled, true)
  assert.equal(patch.mode, "apply")
  assert.equal(patch.state_dir, "/srv/picoclaw/evolution")
  assert.equal(patch.min_task_count, 7)
  assert.equal(patch.min_success_ratio, 0.9)
  assert.deepEqual(patch.cold_path_times, ["03:00", "15:30"])
  assert.equal(patch.apply_policy, "automatic")
  assert.equal(patch.private_data_scrubbing, true)
  assert.equal(patch.draft_timeout_seconds, 120)
  assert.equal(patch.max_evidence_records, 250)
  assert.equal(patch.max_draft_chars, 42000)
  assert.equal(patch.rollback_retention, 50)
})

test("review provider and model selectors use configured model entries", () => {
  const options = buildMemoryReviewOptions({
    model_list: [
      {
        model_name: "main-openai",
        provider: "OpenAI",
        model: "gpt-5-mini",
      },
      {
        model_name: "review-anthropic",
        provider: "anthropic",
        model: "claude-sonnet",
      },
      {
        model_name: "protocol-derived",
        model: "gemini/gemini-2.5-flash",
      },
    ],
  })

  assert.deepEqual(options.providers, ["anthropic", "gemini", "openai"])
  assert.deepEqual(
    options.models.map(({ provider, value }) => ({ provider, value })),
    [
      { provider: "openai", value: "main-openai" },
      { provider: "gemini", value: "protocol-derived" },
      { provider: "anthropic", value: "review-anthropic" },
    ],
  )
})

test("memory dashboard applies the same bounded numeric limits as backend", () => {
  assert.throws(
    () =>
      buildMemoryConfigPatch({
        ...EMPTY_FORM,
        memoryReviewMaxIterations: "5",
      }),
    /must be <= 4/,
  )
  assert.throws(
    () =>
      buildMemoryConfigPatch({
        ...EMPTY_FORM,
        memoryRecallMaxResults: "21",
      }),
    /must be <= 20/,
  )
  assert.throws(
    () =>
      buildMemoryConfigPatch({
        ...EMPTY_FORM,
        memoryCheckpointMaxContextChars: "20001",
      }),
    /must be <= 20000/,
  )
  assert.throws(
    () =>
      buildMemoryConfigPatch({
        ...EMPTY_FORM,
        memoryRetrievalMaxWorkspaceResults: "51",
      }),
    /must be <= 50/,
  )
  assert.throws(
    () =>
      buildMemoryConfigPatch({
        ...EMPTY_FORM,
        memoryArchivedRetentionDays: "3651",
      }),
    /must be <= 3650/,
  )
})

test("evolution dashboard matches backend safety and schedule validation", () => {
  assert.throws(
    () =>
      buildEvolutionConfigPatch({
        ...EMPTY_FORM,
        evolutionEnabled: true,
        evolutionPrivateDataScrubbing: false,
      }),
    /must remain enabled/,
  )
  assert.throws(
    () =>
      buildEvolutionConfigPatch({
        ...EMPTY_FORM,
        evolutionColdPathTrigger: "scheduled",
        evolutionColdPathTimesText: "",
      }),
    /requires at least one/,
  )
  assert.throws(
    () =>
      buildEvolutionConfigPatch({
        ...EMPTY_FORM,
        evolutionColdPathTrigger: "scheduled",
        evolutionColdPathTimesText: "25:99",
      }),
    /HH:MM/,
  )
  assert.throws(
    () =>
      buildEvolutionConfigPatch({
        ...EMPTY_FORM,
        evolutionMinTaskCount: "1",
      }),
    /must be >= 2/,
  )
  assert.throws(
    () =>
      buildEvolutionConfigPatch({
        ...EMPTY_FORM,
        evolutionMaxDraftChars: "50001",
      }),
    /must be <= 50000/,
  )
})

test("frontend delta patch produces only modified memory fields and preserves advanced values", () => {
  // 1. Seed custom advanced values
  const originalConfig = {
    version: 3,
    memory: {
      enabled: true,
      workspace_char_limit: 30000,
      per_user_char_limit: 20000,
      write_approval: false,
      approval_mode: "off",
      notifications: "verbose",
      profile: {
        enabled: true,
        max_chars: 3500,
        min_confidence: 0.85,
      },
      background_review: {
        enabled: true,
        interval: 15,
        provider: "anthropic",
        model: "claude-3-5-sonnet",
        timeout_seconds: 60,
        max_iterations: 4,
      },
      retrieval: {
        enabled: true,
        engine: "hybrid_lexical",
        max_workspace_results: 15,
        max_user_results: 20,
        max_total_chars: 12000,
        pinned_char_budget: 3000,
        minimum_relevance_score: 0.5,
        recency_weight: 0.7,
        recency_half_life_days: 60,
        fuzzy_weight: 0.8,
        recent_fallback_count: 5,
        user_share: 0.75,
      },
      lifecycle: {
        archived_retention_days: 365,
        stale_threshold_days: 90,
        auto_archive_expired: true,
      },
      recall: {
        mode: "user_recall",
        max_results: 10,
        max_chars: 8000,
        max_records: 5000,
      },
      checkpoints: {
        enabled: true,
        max_count: 50,
        max_context_chars: 10000,
        completed_retention_days: 30,
      },
    },
  }

  const baselineForm = buildFormFromConfig(originalConfig)

  // 2. Toggle simple memory UI control: Personal Memory ON -> OFF
  const modifiedForm = {
    ...baselineForm,
    memoryEnabled: false,
  }

  // 3. Inspect actual PATCH request body
  const patch = buildMemoryConfigPatch(modifiedForm, baselineForm)

  // 4. Verify only changed field is sent
  assert.deepEqual(patch, {
    enabled: false,
  })

  // 5. Apply merge patch and reload config
  const mergedConfig = mergePatch(originalConfig, { memory: patch })
  const reloadedForm = buildFormFromConfig(mergedConfig)

  // 6. Advanced values unchanged
  assert.equal(reloadedForm.memoryEnabled, false)
  assert.equal(reloadedForm.memoryWorkspaceCharLimit, "30000")
  assert.equal(reloadedForm.memoryPerUserCharLimit, "20000")
  assert.equal(reloadedForm.memoryProfileMaxChars, "3500")
  assert.equal(reloadedForm.memoryProfileMinConfidence, "0.85")
  assert.equal(reloadedForm.memoryReviewInterval, "15")
  assert.equal(reloadedForm.memoryReviewProvider, "anthropic")
  assert.equal(reloadedForm.memoryReviewModel, "claude-3-5-sonnet")
  assert.equal(reloadedForm.memoryRetrievalMaxTotalChars, "12000")
  assert.equal(reloadedForm.memoryRecallMaxResults, "10")
})

test("memory capture mode round trips and delta patch changes only capture policy", () => {
  const original = {
    memory: {
      enabled: true,
      capture_mode: "automatic",
      background_review: { enabled: true, interval: 19, timeout_seconds: 71 },
      retrieval: { recency_weight: 0.61, fuzzy_weight: 0.83 },
    },
  }
  const baseline = buildFormFromConfig(original)
  assert.equal(baseline.memoryCaptureMode, "automatic")
  const modified = { ...baseline, memoryCaptureMode: "explicit_only" as const }
  const patch = buildMemoryConfigPatch(modified, baseline)
  assert.deepEqual(patch, { capture_mode: "explicit_only" })
  const merged = mergePatch(original, { memory: patch })
  const reloaded = buildFormFromConfig(merged)
  assert.equal(reloaded.memoryCaptureMode, "explicit_only")
  assert.equal(reloaded.memoryReviewInterval, "19")
  assert.equal(reloaded.memoryReviewTimeoutSeconds, "71")
  assert.equal(reloaded.memoryRetrievalRecencyWeight, "0.61")
  assert.equal(reloaded.memoryRetrievalFuzzyWeight, "0.83")
})
