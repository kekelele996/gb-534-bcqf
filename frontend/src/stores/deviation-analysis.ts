import { ref } from 'vue'
import { defineStore } from 'pinia'
import { getAnalysis, listAnalyses, replayAnalysis, reviewPhase, runAnalysis, transitionAnalysis } from '../api/deviation-analysis'
import { errorMessage } from '../api/client'
import type { AnalysisState, DeviationAnalysis, PhaseReviewDecision } from '../types/deviation-analysis'

export const useAnalysisStore = defineStore('deviation-analyses', () => {
  const items = ref<DeviationAnalysis[]>([])
  const selected = ref<DeviationAnalysis | null>(null)
  const loading = ref(false)
  const running = ref(false)
  const phaseSaving = ref<string>('')
  const error = ref('')
  async function hydrate(id: number) {
    selected.value = await getAnalysis(id)
  }
  async function load(selectId?: number) {
    loading.value = true; error.value = ''
    try {
      const page = await listAnalyses({ page_size: 100 })
      items.value = page.items
      const targetId = selectId ?? selected.value?.id
      const match = targetId ? items.value.find((item) => item.id === targetId) : undefined
      const next = match ?? items.value[0] ?? null
      // List rows do not carry per-phase review data; hydrate the selected
      // analysis with its full detail so pending/claimed state is accurate.
      if (next) await hydrate(next.id)
      else selected.value = null
    } catch (cause) { error.value = errorMessage(cause) }
    finally { loading.value = false }
  }
  async function run(seriesId: number, key: string) {
    running.value = true
    try { await runAnalysis(seriesId, key); await load() }
    finally { running.value = false }
  }
  async function transition(state: AnalysisState, comment = '') {
    if (!selected.value) return
    selected.value = await transitionAnalysis(selected.value.id, state, comment)
    await load(selected.value.id)
  }
  async function review(phase: string, decision: PhaseReviewDecision, cause = '', rationale = '') {
    if (!selected.value) return
    phaseSaving.value = phase
    try {
      selected.value = await reviewPhase(selected.value.id, { phase, decision, cause, rationale })
      await load(selected.value.id)
    } finally { phaseSaving.value = '' }
  }
  async function replay() {
    if (!selected.value) return
    selected.value = await replayAnalysis(selected.value.id)
    await load(selected.value.id)
  }
  return { items, selected, loading, running, phaseSaving, error, load, hydrate, run, transition, review, replay }
})
