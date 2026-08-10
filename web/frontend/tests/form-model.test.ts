import assert from "node:assert/strict"
import test from "node:test"

import {
  EMPTY_FORM,
  MEMORY_NOTIFICATION_OPTIONS,
  MEMORY_RECALL_OPTIONS,
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
      result[key] = mergePatch(
        result[key] as JsonObject,
        value as JsonObject,
      )
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
  assert.equal(form.memoryReviewMaxIterations, "2")
})

test("memory dashboard values round trip through its merge patch", () => {
  const original = {
    version: 3,
    gateway: { port: 19444, log_level: "debug" },
    agents: { defaults: { workspace: "/srv/picoclaw", max_tokens: 8192 } },
    channel_list: {
      telegram: { enabled: false, settings: { proxy: "socks5://localhost" } },
    },
    memory: {
      enabled: false,
      workspace_char_limit: 24000,
      per_user_char_limit: 16000,
      write_approval: true,
      notifications: "verbose",
      background_review: {
        enabled: false,
        interval: 17,
        provider: "openai",
        model: "review-model",
        timeout_seconds: 45,
        max_iterations: 4,
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
  }

  const form = buildFormFromConfig(original)
  const memoryPatch = buildMemoryConfigPatch(form)
  const merged = mergePatch(original, { memory: memoryPatch })
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

test("memory numeric and boolean controls serialize without coercion loss", () => {
  const form = {
    ...EMPTY_FORM,
    memoryEnabled: false,
    memoryBackgroundReviewEnabled: false,
    memoryWriteApproval: true,
    memoryCheckpointsEnabled: true,
    memoryReviewInterval: "23",
    memoryReviewTimeoutSeconds: "61",
    memoryReviewMaxIterations: "3",
    memoryWorkspaceCharLimit: "34567",
    memoryPerUserCharLimit: "23456",
    memoryRecallMaxResults: "12",
    memoryRecallMaxChars: "12345",
    memoryCheckpointMaxCount: "456",
    memoryCheckpointMaxContextChars: "7654",
    memoryCheckpointCompletedRetentionDays: "365",
  }

  const patch = buildMemoryConfigPatch(form)
  const review = patch.background_review as JsonObject
  const recall = patch.recall as JsonObject
  const checkpoints = patch.checkpoints as JsonObject

  assert.equal(patch.enabled, false)
  assert.equal(patch.write_approval, true)
  assert.equal(patch.workspace_char_limit, 34567)
  assert.equal(patch.per_user_char_limit, 23456)
  assert.equal(review.enabled, false)
  assert.equal(review.interval, 23)
  assert.equal(review.timeout_seconds, 61)
  assert.equal(review.max_iterations, 3)
  assert.equal(recall.max_results, 12)
  assert.equal(recall.max_chars, 12345)
  assert.equal(checkpoints.enabled, true)
  assert.equal(checkpoints.max_count, 456)
  assert.equal(checkpoints.max_context_chars, 7654)
  assert.equal(checkpoints.completed_retention_days, 365)
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
})
