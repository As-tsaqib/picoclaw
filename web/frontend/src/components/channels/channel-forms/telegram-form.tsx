import {
  IconAlertTriangle,
  IconInfoCircle,
  IconLock,
} from "@tabler/icons-react"
import { useTranslation } from "react-i18next"

import type { ChannelConfig } from "@/api/channels"
import {
  type ArrayFieldFlusher,
  ChannelArrayListField,
} from "@/components/channels/channel-array-list-field"
import {
  asStringArray,
  parseAllowFromInput,
} from "@/components/channels/channel-array-utils"
import { getSecretInputPlaceholder } from "@/components/channels/channel-config-fields"
import { Field, KeyInput, SwitchCardField } from "@/components/shared-form"
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

import { StreamingConfigField } from "./streaming-config-field"
import {
  getTelegramEphemeralCommands,
  getTelegramEphemeralMode,
  isTelegramCommandInputValid,
  isTelegramPersonalSessionIsolationEnabled,
  parseTelegramCommandInput,
  setTelegramEphemeralCommands,
  setTelegramEphemeralMode,
  setTelegramPersonalSessionIsolation,
} from "./telegram-ephemeral-config"

interface TelegramFormProps {
  config: ChannelConfig
  onChange: (key: string, value: unknown) => void
  configuredSecrets: string[]
  fieldErrors?: Record<string, string>
  registerArrayFieldFlusher?: (
    fieldPath: string,
    flusher: ArrayFieldFlusher | null,
  ) => void
  arrayFieldResetVersion?: number
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value && typeof value === "object" && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function asBool(value: unknown): boolean {
  return value === true
}

export function TelegramForm({
  config,
  onChange,
  configuredSecrets,
  fieldErrors = {},
  registerArrayFieldFlusher,
  arrayFieldResetVersion,
}: TelegramFormProps) {
  const { t } = useTranslation()
  const typingConfig = asRecord(config.typing)
  const placeholderConfig = asRecord(config.placeholder)
  const placeholderEnabled = asBool(placeholderConfig.enabled)
  const ephemeralConfig = config.ephemeral
  const ephemeralMode = getTelegramEphemeralMode(ephemeralConfig)
  const ephemeralCommands = getTelegramEphemeralCommands(ephemeralConfig)
  const ephemeralEnabled = ephemeralMode !== "off"
  const personalSessionIsolation =
    isTelegramPersonalSessionIsolationEnabled(ephemeralConfig)

  const handleEphemeralModeChange = (value: string) => {
    if (value !== "off" && value !== "commands" && value !== "all") {
      return
    }
    onChange("ephemeral", setTelegramEphemeralMode(ephemeralConfig, value))
  }

  return (
    <div className="space-y-6">
      <Card className="shadow-sm">
        <CardHeader className="border-border/60 border-b pb-5">
          <CardTitle>{t("channels.telegram.chatbot.title")}</CardTitle>
          <CardDescription>
            {t("channels.telegram.chatbot.description")}
          </CardDescription>
        </CardHeader>
        <CardContent className="divide-border/60 divide-y px-6 py-0 [&>div]:py-5">
          <Field
            label={t("channels.field.token")}
            required
            hint={t("channels.form.desc.token")}
            error={fieldErrors.token}
          >
            <KeyInput
              value={asString(config._token)}
              onChange={(v) => onChange("_token", v)}
              placeholder={getSecretInputPlaceholder(
                configuredSecrets,
                "token",
                t("channels.field.secretHintSet"),
                t("channels.field.tokenPlaceholder"),
              )}
            />
          </Field>

          <Field
            label={t("channels.field.baseUrl")}
            hint={t("channels.form.desc.baseUrl")}
          >
            <Input
              value={asString(config.base_url)}
              onChange={(e) => onChange("base_url", e.target.value)}
              placeholder="https://api.telegram.org"
            />
          </Field>
        </CardContent>
      </Card>

      <Card className="shadow-sm">
        <CardHeader className="border-border/60 border-b pb-5">
          <CardTitle>{t("channels.telegram.ephemeral.title")}</CardTitle>
          <CardDescription>
            {t("channels.telegram.ephemeral.description")}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-5">
          <Field
            label={t("channels.telegram.ephemeral.modeLabel")}
            hint={t(
              `channels.telegram.ephemeral.modeDescription.${ephemeralMode}`,
            )}
          >
            <Select
              value={ephemeralMode}
              onValueChange={handleEphemeralModeChange}
            >
              <SelectTrigger
                className="w-full"
                aria-label={t("channels.telegram.ephemeral.modeLabel")}
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent position="popper">
                <SelectItem value="off">
                  {t("channels.telegram.ephemeral.modeOption.off")}
                </SelectItem>
                <SelectItem value="commands">
                  {t("channels.telegram.ephemeral.modeOption.commands")}
                </SelectItem>
                <SelectItem value="all">
                  {t("channels.telegram.ephemeral.modeOption.all")}
                </SelectItem>
              </SelectContent>
            </Select>
          </Field>

          {ephemeralMode === "commands" && (
            <div className="border-border/60 space-y-2 border-t pt-5">
              <ChannelArrayListField
                label={t("channels.telegram.ephemeral.commandsLabel")}
                hint={t("channels.telegram.ephemeral.commandsDescription")}
                value={ephemeralCommands}
                onChange={(commands) =>
                  onChange(
                    "ephemeral",
                    setTelegramEphemeralCommands(ephemeralConfig, commands),
                  )
                }
                placeholder={t(
                  "channels.telegram.ephemeral.commandsPlaceholder",
                )}
                inputAriaLabel={t("channels.telegram.ephemeral.commandsLabel")}
                parser={parseTelegramCommandInput}
                validateDraft={(raw) =>
                  isTelegramCommandInputValid(raw)
                    ? null
                    : t("channels.telegram.ephemeral.invalidCommand")
                }
                fieldPath="ephemeral.commands"
                registerFlusher={registerArrayFieldFlusher}
                resetVersion={arrayFieldResetVersion}
              />
              {ephemeralCommands.length === 0 && (
                <p
                  className="text-muted-foreground text-xs leading-normal"
                  aria-live="polite"
                >
                  {t("channels.telegram.ephemeral.emptyCommands")}
                </p>
              )}
            </div>
          )}

          <div className="border-border/60 border-t pt-5">
            <SwitchCardField
              label={t("channels.telegram.ephemeral.isolationLabel")}
              hint={t("channels.telegram.ephemeral.isolationDescription")}
              checked={ephemeralEnabled ? true : personalSessionIsolation}
              onCheckedChange={(checked) =>
                onChange(
                  "ephemeral",
                  setTelegramPersonalSessionIsolation(ephemeralConfig, checked),
                )
              }
              disabled={ephemeralEnabled}
              ariaLabel={t("channels.telegram.ephemeral.isolationLabel")}
            >
              {ephemeralEnabled && (
                <p className="text-muted-foreground flex items-start gap-1.5 text-xs leading-normal">
                  <IconLock className="mt-0.5 size-3.5 shrink-0" />
                  <span>
                    {t("channels.telegram.ephemeral.isolationLocked")}
                  </span>
                </p>
              )}
            </SwitchCardField>
          </div>

          {ephemeralMode === "commands" && (
            <div
              className="flex items-start gap-3 rounded-lg border border-blue-200 bg-blue-50 px-4 py-3 text-blue-950 dark:border-blue-900 dark:bg-blue-950/40 dark:text-blue-100"
              role="note"
            >
              <IconInfoCircle className="mt-0.5 size-4 shrink-0" />
              <ul className="list-disc space-y-1 pl-4 text-xs leading-normal">
                <li>
                  {t("channels.telegram.ephemeral.warning.commandsSelected")}
                </li>
                <li>
                  {t("channels.telegram.ephemeral.warning.commandsPublic")}
                </li>
                <li>
                  {t("channels.telegram.ephemeral.warning.commandsIsolated")}
                </li>
              </ul>
            </div>
          )}

          {ephemeralMode === "all" && (
            <div
              className="flex items-start gap-3 rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-amber-950 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-100"
              role="note"
            >
              <IconAlertTriangle className="mt-0.5 size-4 shrink-0" />
              <ul className="list-disc space-y-1 pl-4 text-xs leading-normal">
                <li>{t("channels.telegram.ephemeral.warning.allAdmin")}</li>
                <li>{t("channels.telegram.ephemeral.warning.allDelivery")}</li>
                <li>
                  {t("channels.telegram.ephemeral.warning.allVisibility")}
                </li>
              </ul>
            </div>
          )}

          {ephemeralEnabled && (
            <div
              className="bg-muted/50 text-muted-foreground flex items-start gap-3 rounded-lg border px-4 py-3"
              role="note"
            >
              <IconInfoCircle className="mt-0.5 size-4 shrink-0" />
              <ul className="list-disc space-y-1 pl-4 text-xs leading-normal">
                <li>{t("channels.telegram.ephemeral.warning.botApi")}</li>
                <li>{t("channels.telegram.ephemeral.warning.streaming")}</li>
                <li>{t("channels.telegram.ephemeral.warning.richTables")}</li>
                <li>{t("channels.telegram.ephemeral.warning.failClosed")}</li>
              </ul>
            </div>
          )}
        </CardContent>
      </Card>

      <Card className="shadow-sm">
        <CardContent className="divide-border/60 divide-y px-6 py-0 [&>div]:py-5">
          <Field
            label={t("channels.field.proxy")}
            hint={t("channels.form.desc.proxy")}
          >
            <Input
              value={asString(config.proxy)}
              onChange={(e) => onChange("proxy", e.target.value)}
              placeholder="http://127.0.0.1:7890"
            />
          </Field>
          <ChannelArrayListField
            label={t("channels.field.allowFrom")}
            hint={t("channels.form.desc.allowFrom")}
            value={asStringArray(config.allow_from)}
            onChange={(value) => onChange("allow_from", value)}
            placeholder={t("channels.field.allowFromPlaceholder")}
            parser={parseAllowFromInput}
            fieldPath="allow_from"
            registerFlusher={registerArrayFieldFlusher}
            resetVersion={arrayFieldResetVersion}
          />

          <div>
            <SwitchCardField
              label={t("channels.field.typingEnabled")}
              hint={t("channels.form.desc.typingEnabled")}
              checked={asBool(typingConfig.enabled)}
              onCheckedChange={(checked) =>
                onChange("typing", { ...typingConfig, enabled: checked })
              }
              ariaLabel={t("channels.field.typingEnabled")}
            />
          </div>

          <div>
            <StreamingConfigField
              value={config.streaming}
              onChange={(value) => onChange("streaming", value)}
            />
          </div>

          <div>
            <SwitchCardField
              label={t("channels.field.placeholderEnabled")}
              hint={t("channels.form.desc.placeholderEnabled")}
              checked={placeholderEnabled}
              onCheckedChange={(checked) =>
                onChange("placeholder", {
                  ...placeholderConfig,
                  enabled: checked,
                })
              }
              ariaLabel={t("channels.field.placeholderEnabled")}
            >
              {placeholderEnabled && (
                <div className="space-y-1">
                  <Input
                    value={asString(placeholderConfig.text)}
                    onChange={(e) =>
                      onChange("placeholder", {
                        ...placeholderConfig,
                        text: e.target.value,
                      })
                    }
                    placeholder={t("channels.field.placeholderText")}
                    aria-label={t("channels.field.placeholderText")}
                  />
                </div>
              )}
            </SwitchCardField>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
