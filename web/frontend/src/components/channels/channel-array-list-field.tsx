import { IconX } from "@tabler/icons-react"
import {
  type KeyboardEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react"
import { useTranslation } from "react-i18next"

import {
  mergeUniqueStringItems,
  parseConservativeStringListInput,
} from "@/components/channels/channel-array-utils"
import { Field } from "@/components/shared-form"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"

type StringListParser = (raw: string) => string[]
type StringListValidator = (raw: string) => string | null
export type ArrayFieldFlusher = () => string[] | null

type RegisterArrayFieldFlusher = (
  fieldPath: string,
  flusher: ArrayFieldFlusher | null,
) => void

function areStringArraysEqual(left: string[], right: string[]): boolean {
  if (left.length !== right.length) {
    return false
  }
  return left.every((item, index) => item === right[index])
}

interface ChannelArrayListFieldProps {
  label: string
  hint?: string
  error?: string
  required?: boolean
  value: string[]
  onChange: (value: string[]) => void
  placeholder?: string
  inputAriaLabel?: string
  parser?: StringListParser
  validateDraft?: StringListValidator
  fieldPath?: string
  registerFlusher?: RegisterArrayFieldFlusher
  resetVersion?: number
}

export function ChannelArrayListField({
  label,
  hint,
  error,
  required,
  value,
  onChange,
  placeholder,
  inputAriaLabel,
  parser = parseConservativeStringListInput,
  validateDraft,
  fieldPath,
  registerFlusher,
  resetVersion,
}: ChannelArrayListFieldProps) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState("")
  const [draftError, setDraftError] = useState("")
  const draftRef = useRef("")
  const valueRef = useRef(value)
  const localValueRef = useRef(value)
  const parserRef = useRef(parser)
  const validateDraftRef = useRef(validateDraft)
  const onChangeRef = useRef(onChange)

  useEffect(() => {
    valueRef.current = value
    localValueRef.current = value
  }, [value])

  useEffect(() => {
    draftRef.current = ""
    setDraft("")
    setDraftError("")
  }, [resetVersion])

  useEffect(() => {
    parserRef.current = parser
  }, [parser])

  useEffect(() => {
    validateDraftRef.current = validateDraft
  }, [validateDraft])

  useEffect(() => {
    onChangeRef.current = onChange
  }, [onChange])

  const commitDraft = useCallback(() => {
    const rawDraft = draftRef.current
    if (rawDraft.trim() === "") {
      if (!areStringArraysEqual(localValueRef.current, valueRef.current)) {
        return localValueRef.current
      }
      draftRef.current = ""
      setDraft("")
      setDraftError("")
      return null
    }
    const validationError = validateDraftRef.current?.(rawDraft) ?? null
    if (validationError) {
      setDraftError(validationError)
      return null
    }
    draftRef.current = ""
    setDraft("")
    setDraftError("")
    const nextItems = parserRef.current(rawDraft)
    if (nextItems.length === 0) {
      return null
    }
    const mergedItems = mergeUniqueStringItems(localValueRef.current, nextItems)
    localValueRef.current = mergedItems
    onChangeRef.current(mergedItems)
    return mergedItems
  }, [])

  useEffect(() => {
    if (!fieldPath || !registerFlusher) {
      return
    }
    registerFlusher(fieldPath, commitDraft)
    return () => registerFlusher(fieldPath, null)
  }, [commitDraft, fieldPath, registerFlusher])

  const handleKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
    if (event.key !== "Enter") {
      return
    }
    event.preventDefault()
    commitDraft()
  }

  const handleRemove = (index: number) => {
    const nextValue = value.filter((_, itemIndex) => itemIndex !== index)
    localValueRef.current = nextValue
    onChangeRef.current(nextValue)
  }

  return (
    <Field
      label={label}
      hint={hint}
      error={draftError || error}
      required={required}
    >
      <div className="space-y-3">
        {value.length > 0 && (
          <div className="flex flex-wrap gap-2">
            {value.map((item, index) => (
              <span
                key={`${item}-${index}`}
                className="bg-muted text-foreground inline-flex max-w-full items-center gap-1 rounded-md border px-2 py-1 text-xs"
              >
                <span className="break-all">{item}</span>
                <button
                  type="button"
                  onClick={() => handleRemove(index)}
                  className="text-muted-foreground hover:text-foreground shrink-0 transition-colors"
                  aria-label={t("channels.field.removeListItem", {
                    value: item,
                  })}
                >
                  <IconX className="size-3" />
                </button>
              </span>
            ))}
          </div>
        )}

        <div className="flex gap-2">
          <Input
            value={draft}
            onChange={(event) => {
              const nextDraft = event.target.value
              draftRef.current = nextDraft
              setDraft(nextDraft)
              if (draftError) {
                setDraftError("")
              }
            }}
            onKeyDown={handleKeyDown}
            placeholder={placeholder}
            aria-label={inputAriaLabel}
            aria-invalid={draftError !== ""}
          />
          <Button
            type="button"
            size="sm"
            onClick={commitDraft}
            disabled={draft.trim() === ""}
          >
            {t("common.confirm")}
          </Button>
        </div>
      </div>
    </Field>
  )
}
