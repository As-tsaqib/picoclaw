import { IconAlertTriangle, IconPlus, IconTrash } from "@tabler/icons-react"
import { useState } from "react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import {
  type CoreConfigForm,
  DM_SCOPE_OPTIONS,
  type LauncherForm,
  type MCPServerForm,
  type MCPServerType,
  type MemoryNotificationMode,
  type MemoryRecallMode,
  type MemoryReviewModelOption,
  type TurnProfileForm,
  type TurnProfileMode,
} from "@/components/config/form-model"
import { Field, SwitchCardField } from "@/components/shared-form"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"

type UpdateCoreField = <K extends keyof CoreConfigForm>(
  key: K,
  value: CoreConfigForm[K],
) => void

type UpdateLauncherField = <K extends keyof LauncherForm>(
  key: K,
  value: LauncherForm[K],
) => void

interface ConfigSectionCardProps {
  title: string
  description?: string
  children: ReactNode
}

interface MemoryRecallSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
  reviewProviders: string[]
  reviewModels: MemoryReviewModelOption[]
}

const FOLLOW_MAIN_VALUE = "__follow_main__"

export function MemoryRecallSection({
  form,
  onFieldChange,
  reviewProviders,
  reviewModels,
}: MemoryRecallSectionProps) {
  const { t } = useTranslation()
  const providerOptions = Array.from(
    new Set(
      [form.memoryReviewProvider, ...reviewProviders].filter(
        (value) => value.trim() !== "",
      ),
    ),
  )
  const visibleModels = new Map<string, MemoryReviewModelOption>()
  for (const option of reviewModels) {
    if (
      form.memoryReviewProvider &&
      option.provider !== form.memoryReviewProvider
    ) {
      continue
    }
    if (!visibleModels.has(option.value)) {
      visibleModels.set(option.value, option)
    }
  }
  if (
    form.memoryReviewModel &&
    !visibleModels.has(form.memoryReviewModel)
  ) {
    visibleModels.set(form.memoryReviewModel, {
      provider: form.memoryReviewProvider,
      value: form.memoryReviewModel,
      label: form.memoryReviewModel,
    })
  }

  const updateReviewProvider = (provider: string) => {
    const nextProvider = provider === FOLLOW_MAIN_VALUE ? "" : provider
    onFieldChange("memoryReviewProvider", nextProvider)
    if (
      form.memoryReviewModel &&
      nextProvider &&
      !reviewModels.some(
        (option) =>
          option.provider === nextProvider &&
          option.value === form.memoryReviewModel,
      )
    ) {
      onFieldChange("memoryReviewModel", "")
    }
  }

  return (
    <ConfigSectionCard
      title={t("pages.config.sections.memory")}
      description={t("pages.config.memory_section_hint")}
    >
      <SwitchCardField
        label={t("pages.config.memory_enabled")}
        hint={t("pages.config.memory_enabled_hint")}
        layout="setting-row"
        checked={form.memoryEnabled}
        onCheckedChange={(checked) => onFieldChange("memoryEnabled", checked)}
      />

      <SwitchCardField
        label={t("pages.config.memory_background_review_enabled")}
        hint={t("pages.config.memory_background_review_enabled_hint")}
        layout="setting-row"
        checked={form.memoryBackgroundReviewEnabled}
        onCheckedChange={(checked) =>
          onFieldChange("memoryBackgroundReviewEnabled", checked)
        }
      />

      {form.memoryBackgroundReviewEnabled && (
        <div className="border-amber-500/30 bg-amber-500/10 my-3 flex gap-3 rounded-lg border p-3 text-sm">
          <IconAlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
          <p className="text-amber-950 dark:text-amber-100">
            {t("pages.config.memory_background_review_cost_warning")}
          </p>
        </div>
      )}

      <Field
        label={t("pages.config.memory_review_interval")}
        hint={t("pages.config.memory_review_interval_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.memoryReviewInterval}
          onChange={(event) =>
            onFieldChange("memoryReviewInterval", event.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_review_provider")}
        hint={t("pages.config.memory_review_provider_hint")}
        layout="setting-row"
      >
        <Select
          value={form.memoryReviewProvider || FOLLOW_MAIN_VALUE}
          onValueChange={updateReviewProvider}
        >
          <SelectTrigger className="h-9 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={FOLLOW_MAIN_VALUE}>
              {t("pages.config.memory_follow_main_model")}
            </SelectItem>
            {providerOptions.map((provider) => (
              <SelectItem key={provider} value={provider}>
                {provider}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field
        label={t("pages.config.memory_review_model")}
        hint={t("pages.config.memory_review_model_hint")}
        layout="setting-row"
      >
        <Select
          value={form.memoryReviewModel || FOLLOW_MAIN_VALUE}
          onValueChange={(value) =>
            onFieldChange(
              "memoryReviewModel",
              value === FOLLOW_MAIN_VALUE ? "" : value,
            )
          }
        >
          <SelectTrigger className="h-9 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={FOLLOW_MAIN_VALUE}>
              {t("pages.config.memory_follow_main_model")}
            </SelectItem>
            {Array.from(visibleModels.values()).map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {option.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <Field
        label={t("pages.config.memory_review_timeout")}
        hint={t("pages.config.memory_review_timeout_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.memoryReviewTimeoutSeconds}
          onChange={(event) =>
            onFieldChange("memoryReviewTimeoutSeconds", event.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_review_max_iterations")}
        hint={t("pages.config.memory_review_max_iterations_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={4}
          value={form.memoryReviewMaxIterations}
          onChange={(event) =>
            onFieldChange("memoryReviewMaxIterations", event.target.value)
          }
        />
      </Field>

      <SwitchCardField
        label={t("pages.config.memory_write_approval")}
        hint={t("pages.config.memory_write_approval_hint")}
        layout="setting-row"
        checked={form.memoryWriteApproval}
        onCheckedChange={(checked) =>
          onFieldChange("memoryWriteApproval", checked)
        }
      />

      <Field
        label={t("pages.config.memory_notifications")}
        hint={t("pages.config.memory_notifications_hint")}
        layout="setting-row"
      >
        <Select
          value={form.memoryNotifications}
          onValueChange={(value) =>
            onFieldChange(
              "memoryNotifications",
              value as MemoryNotificationMode,
            )
          }
        >
          <SelectTrigger className="h-9 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="off">off</SelectItem>
            <SelectItem value="on">on</SelectItem>
            <SelectItem value="verbose">verbose</SelectItem>
          </SelectContent>
        </Select>
      </Field>

      <Field
        label={t("pages.config.memory_workspace_char_limit")}
        hint={t("pages.config.memory_workspace_char_limit_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.memoryWorkspaceCharLimit}
          onChange={(event) =>
            onFieldChange("memoryWorkspaceCharLimit", event.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_per_user_char_limit")}
        hint={t("pages.config.memory_per_user_char_limit_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.memoryPerUserCharLimit}
          onChange={(event) =>
            onFieldChange("memoryPerUserCharLimit", event.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_recall_mode")}
        hint={t("pages.config.memory_recall_mode_hint")}
        layout="setting-row"
      >
        <Select
          value={form.memoryRecallMode}
          onValueChange={(value) =>
            onFieldChange("memoryRecallMode", value as MemoryRecallMode)
          }
        >
          <SelectTrigger className="h-9 w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="isolated">isolated</SelectItem>
            <SelectItem value="user_recall">user_recall</SelectItem>
            <SelectItem value="group_recall">group_recall</SelectItem>
          </SelectContent>
        </Select>
      </Field>

      {form.memoryRecallMode === "group_recall" && (
        <div className="border-orange-500/30 bg-orange-500/10 my-3 flex gap-3 rounded-lg border p-3 text-sm">
          <IconAlertTriangle className="mt-0.5 size-4 shrink-0 text-orange-600 dark:text-orange-400" />
          <p className="text-orange-950 dark:text-orange-100">
            {t("pages.config.memory_group_recall_privacy_warning")}
          </p>
        </div>
      )}

      <Field
        label={t("pages.config.memory_recall_max_results")}
        hint={t("pages.config.memory_recall_max_results_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={20}
          value={form.memoryRecallMaxResults}
          onChange={(event) =>
            onFieldChange("memoryRecallMaxResults", event.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_recall_max_chars")}
        hint={t("pages.config.memory_recall_max_chars_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={20000}
          value={form.memoryRecallMaxChars}
          onChange={(event) =>
            onFieldChange("memoryRecallMaxChars", event.target.value)
          }
        />
      </Field>

      <SwitchCardField
        label={t("pages.config.memory_checkpoints_enabled")}
        hint={t("pages.config.memory_checkpoints_enabled_hint")}
        layout="setting-row"
        checked={form.memoryCheckpointsEnabled}
        onCheckedChange={(checked) =>
          onFieldChange("memoryCheckpointsEnabled", checked)
        }
      />

      <Field
        label={t("pages.config.memory_checkpoint_max_count")}
        hint={t("pages.config.memory_checkpoint_max_count_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={1000}
          value={form.memoryCheckpointMaxCount}
          onChange={(event) =>
            onFieldChange("memoryCheckpointMaxCount", event.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_checkpoint_context_limit")}
        hint={t("pages.config.memory_checkpoint_context_limit_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={20000}
          value={form.memoryCheckpointMaxContextChars}
          onChange={(event) =>
            onFieldChange(
              "memoryCheckpointMaxContextChars",
              event.target.value,
            )
          }
        />
      </Field>

      <Field
        label={t("pages.config.memory_checkpoint_retention_days")}
        hint={t("pages.config.memory_checkpoint_retention_days_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.memoryCheckpointCompletedRetentionDays}
          onChange={(event) =>
            onFieldChange(
              "memoryCheckpointCompletedRetentionDays",
              event.target.value,
            )
          }
        />
      </Field>
    </ConfigSectionCard>
  )
}

function ConfigSectionCard({
  title,
  description,
  children,
}: ConfigSectionCardProps) {
  return (
    <Card size="sm">
      <CardHeader className="border-border border-b">
        <CardTitle>{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent className="pt-0">
        <div className="divide-border/70 divide-y">{children}</div>
      </CardContent>
    </Card>
  )
}

interface AgentDefaultsSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
  onTurnProfileFieldChange: <K extends keyof TurnProfileForm>(
    key: K,
    value: TurnProfileForm[K],
  ) => void
}

export function AgentDefaultsSection({
  form,
  onFieldChange,
  onTurnProfileFieldChange,
}: AgentDefaultsSectionProps) {
  const { t } = useTranslation()
  const renderModeSelect = ({
    value,
    onValueChange,
    allowCustom,
  }: {
    value: TurnProfileMode
    onValueChange: (mode: TurnProfileMode) => void
    allowCustom: boolean
  }) => (
    <Select
      value={value}
      onValueChange={(next) => onValueChange(next as TurnProfileMode)}
    >
      <SelectTrigger className="h-9">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value="default">
          {t("pages.config.turn_profile_mode_default")}
        </SelectItem>
        <SelectItem value="off">
          {t("pages.config.turn_profile_mode_off")}
        </SelectItem>
        {allowCustom && (
          <SelectItem value="custom">
            {t("pages.config.turn_profile_mode_custom")}
          </SelectItem>
        )}
      </SelectContent>
    </Select>
  )

  return (
    <ConfigSectionCard title={t("pages.config.sections.agent")}>
      <Field
        label={t("pages.config.workspace")}
        hint={t("pages.config.workspace_hint")}
        layout="setting-row"
      >
        <Input
          value={form.workspace}
          onChange={(e) => onFieldChange("workspace", e.target.value)}
          placeholder="~/.picoclaw/workspace"
        />
      </Field>

      <SwitchCardField
        label={t("pages.config.restrict_workspace")}
        hint={t("pages.config.restrict_workspace_hint")}
        layout="setting-row"
        checked={form.restrictToWorkspace}
        onCheckedChange={(checked) =>
          onFieldChange("restrictToWorkspace", checked)
        }
      />

      <SwitchCardField
        label={t("pages.config.split_on_marker")}
        hint={t("pages.config.split_on_marker_hint")}
        layout="setting-row"
        checked={form.splitOnMarker}
        onCheckedChange={(checked) => onFieldChange("splitOnMarker", checked)}
      />

      <SwitchCardField
        label={t("pages.config.tool_feedback_enabled")}
        hint={t("pages.config.tool_feedback_enabled_hint")}
        layout="setting-row"
        checked={form.toolFeedbackEnabled}
        onCheckedChange={(checked) =>
          onFieldChange("toolFeedbackEnabled", checked)
        }
      />

      {form.toolFeedbackEnabled && (
        <SwitchCardField
          label={t("pages.config.tool_feedback_separate_messages")}
          hint={t("pages.config.tool_feedback_separate_messages_hint")}
          layout="setting-row"
          checked={form.toolFeedbackSeparateMessages}
          onCheckedChange={(checked) =>
            onFieldChange("toolFeedbackSeparateMessages", checked)
          }
        />
      )}

      {form.toolFeedbackEnabled && (
        <Field
          label={t("pages.config.tool_feedback_max_args_length")}
          hint={t("pages.config.tool_feedback_max_args_length_hint")}
          layout="setting-row"
        >
          <Input
            type="number"
            min={0}
            value={form.toolFeedbackMaxArgsLength}
            onChange={(e) =>
              onFieldChange("toolFeedbackMaxArgsLength", e.target.value)
            }
          />
        </Field>
      )}

      <Field
        label={t("pages.config.max_tokens")}
        hint={t("pages.config.max_tokens_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.maxTokens}
          onChange={(e) => onFieldChange("maxTokens", e.target.value)}
        />
      </Field>

      <Field
        label={t("pages.config.context_window")}
        hint={t("pages.config.context_window_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.contextWindow}
          onChange={(e) => onFieldChange("contextWindow", e.target.value)}
          placeholder="131072"
        />
      </Field>

      <Field
        label={t("pages.config.max_tool_iterations")}
        hint={t("pages.config.max_tool_iterations_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.maxToolIterations}
          onChange={(e) => onFieldChange("maxToolIterations", e.target.value)}
        />
      </Field>

      <Field
        label={t("pages.config.summarize_threshold")}
        hint={t("pages.config.summarize_threshold_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.summarizeMessageThreshold}
          onChange={(e) =>
            onFieldChange("summarizeMessageThreshold", e.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.summarize_token_percent")}
        hint={t("pages.config.summarize_token_percent_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={100}
          value={form.summarizeTokenPercent}
          onChange={(e) =>
            onFieldChange("summarizeTokenPercent", e.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.turn_profile")}
        hint={t("pages.config.turn_profile_hint")}
        layout="setting-row"
        controlClassName="md:max-w-[42rem]"
      >
        <div className="space-y-3">
          <SwitchCardField
            label={t("pages.config.turn_profile_enabled")}
            hint={t("pages.config.turn_profile_enabled_hint")}
            checked={form.turnProfile.enabled}
            onCheckedChange={(checked) =>
              onTurnProfileFieldChange("enabled", checked)
            }
          />

          <div className="grid gap-3 lg:grid-cols-2">
            <div className="space-y-2">
              <label className="text-sm font-medium">
                {t("pages.config.turn_profile_history")}
              </label>
              {renderModeSelect({
                value: form.turnProfile.historyMode,
                onValueChange: (mode) =>
                  onTurnProfileFieldChange(
                    "historyMode",
                    mode === "off" ? "off" : "default",
                  ),
                allowCustom: false,
              })}
              <p className="text-muted-foreground text-xs leading-relaxed">
                {t("pages.config.turn_profile_history_hint")}
              </p>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">
                {t("pages.config.turn_profile_system_prompt")}
              </label>
              {renderModeSelect({
                value: form.turnProfile.systemPromptMode,
                onValueChange: (mode) =>
                  onTurnProfileFieldChange(
                    "systemPromptMode",
                    mode === "off" ? "off" : "default",
                  ),
                allowCustom: false,
              })}
              <p className="text-muted-foreground text-xs leading-relaxed">
                {t("pages.config.turn_profile_system_prompt_hint")}
              </p>
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">
                {t("pages.config.turn_profile_skills")}
              </label>
              {renderModeSelect({
                value: form.turnProfile.skillsMode,
                onValueChange: (mode) =>
                  onTurnProfileFieldChange("skillsMode", mode),
                allowCustom: true,
              })}
              <p className="text-muted-foreground text-xs leading-relaxed">
                {t("pages.config.turn_profile_skills_hint")}
              </p>
              {form.turnProfile.skillsMode === "custom" && (
                <Textarea
                  value={form.turnProfile.skillsAllowText}
                  onChange={(e) =>
                    onTurnProfileFieldChange("skillsAllowText", e.target.value)
                  }
                  placeholder={t(
                    "pages.config.turn_profile_skills_allow_placeholder",
                  )}
                  className="min-h-20 font-mono text-xs"
                />
              )}
            </div>

            <div className="space-y-2">
              <label className="text-sm font-medium">
                {t("pages.config.turn_profile_tools")}
              </label>
              {renderModeSelect({
                value: form.turnProfile.toolsMode,
                onValueChange: (mode) =>
                  onTurnProfileFieldChange("toolsMode", mode),
                allowCustom: true,
              })}
              <p className="text-muted-foreground text-xs leading-relaxed">
                {t("pages.config.turn_profile_tools_hint")}
              </p>
              {form.turnProfile.toolsMode === "custom" && (
                <Textarea
                  value={form.turnProfile.toolsAllowText}
                  onChange={(e) =>
                    onTurnProfileFieldChange("toolsAllowText", e.target.value)
                  }
                  placeholder={t(
                    "pages.config.turn_profile_tools_allow_placeholder",
                  )}
                  className="min-h-20 font-mono text-xs"
                />
              )}
            </div>
          </div>
        </div>
      </Field>
    </ConfigSectionCard>
  )
}

interface ExecSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
}

interface MCPSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
  onAddServer: () => void
  onRemoveServer: (id: string) => void
  onServerFieldChange: <K extends keyof MCPServerForm>(
    id: string,
    key: K,
    value: MCPServerForm[K],
  ) => void
}

interface EvolutionSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
}

export function EvolutionSection({
  form,
  onFieldChange,
}: EvolutionSectionProps) {
  const { t } = useTranslation()

  return (
    <ConfigSectionCard
      title={t("pages.config.sections.evolution")}
      description={t("pages.config.evolution_section_hint")}
    >
      <SwitchCardField
        label={t("pages.config.evolution_enabled")}
        hint={t("pages.config.evolution_enabled_hint")}
        layout="setting-row"
        checked={form.evolutionEnabled}
        onCheckedChange={(checked) =>
          onFieldChange("evolutionEnabled", checked)
        }
      />

      <Field
        label={t("pages.config.evolution_mode")}
        hint={t("pages.config.evolution_mode_hint")}
        layout="setting-row"
      >
        <Select
          value={form.evolutionMode}
          onValueChange={(value) => onFieldChange("evolutionMode", value)}
        >
          <SelectTrigger aria-label={t("pages.config.evolution_mode")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="observe">
              {t("pages.config.evolution_mode_observe")}
            </SelectItem>
            <SelectItem value="draft">
              {t("pages.config.evolution_mode_draft")}
            </SelectItem>
            <SelectItem value="apply">
              {t("pages.config.evolution_mode_apply")}
            </SelectItem>
          </SelectContent>
        </Select>
      </Field>

      <Field
        label={t("pages.config.evolution_state_dir")}
        hint={t("pages.config.evolution_state_dir_hint")}
        layout="setting-row"
      >
        <Input
          value={form.evolutionStateDir}
          onChange={(e) => onFieldChange("evolutionStateDir", e.target.value)}
          placeholder="e.g. /var/lib/picoclaw/evolution"
        />
      </Field>

      <Field
        label={t("pages.config.evolution_min_task_count")}
        hint={t("pages.config.evolution_min_task_count_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          value={form.evolutionMinTaskCount}
          onChange={(e) =>
            onFieldChange("evolutionMinTaskCount", e.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.evolution_min_success_ratio")}
        hint={t("pages.config.evolution_min_success_ratio_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={0.01}
          max={1}
          step="0.05"
          value={form.evolutionMinSuccessRatio}
          onChange={(e) =>
            onFieldChange("evolutionMinSuccessRatio", e.target.value)
          }
        />
      </Field>

      <Field
        label={t("pages.config.evolution_cold_path_trigger")}
        hint={t("pages.config.evolution_cold_path_trigger_hint")}
        layout="setting-row"
      >
        <Select
          value={form.evolutionColdPathTrigger}
          onValueChange={(value) =>
            onFieldChange("evolutionColdPathTrigger", value)
          }
        >
          <SelectTrigger
            aria-label={t("pages.config.evolution_cold_path_trigger")}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="after_turn">
              {t("pages.config.evolution_cold_path_after_turn")}
            </SelectItem>
            <SelectItem value="scheduled">
              {t("pages.config.evolution_cold_path_scheduled")}
            </SelectItem>
            <SelectItem value="manual">
              {t("pages.config.evolution_cold_path_manual")}
            </SelectItem>
          </SelectContent>
        </Select>
      </Field>

      {form.evolutionColdPathTrigger === "scheduled" && (
        <Field
          label={t("pages.config.evolution_cold_path_times")}
          hint={t("pages.config.evolution_cold_path_times_hint")}
          layout="setting-row"
        >
          <Textarea
            value={form.evolutionColdPathTimesText}
            placeholder={"03:00\n15:30"}
            className="min-h-[88px] font-mono text-xs"
            onChange={(e) =>
              onFieldChange("evolutionColdPathTimesText", e.target.value)
            }
          />
        </Field>
      )}
    </ConfigSectionCard>
  )
}

export function MCPSection({
  form,
  onFieldChange,
  onAddServer,
  onRemoveServer,
  onServerFieldChange,
}: MCPSectionProps) {
  const { t } = useTranslation()

  return (
    <ConfigSectionCard
      title={t("pages.config.sections.mcp")}
      description={t("pages.config.mcp_section_hint")}
    >
      <SwitchCardField
        label={t("pages.config.mcp_enabled")}
        hint={t("pages.config.mcp_enabled_hint")}
        layout="setting-row"
        checked={form.mcpEnabled}
        onCheckedChange={(checked) => onFieldChange("mcpEnabled", checked)}
      />

      {form.mcpEnabled && (
        <>
          <SwitchCardField
            label={t("pages.config.mcp_discovery_enabled")}
            hint={t("pages.config.mcp_discovery_enabled_hint")}
            layout="setting-row"
            checked={form.mcpDiscoveryEnabled}
            onCheckedChange={(checked) =>
              onFieldChange("mcpDiscoveryEnabled", checked)
            }
          />

          {form.mcpDiscoveryEnabled && (
            <>
              <Field
                label={t("pages.config.mcp_discovery_ttl")}
                hint={t("pages.config.mcp_discovery_ttl_hint")}
                layout="setting-row"
              >
                <Input
                  type="number"
                  min={1}
                  value={form.mcpDiscoveryTTL}
                  onChange={(e) =>
                    onFieldChange("mcpDiscoveryTTL", e.target.value)
                  }
                />
              </Field>

              <Field
                label={t("pages.config.mcp_discovery_max_results")}
                hint={t("pages.config.mcp_discovery_max_results_hint")}
                layout="setting-row"
              >
                <Input
                  type="number"
                  min={1}
                  value={form.mcpDiscoveryMaxSearchResults}
                  onChange={(e) =>
                    onFieldChange(
                      "mcpDiscoveryMaxSearchResults",
                      e.target.value,
                    )
                  }
                />
              </Field>

              <SwitchCardField
                label={t("pages.config.mcp_discovery_use_bm25")}
                hint={t("pages.config.mcp_discovery_use_bm25_hint")}
                layout="setting-row"
                checked={form.mcpDiscoveryUseBM25}
                disabled={
                  form.mcpDiscoveryUseBM25 && !form.mcpDiscoveryUseRegex
                }
                onCheckedChange={(checked) =>
                  onFieldChange("mcpDiscoveryUseBM25", checked)
                }
              />

              <SwitchCardField
                label={t("pages.config.mcp_discovery_use_regex")}
                hint={t("pages.config.mcp_discovery_use_regex_hint")}
                layout="setting-row"
                checked={form.mcpDiscoveryUseRegex}
                disabled={
                  form.mcpDiscoveryUseRegex && !form.mcpDiscoveryUseBM25
                }
                onCheckedChange={(checked) =>
                  onFieldChange("mcpDiscoveryUseRegex", checked)
                }
              />
            </>
          )}

          <Field
            label={t("pages.config.mcp_servers")}
            hint={t("pages.config.mcp_servers_hint")}
            layout="setting-row"
            controlClassName="md:max-w-2xl"
          >
            <div className="flex flex-col gap-3">
              {form.mcpServers.map((server) => (
                <div
                  key={server.id}
                  className="border-border rounded-md border p-3"
                >
                  <div className="mb-3 flex items-center justify-between">
                    <div className="text-sm font-medium">
                      {server.name.trim() || t("pages.config.mcp_server_new")}
                    </div>
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => onRemoveServer(server.id)}
                    >
                      <IconTrash className="size-4" />
                      {t("pages.config.mcp_server_remove")}
                    </Button>
                  </div>

                  <div className="grid gap-3 md:grid-cols-2">
                    <Input
                      value={server.name}
                      placeholder={t(
                        "pages.config.mcp_server_name_placeholder",
                      )}
                      aria-label={t("pages.config.mcp_server_name_placeholder")}
                      onChange={(e) =>
                        onServerFieldChange(server.id, "name", e.target.value)
                      }
                    />

                    <Select
                      value={server.type}
                      onValueChange={(value) =>
                        onServerFieldChange(
                          server.id,
                          "type",
                          value as MCPServerType,
                        )
                      }
                    >
                      <SelectTrigger
                        aria-label={t("pages.config.mcp_server_discovery_mode")}
                      >
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="stdio">stdio</SelectItem>
                        <SelectItem value="sse">sse</SelectItem>
                        <SelectItem value="http">http</SelectItem>
                      </SelectContent>
                    </Select>

                    <SwitchCardField
                      label={t("pages.config.mcp_server_enabled")}
                      layout="setting-row"
                      checked={server.enabled}
                      onCheckedChange={(checked) =>
                        onServerFieldChange(server.id, "enabled", checked)
                      }
                    />

                    <Select
                      value={
                        server.deferredOverride === null
                          ? "inherit"
                          : server.deferredOverride
                            ? "deferred"
                            : "eager"
                      }
                      onValueChange={(value) =>
                        onServerFieldChange(
                          server.id,
                          "deferredOverride",
                          value === "inherit" ? null : value === "deferred",
                        )
                      }
                    >
                      <SelectTrigger
                        aria-label={t("pages.config.mcp_server_discovery_mode")}
                      >
                        <SelectValue
                          placeholder={t(
                            "pages.config.mcp_server_discovery_mode",
                          )}
                        />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="inherit">
                          {t("pages.config.mcp_server_discovery_mode_inherit")}
                        </SelectItem>
                        <SelectItem value="deferred">
                          {t("pages.config.mcp_server_discovery_mode_deferred")}
                        </SelectItem>
                        <SelectItem value="eager">
                          {t("pages.config.mcp_server_discovery_mode_eager")}
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {server.type !== "stdio" ? (
                    <div className="mt-3 grid gap-3">
                      <Input
                        value={server.url}
                        placeholder={t(
                          "pages.config.mcp_server_url_placeholder",
                        )}
                        aria-label={t(
                          "pages.config.mcp_server_url_placeholder",
                        )}
                        onChange={(e) =>
                          onServerFieldChange(server.id, "url", e.target.value)
                        }
                      />
                      <Textarea
                        value={server.headersText}
                        placeholder={t(
                          "pages.config.mcp_server_headers_placeholder",
                        )}
                        aria-label={t(
                          "pages.config.mcp_server_headers_placeholder",
                        )}
                        className="min-h-[88px] font-mono text-xs"
                        onChange={(e) =>
                          onServerFieldChange(
                            server.id,
                            "headersText",
                            e.target.value,
                          )
                        }
                      />
                    </div>
                  ) : (
                    <div className="mt-3 grid gap-3">
                      <Input
                        value={server.command}
                        placeholder={t(
                          "pages.config.mcp_server_command_placeholder",
                        )}
                        aria-label={t(
                          "pages.config.mcp_server_command_placeholder",
                        )}
                        onChange={(e) =>
                          onServerFieldChange(
                            server.id,
                            "command",
                            e.target.value,
                          )
                        }
                      />
                      <Input
                        value={server.envFile}
                        placeholder={t(
                          "pages.config.mcp_server_env_file_placeholder",
                        )}
                        aria-label={t(
                          "pages.config.mcp_server_env_file_placeholder",
                        )}
                        onChange={(e) =>
                          onServerFieldChange(
                            server.id,
                            "envFile",
                            e.target.value,
                          )
                        }
                      />
                      <Textarea
                        value={server.argsText}
                        placeholder={t(
                          "pages.config.mcp_server_args_placeholder",
                        )}
                        aria-label={t(
                          "pages.config.mcp_server_args_placeholder",
                        )}
                        className="min-h-[88px] font-mono text-xs"
                        onChange={(e) =>
                          onServerFieldChange(
                            server.id,
                            "argsText",
                            e.target.value,
                          )
                        }
                      />
                      <Textarea
                        value={server.envText}
                        placeholder={t(
                          "pages.config.mcp_server_env_placeholder",
                        )}
                        aria-label={t(
                          "pages.config.mcp_server_env_placeholder",
                        )}
                        className="min-h-[88px] font-mono text-xs"
                        onChange={(e) =>
                          onServerFieldChange(
                            server.id,
                            "envText",
                            e.target.value,
                          )
                        }
                      />
                    </div>
                  )}
                </div>
              ))}

              <div>
                <Button type="button" variant="outline" onClick={onAddServer}>
                  <IconPlus className="size-4" />
                  {t("pages.config.mcp_server_add")}
                </Button>
              </div>
            </div>
          </Field>
        </>
      )}
    </ConfigSectionCard>
  )
}

export function ExecSection({ form, onFieldChange }: ExecSectionProps) {
  const { t } = useTranslation()
  const [testCommand, setTestCommand] = useState("")
  const [testResult, setTestResult] = useState<{
    allowed: boolean
    blocked: boolean
    matchedWhitelist: string | null
    matchedBlacklist: string | null
  } | null>(null)
  const [isLoading, setIsLoading] = useState(false)

  const testPatterns = async () => {
    if (!testCommand.trim()) {
      setTestResult(null)
      return
    }

    const allowPatterns = form.customAllowPatternsText
      .split("\n")
      .map((p) => p.trim())
      .filter((p) => p.length > 0)
    const denyPatterns = form.enableDenyPatterns
      ? form.customDenyPatternsText
          .split("\n")
          .map((p) => p.trim())
          .filter((p) => p.length > 0)
      : []

    setIsLoading(true)
    try {
      const res = await fetch("/api/config/test-command-patterns", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          allow_patterns: allowPatterns,
          deny_patterns: denyPatterns,
          command: testCommand,
        }),
      })
      const data = await res.json()
      setTestResult({
        allowed: data.allowed,
        blocked: data.blocked,
        matchedWhitelist: data.matched_whitelist ?? null,
        matchedBlacklist: data.matched_blacklist ?? null,
      })
    } catch {
      setTestResult(null)
    } finally {
      setIsLoading(false)
    }
  }

  return (
    <ConfigSectionCard title={t("pages.config.sections.exec")}>
      <SwitchCardField
        label={t("pages.config.exec_enabled")}
        hint={t("pages.config.exec_enabled_hint")}
        layout="setting-row"
        checked={form.execEnabled}
        onCheckedChange={(checked) => onFieldChange("execEnabled", checked)}
      />

      {form.execEnabled && (
        <>
          <SwitchCardField
            label={t("pages.config.allow_remote")}
            hint={t("pages.config.allow_remote_hint")}
            layout="setting-row"
            checked={form.allowRemote}
            onCheckedChange={(checked) => onFieldChange("allowRemote", checked)}
          />

          <SwitchCardField
            label={t("pages.config.enable_deny_patterns")}
            hint={t("pages.config.enable_deny_patterns_hint")}
            layout="setting-row"
            checked={form.enableDenyPatterns}
            onCheckedChange={(checked) =>
              onFieldChange("enableDenyPatterns", checked)
            }
          />

          {form.enableDenyPatterns && (
            <Field
              label={t("pages.config.custom_deny_patterns")}
              hint={t("pages.config.custom_deny_patterns_hint")}
              layout="setting-row"
              controlClassName="md:max-w-md"
            >
              <Textarea
                value={form.customDenyPatternsText}
                placeholder={t("pages.config.custom_patterns_placeholder")}
                className="min-h-[88px]"
                onChange={(e) =>
                  onFieldChange("customDenyPatternsText", e.target.value)
                }
              />
            </Field>
          )}

          <Field
            label={t("pages.config.custom_allow_patterns")}
            hint={t("pages.config.custom_allow_patterns_hint")}
            layout="setting-row"
            controlClassName="md:max-w-md"
          >
            <Textarea
              value={form.customAllowPatternsText}
              placeholder={t("pages.config.custom_patterns_placeholder")}
              className="min-h-[88px]"
              onChange={(e) =>
                onFieldChange("customAllowPatternsText", e.target.value)
              }
            />
          </Field>

          <Field
            label={t("pages.config.pattern_detector_title")}
            hint={t("pages.config.pattern_detector_hint")}
            layout="setting-row"
            controlClassName="md:max-w-md"
          >
            <div className="flex w-full flex-col gap-2">
              <div className="flex gap-2">
                <Input
                  value={testCommand}
                  placeholder={t(
                    "pages.config.pattern_detector_input_placeholder",
                  )}
                  onChange={(e) => setTestCommand(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      testPatterns()
                    }
                  }}
                />
                <Button onClick={testPatterns} disabled={isLoading}>
                  {t("pages.config.pattern_detector_test_button")}
                </Button>
              </div>
              {testResult && (
                <div
                  className={`rounded-md p-2 text-sm ${
                    testResult.allowed
                      ? "bg-green-500/10 text-green-600"
                      : testResult.blocked
                        ? "bg-red-500/10 text-red-600"
                        : "bg-muted text-muted-foreground"
                  }`}
                >
                  {testResult.allowed
                    ? `${t("pages.config.pattern_detector_result_allowed")}${testResult.matchedWhitelist ? ` (${testResult.matchedWhitelist})` : ""}`
                    : testResult.blocked
                      ? `${t("pages.config.pattern_detector_result_blocked")}${testResult.matchedBlacklist ? ` (${testResult.matchedBlacklist})` : ""}`
                      : t("pages.config.pattern_detector_result_no_match")}
                </div>
              )}
            </div>
          </Field>

          <Field
            label={t("pages.config.exec_timeout_seconds")}
            hint={t("pages.config.exec_timeout_seconds_hint")}
            layout="setting-row"
          >
            <Input
              type="number"
              min={0}
              value={form.execTimeoutSeconds}
              onChange={(e) =>
                onFieldChange("execTimeoutSeconds", e.target.value)
              }
            />
          </Field>
        </>
      )}
    </ConfigSectionCard>
  )
}

interface RuntimeSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
}

export function RuntimeSection({ form, onFieldChange }: RuntimeSectionProps) {
  const { t } = useTranslation()
  const selectedDmScopeOption = DM_SCOPE_OPTIONS.find(
    (scope) => scope.value === form.dmScope,
  )

  return (
    <ConfigSectionCard title={t("pages.config.sections.runtime")}>
      <Field
        label={t("pages.config.session_scope")}
        hint={t("pages.config.session_scope_hint")}
        layout="setting-row"
      >
        <Select
          value={form.dmScope}
          onValueChange={(value) => onFieldChange("dmScope", value)}
        >
          <SelectTrigger className="w-full">
            <SelectValue>
              {selectedDmScopeOption
                ? t(
                    selectedDmScopeOption.labelKey,
                    selectedDmScopeOption.labelDefault,
                  )
                : form.dmScope}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {DM_SCOPE_OPTIONS.map((scope) => (
              <SelectItem key={scope.value} value={scope.value}>
                <div className="flex flex-col gap-0.5">
                  <span className="font-medium">{t(scope.labelKey)}</span>
                  <span className="text-muted-foreground text-xs">
                    {t(scope.descKey)}
                  </span>
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>

      <SwitchCardField
        label={t("pages.config.heartbeat_enabled")}
        hint={t("pages.config.heartbeat_enabled_hint")}
        layout="setting-row"
        checked={form.heartbeatEnabled}
        onCheckedChange={(checked) =>
          onFieldChange("heartbeatEnabled", checked)
        }
      />

      {form.heartbeatEnabled && (
        <Field
          label={t("pages.config.heartbeat_interval")}
          hint={t("pages.config.heartbeat_interval_hint")}
          layout="setting-row"
        >
          <Input
            type="number"
            min={1}
            value={form.heartbeatInterval}
            onChange={(e) => onFieldChange("heartbeatInterval", e.target.value)}
          />
        </Field>
      )}
    </ConfigSectionCard>
  )
}

interface CronSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
}

export function CronSection({ form, onFieldChange }: CronSectionProps) {
  const { t } = useTranslation()

  return (
    <ConfigSectionCard title={t("pages.config.sections.cron")}>
      <SwitchCardField
        label={t("pages.config.allow_shell_execution")}
        hint={t("pages.config.allow_shell_execution_hint")}
        layout="setting-row"
        checked={form.allowCommand}
        disabled={!form.execEnabled}
        onCheckedChange={(checked) => onFieldChange("allowCommand", checked)}
      />

      <Field
        label={t("pages.config.cron_exec_timeout")}
        hint={t("pages.config.cron_exec_timeout_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={0}
          disabled={!form.execEnabled}
          value={form.cronExecTimeoutMinutes}
          onChange={(e) =>
            onFieldChange("cronExecTimeoutMinutes", e.target.value)
          }
        />
      </Field>
    </ConfigSectionCard>
  )
}

interface LauncherSectionProps {
  launcherForm: LauncherForm
  onFieldChange: UpdateLauncherField
  disabled: boolean
}

export function LauncherSection({
  launcherForm,
  onFieldChange,
  disabled,
}: LauncherSectionProps) {
  const { t } = useTranslation()

  return (
    <ConfigSectionCard
      title={t("pages.config.sections.launcher")}
      description={t("pages.config.launcher_section_hint")}
    >
      <Field
        label={t("pages.config.dashboard_password")}
        hint={t("pages.config.dashboard_password_hint")}
        layout="setting-row"
        controlClassName="md:max-w-md"
      >
        <Input
          type="password"
          value={launcherForm.dashboardPassword}
          disabled={disabled}
          autoComplete="new-password"
          placeholder={t("pages.config.dashboard_password_placeholder")}
          onChange={(e) => onFieldChange("dashboardPassword", e.target.value)}
        />
      </Field>

      {launcherForm.dashboardPassword.trim() !== "" && (
        <Field
          label={t("pages.config.dashboard_password_confirm")}
          hint={t("pages.config.dashboard_password_confirm_hint")}
          layout="setting-row"
          controlClassName="md:max-w-md"
        >
          <Input
            type="password"
            value={launcherForm.dashboardPasswordConfirm}
            disabled={disabled}
            autoComplete="new-password"
            placeholder={t(
              "pages.config.dashboard_password_confirm_placeholder",
            )}
            onChange={(e) =>
              onFieldChange("dashboardPasswordConfirm", e.target.value)
            }
          />
        </Field>
      )}

      <SwitchCardField
        label={t("pages.config.lan_access")}
        hint={t("pages.config.lan_access_hint")}
        layout="setting-row"
        checked={launcherForm.publicAccess}
        disabled={disabled}
        onCheckedChange={(checked) => onFieldChange("publicAccess", checked)}
      />

      <Field
        label={t("pages.config.server_port")}
        hint={t("pages.config.server_port_hint")}
        layout="setting-row"
      >
        <Input
          type="number"
          min={1}
          max={65535}
          value={launcherForm.port}
          disabled={disabled}
          onChange={(e) => onFieldChange("port", e.target.value)}
        />
      </Field>

      <Field
        label={t("pages.config.allowed_cidrs")}
        hint={t("pages.config.allowed_cidrs_hint")}
        layout="setting-row"
        controlClassName="md:max-w-md"
      >
        <Textarea
          value={launcherForm.allowedCIDRsText}
          disabled={disabled}
          placeholder={t("pages.config.allowed_cidrs_placeholder")}
          className="min-h-[88px]"
          onChange={(e) => onFieldChange("allowedCIDRsText", e.target.value)}
        />
      </Field>

      <SwitchCardField
        label={t("pages.config.allow_localhost_bypass")}
        hint={t("pages.config.allow_localhost_bypass_hint")}
        layout="setting-row"
        checked={launcherForm.allowLocalhostBypass}
        disabled={disabled}
        onCheckedChange={(checked) =>
          onFieldChange("allowLocalhostBypass", checked)
        }
      />

      <Field
        label={t("pages.config.trusted_proxy_cidrs")}
        hint={t("pages.config.trusted_proxy_cidrs_hint")}
        layout="setting-row"
        controlClassName="md:max-w-md"
      >
        <Textarea
          value={launcherForm.trustedProxyCIDRsText}
          disabled={disabled}
          placeholder={t("pages.config.trusted_proxy_cidrs_placeholder")}
          className="min-h-[88px]"
          onChange={(e) =>
            onFieldChange("trustedProxyCIDRsText", e.target.value)
          }
        />
      </Field>
    </ConfigSectionCard>
  )
}

interface DevicesSectionProps {
  form: CoreConfigForm
  onFieldChange: UpdateCoreField
  autoStartEnabled: boolean
  autoStartHint: string
  autoStartDisabled: boolean
  onAutoStartChange: (checked: boolean) => void
}

export function DevicesSection({
  form,
  onFieldChange,
  autoStartEnabled,
  autoStartHint,
  autoStartDisabled,
  onAutoStartChange,
}: DevicesSectionProps) {
  const { t } = useTranslation()

  return (
    <ConfigSectionCard title={t("pages.config.sections.devices")}>
      <SwitchCardField
        label={t("pages.config.devices_enabled")}
        hint={t("pages.config.devices_enabled_hint")}
        layout="setting-row"
        checked={form.devicesEnabled}
        onCheckedChange={(checked) => onFieldChange("devicesEnabled", checked)}
      />

      <SwitchCardField
        label={t("pages.config.monitor_usb")}
        hint={t("pages.config.monitor_usb_hint")}
        layout="setting-row"
        checked={form.monitorUSB}
        onCheckedChange={(checked) => onFieldChange("monitorUSB", checked)}
      />

      <SwitchCardField
        label={t("pages.config.autostart_label")}
        hint={autoStartHint}
        layout="setting-row"
        checked={autoStartEnabled}
        disabled={autoStartDisabled}
        onCheckedChange={onAutoStartChange}
      />
    </ConfigSectionCard>
  )
}
