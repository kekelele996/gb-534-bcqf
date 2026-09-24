<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { BadgeCheck, CornerUpLeft, ShieldQuestion, UserCheck } from 'lucide-vue-next'
import type { DeviationAnalysis, PhaseReviewDecision } from '../../types/deviation-analysis'
import { useAnalysisStore } from '../../stores/deviation-analysis'
import { ApiError } from '../../api/client'
import PhaseBadge from './PhaseBadge.vue'

const props = defineProps<{ analysis: DeviationAnalysis; currentUserId?: number; canReview: boolean }>()
const analyses = useAnalysisStore()

const decisionLabels: Record<PhaseReviewDecision, string> = {
  accepted: '已认可证据',
  excluded: '已排除疑似原因',
  returned: '已退回调查',
}
const decisionTypes: Record<PhaseReviewDecision, 'success' | 'warning' | 'danger'> = {
  accepted: 'success',
  excluded: 'warning',
  returned: 'danger',
}

interface Draft {
  decision: PhaseReviewDecision | ''
  cause: string
  rationale: string
}
const drafts = reactive<Record<string, Draft>>({})

function draftFor(phase: string): Draft {
  if (!drafts[phase]) drafts[phase] = { decision: '', cause: '', rationale: '' }
  return drafts[phase]
}

watch(() => props.analysis.id, () => {
  for (const key of Object.keys(drafts)) delete drafts[key]
}, { immediate: true })

const highRiskScores = computed(() =>
  props.analysis.phase_scores_json
    .filter((score) => score.weighted_deviation >= 0.2 - 1e-9)
    .sort((a, b) => b.weighted_deviation - a.weighted_deviation),
)

function candidates(phase: string): string[] {
  return props.analysis.phase_candidate_causes?.[phase] ?? []
}
function events(phase: string) {
  return props.analysis.phase_review_events.filter((event) => event.phase === phase)
}
function ownerOf(phase: string) {
  return props.analysis.phase_reviews?.[phase]
}
function claimedByOther(phase: string) {
  const owner = ownerOf(phase)
  return Boolean(owner && props.currentUserId && owner.reviewed_by !== props.currentUserId)
}
const phaseReviewOpen = computed(() =>
  ['completed', 'reviewed', 'investigating'].includes(props.analysis.analysis_state),
)

async function submit(phase: string, decision: PhaseReviewDecision) {
  const draft = draftFor(phase)
  draft.decision = decision
  if ((decision === 'returned' || decision === 'excluded') && !draft.rationale.trim()) {
    ElMessage.error(decision === 'returned' ? '退回调查必须填写依据' : '排除疑似原因必须填写依据')
    return
  }
  if (decision === 'excluded' && !draft.cause) {
    ElMessage.error('请先选择要排除的具体疑似原因')
    return
  }
  try {
    await analyses.review(phase, decision, draft.cause, draft.rationale.trim())
    ElMessage.success(`阶段「${phase}」结论已记录`)
  } catch (error) {
    if (error instanceof ApiError) ElMessage.error(error.message)
    else ElMessage.error('阶段复核提交失败')
  }
}

function formatTime(value: string) {
  return new Date(value).toLocaleString()
}
</script>

<template>
  <section class="phase-review-panel">
    <div class="phase-review-heading">
      <div><ShieldQuestion :size="19" /><span><strong>高风险阶段逐项复核</strong><small>加权偏差达到 20% 及以上的阶段须逐个处理，确认前不能留有未结论或退回调查的阶段</small></span></div>
      <el-tag v-if="!analysis.pending_review_phases.length" type="success" effect="light">高风险阶段均已结论</el-tag>
      <el-tag v-else type="danger" effect="light">待处理 {{ analysis.pending_review_phases.length }} 项</el-tag>
    </div>

    <el-alert
      v-if="analysis.pending_review_phases.length"
      type="warning"
      :closable="false"
      show-icon
      class="phase-review-blocker"
      :title="`以下阶段阻塞确认：${analysis.pending_review_phases.join('、')}`"
      description="请逐个认可证据或排除疑似原因；处于退回调查的阶段需重新调查后再给出结论。"
    />

    <div v-if="!highRiskScores.length" class="phase-review-empty">
      本次分析没有加权偏差达到 20% 的阶段，无需逐项阶段复核。
    </div>

    <article
      v-for="score in highRiskScores"
      :key="score.phase"
      class="phase-review-card"
      :class="{ 'is-returned': ownerOf(score.phase)?.decision === 'returned', 'is-pending': analysis.pending_review_phases.includes(score.phase) }"
    >
      <header>
        <PhaseBadge :phase="score.phase" />
        <strong class="phase-review-score">{{ (score.weighted_deviation * 100).toFixed(1) }}%</strong>
        <el-tag
          v-if="ownerOf(score.phase)"
          :type="decisionTypes[ownerOf(score.phase)!.decision]"
          effect="dark"
          size="small"
        >{{ decisionLabels[ownerOf(score.phase)!.decision] }}</el-tag>
        <el-tag v-else type="info" effect="plain" size="small">待处理</el-tag>
      </header>

      <div v-if="ownerOf(score.phase)" class="phase-review-meta">
        <UserCheck :size="14" />
        <span>
          首个处理人：{{ ownerOf(score.phase)!.reviewed_by_name }} · {{ formatTime(ownerOf(score.phase)!.reviewed_at) }}
        </span>
      </div>
      <div v-if="ownerOf(score.phase)?.cause" class="phase-review-cause">
        排除原因：{{ ownerOf(score.phase)!.cause }}
      </div>
      <div v-if="ownerOf(score.phase)?.rationale" class="phase-review-rationale">
        依据：{{ ownerOf(score.phase)!.rationale }}
      </div>

      <template v-if="canReview && phaseReviewOpen">
        <el-alert
          v-if="claimedByOther(score.phase)"
          type="info"
          :closable="false"
          show-icon
          :title="`该阶段已被复核人 ${ownerOf(score.phase)!.reviewed_by_name} 接收，不能重复提交`"
          class="phase-review-claimed"
        />
        <template v-else>
          <el-select
            v-model="draftFor(score.phase).cause"
            class="phase-review-cause-select"
            placeholder="排除时选择具体疑似原因"
            clearable
            filterable
            :disabled="!candidates(score.phase).length"
          >
            <el-option
              v-for="cause in candidates(score.phase)"
              :key="cause"
              :label="cause"
              :value="cause"
            />
          </el-select>
          <el-input
            v-model="draftFor(score.phase).rationale"
            type="textarea"
            :autosize="{ minRows: 2, maxRows: 4 }"
            placeholder="退回调查或排除疑似原因时必须填写依据；认可证据可留空"
          />
          <div class="phase-review-actions">
            <el-button
              type="success"
              plain
              :loading="analyses.phaseSaving === score.phase"
              @click="submit(score.phase, 'accepted')"
            >
              <BadgeCheck :size="15" />认可证据
            </el-button>
            <el-button
              type="warning"
              plain
              :disabled="!candidates(score.phase).length"
              :loading="analyses.phaseSaving === score.phase"
              @click="submit(score.phase, 'excluded')"
            >
              <ShieldQuestion :size="15" />排除疑似原因
            </el-button>
            <el-button
              type="danger"
              plain
              :loading="analyses.phaseSaving === score.phase"
              @click="submit(score.phase, 'returned')"
            >
              <CornerUpLeft :size="15" />退回调查
            </el-button>
          </div>
        </template>
      </template>

      <details v-if="events(score.phase).length" class="phase-review-history">
        <summary>处理记录（{{ events(score.phase).length }}）</summary>
        <ul>
          <li v-for="(event, index) in events(score.phase)" :key="index">
            <el-tag :type="decisionTypes[event.decision]" size="small" effect="plain">{{ decisionLabels[event.decision] }}</el-tag>
            <span>{{ event.actor_name }}（{{ event.actor_role }}）</span>
            <time>{{ formatTime(event.created_at) }}</time>
            <p v-if="event.cause">原因：{{ event.cause }}</p>
            <p v-if="event.rationale">依据：{{ event.rationale }}</p>
          </li>
        </ul>
      </details>
    </article>
  </section>
</template>
