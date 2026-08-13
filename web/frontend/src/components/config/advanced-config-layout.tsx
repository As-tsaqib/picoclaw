import type { ComponentProps, ReactNode } from "react"

import {
  EvolutionSafetySection,
  MemoryRecallSection,
  RuntimeConcurrencySection,
} from "@/components/config/config-sections"
import {
  CurrentUserProfileManagementSection,
  EvolutionManagementSection,
  MemoryManagementSection,
} from "@/components/config/memory-evolution-management"

interface AdvancedConfigLayoutProps {
  runtimeConcurrency: ComponentProps<typeof RuntimeConcurrencySection>
  memoryRecall: ComponentProps<typeof MemoryRecallSection>
  evolutionSafety: ComponentProps<typeof EvolutionSafetySection>
  management?: {
    workspaceMemory?: ReactNode
    currentUserProfile?: ReactNode
    evolution?: ReactNode
  }
}

export function AdvancedConfigLayout({
  runtimeConcurrency,
  memoryRecall,
  evolutionSafety,
  management,
}: AdvancedConfigLayoutProps) {
  return (
    <>
      <RuntimeConcurrencySection {...runtimeConcurrency} />
      <MemoryRecallSection {...memoryRecall} />
      {management?.workspaceMemory ?? <MemoryManagementSection />}
      {management?.currentUserProfile ?? (
        <CurrentUserProfileManagementSection />
      )}
      <EvolutionSafetySection {...evolutionSafety} />
      {management?.evolution ?? <EvolutionManagementSection />}
    </>
  )
}
