import { useTranslation } from "react-i18next"

import { cn } from "@/lib/utils"

export type ConfigPageTab = "settings" | "advanced"

interface ConfigTabsProps {
  activeTab: ConfigPageTab
  onChange: (tab: ConfigPageTab) => void
}

const tabs: Array<{
  key: ConfigPageTab
  translationKey: string
}> = [
  {
    key: "settings",
    translationKey: "pages.config.tabs.settings",
  },
  {
    key: "advanced",
    translationKey: "pages.config.tabs.advanced",
  },
]

export function ConfigTabs({ activeTab, onChange }: ConfigTabsProps) {
  const { t } = useTranslation()

  const selectAndFocus = (tab: ConfigPageTab) => {
    onChange(tab)
    globalThis.document
      ?.getElementById(`config-tab-${tab}`)
      ?.focus({ preventScroll: true })
  }

  return (
    <div className="border-border/60 shrink-0 border-b px-3 pt-2 sm:px-6">
      <div
        className="flex max-w-full gap-6 overflow-x-auto sm:gap-8"
        role="tablist"
        aria-label={t("pages.config.tabs.label")}
      >
        {tabs.map((tab, index) => (
          <button
            key={tab.key}
            id={`config-tab-${tab.key}`}
            type="button"
            role="tab"
            aria-selected={activeTab === tab.key}
            aria-controls={`config-panel-${tab.key}`}
            tabIndex={activeTab === tab.key ? 0 : -1}
            onClick={() => onChange(tab.key)}
            onKeyDown={(event) => {
              let nextIndex: number
              if (event.key === "ArrowRight") {
                nextIndex = (index + 1) % tabs.length
              } else if (event.key === "ArrowLeft") {
                nextIndex = (index - 1 + tabs.length) % tabs.length
              } else if (event.key === "Home") {
                nextIndex = 0
              } else if (event.key === "End") {
                nextIndex = tabs.length - 1
              } else {
                return
              }
              event.preventDefault()
              selectAndFocus(tabs[nextIndex].key)
            }}
            className={cn(
              "hover:text-foreground focus-visible:ring-ring relative shrink-0 cursor-pointer pb-4 text-sm font-medium transition-colors outline-none focus-visible:ring-2 focus-visible:ring-offset-2",
              activeTab === tab.key
                ? "text-foreground"
                : "text-muted-foreground",
            )}
          >
            {t(tab.translationKey)}
            {activeTab === tab.key && (
              <span
                className="bg-primary absolute inset-x-0 bottom-0 h-0.5 rounded-t-full"
                aria-hidden="true"
              />
            )}
          </button>
        ))}
      </div>
    </div>
  )
}
