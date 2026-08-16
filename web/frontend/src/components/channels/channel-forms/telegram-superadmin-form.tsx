import { IconLoader2, IconShieldLock } from "@tabler/icons-react"
import { useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  EMPTY_SUPERADMIN,
  type SessionSuperadminConfig,
  deleteSessionSuperadmin,
  getSessionSuperadmin,
  putSessionSuperadmin,
} from "@/api/session-dashboard"
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

interface TelegramSuperadminFormProps {
  defaultBotAccount: string
}

function configured(value: SessionSuperadminConfig): boolean {
  return (
    value.telegram_user_id.trim() !== "" ||
    value.bot_account.trim() !== "" ||
    value.agent_id.trim() !== ""
  )
}

export function TelegramSuperadminForm({
  defaultBotAccount,
}: TelegramSuperadminFormProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [stored, setStored] = useState<SessionSuperadminConfig>({
    ...EMPTY_SUPERADMIN,
  })
  const [draft, setDraft] = useState<SessionSuperadminConfig>({
    ...EMPTY_SUPERADMIN,
    bot_account: defaultBotAccount,
  })

  const hasStoredConfig = useMemo(() => configured(stored), [stored])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    void getSessionSuperadmin()
      .then((value) => {
        if (cancelled) return
        const normalized = {
          ...value,
          bot_account: value.bot_account || defaultBotAccount,
        }
        setStored(value)
        setDraft(normalized)
        setError("")
      })
      .catch((cause) => {
        if (cancelled) return
        setError(
          cause instanceof Error
            ? cause.message
            : t("channels.telegram.superadmin.loadError"),
        )
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [defaultBotAccount, t])

  const setField = <K extends keyof SessionSuperadminConfig,>(
    key: K,
    value: SessionSuperadminConfig[K],
  ) => {
    setDraft((current) => ({ ...current, [key]: value }))
    setError("")
    setNotice("")
  }

  const save = async () => {
    setSaving(true)
    setError("")
    setNotice("")
    try {
      const saved = await putSessionSuperadmin({
        ...draft,
        telegram_user_id: draft.telegram_user_id.trim(),
        bot_account: draft.bot_account.trim(),
        agent_id: draft.agent_id.trim(),
      })
      setStored(saved)
      setDraft({
        ...saved,
        bot_account: saved.bot_account || defaultBotAccount,
      })
      setNotice(t("channels.telegram.superadmin.saved"))
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : t("channels.telegram.superadmin.saveError"),
      )
    } finally {
      setSaving(false)
    }
  }

  const remove = async () => {
    setSaving(true)
    setError("")
    setNotice("")
    try {
      const cleared = await deleteSessionSuperadmin()
      setStored(cleared)
      setDraft({ ...EMPTY_SUPERADMIN, bot_account: defaultBotAccount })
      setNotice(t("channels.telegram.superadmin.deleted"))
    } catch (cause) {
      setError(
        cause instanceof Error
          ? cause.message
          : t("channels.telegram.superadmin.deleteError"),
      )
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card className="shadow-sm">
      <CardHeader className="border-border/60 border-b pb-5">
        <div className="flex items-start gap-3">
          <IconShieldLock className="text-muted-foreground mt-0.5 size-5 shrink-0" />
          <div className="space-y-1">
            <CardTitle>{t("channels.telegram.superadmin.title")}</CardTitle>
            <CardDescription>
              {t("channels.telegram.superadmin.description")}
            </CardDescription>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-5 pt-5">
        {loading ? (
          <div className="text-muted-foreground flex items-center gap-2 text-sm">
            <IconLoader2 className="size-4 animate-spin" />
            {t("channels.telegram.superadmin.loading")}
          </div>
        ) : (
          <>
            <Field
              label={t("channels.telegram.superadmin.userId")}
              hint={t("channels.telegram.superadmin.userIdHint")}
              required={draft.enabled}
            >
              <Input
                inputMode="numeric"
                pattern="[0-9]*"
                value={draft.telegram_user_id}
                onChange={(event) =>
                  setField("telegram_user_id", event.target.value)
                }
                placeholder="123456789"
              />
            </Field>

            <Field
              label={t("channels.telegram.superadmin.botAccount")}
              hint={t("channels.telegram.superadmin.botAccountHint")}
              required={draft.enabled}
            >
              <Input
                value={draft.bot_account}
                onChange={(event) =>
                  setField("bot_account", event.target.value)
                }
                placeholder={defaultBotAccount || "telegram"}
              />
            </Field>

            <Field
              label={t("channels.telegram.superadmin.agent")}
              hint={t("channels.telegram.superadmin.agentHint")}
              required={draft.enabled}
            >
              <Input
                value={draft.agent_id}
                onChange={(event) => setField("agent_id", event.target.value)}
                placeholder="main"
              />
            </Field>

            <SwitchCardField
              label={t("channels.telegram.superadmin.enabled")}
              hint={t("channels.telegram.superadmin.enabledHint")}
              checked={draft.enabled}
              onCheckedChange={(checked) => setField("enabled", checked)}
              ariaLabel={t("channels.telegram.superadmin.enabled")}
            />

            <SwitchCardField
              label={t("channels.telegram.superadmin.includeLegacy")}
              hint={t("channels.telegram.superadmin.includeLegacyHint")}
              checked={draft.include_legacy_unknown}
              onCheckedChange={(checked) =>
                setField("include_legacy_unknown", checked)
              }
              ariaLabel={t("channels.telegram.superadmin.includeLegacy")}
            />

            {error && (
              <p className="text-destructive text-sm" role="alert">
                {error}
              </p>
            )}
            {notice && (
              <p className="text-muted-foreground text-sm" role="status">
                {notice}
              </p>
            )}

            <div className="flex flex-wrap gap-2">
              <Button disabled={saving} onClick={() => void save()}>
                {saving && <IconLoader2 className="size-4 animate-spin" />}
                {t(
                  hasStoredConfig
                    ? "channels.telegram.superadmin.replace"
                    : "channels.telegram.superadmin.save",
                )}
              </Button>
              {hasStoredConfig && (
                <Button
                  variant="destructive"
                  disabled={saving}
                  onClick={() => void remove()}
                >
                  {t("channels.telegram.superadmin.delete")}
                </Button>
              )}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  )
}
