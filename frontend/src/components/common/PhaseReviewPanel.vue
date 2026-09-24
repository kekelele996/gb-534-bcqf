<script setup lang="ts">
import { computed, reactive } from 'vue'
import { CheckCircle2, CircleSlash, CornerUpLeft, ShieldAlert } from 'lucide-vue-next'
import type { DeviationAnalysis, PhaseReviewDecision, PhaseReviewDecisionRequest } from '../../types/deviation-analysis'
import type { SessionUser } from '../../types/auth'
import PhaseBadge from './PhaseBadge.vue'

const props = defineProps<{ analysis: DeviationAnalysis; user: SessionUser | null; editable: boolean }>()
const emit = defineEmits<{ submit: [request: PhaseReviewDecisionRequest] }>()

const HIGH_RISK = 0.2
const decisionLabels: Record<PhaseReviewDecision, string> = {
  accepted: '认可证据',
  excluded: '排除疑似原因',
  returned: '退回调查',
}

const highRiskScores = computed(() =>
  props.analysis.phase_scores_json.filter((score) => score.weighted_deviation >= HIGH_RISK),
)
const reviewByPhase = computed(() => {
  const map: Record<string, (typeof props.analysis.phase_reviews)[number]> = {}
  for (const review of props.analysis.phase_reviews) map[review.phase] = review
  return map
})
function causesForPhase(phase: string): string[] {
  return props.analysis.suspected_causes_json.filter((cause) => cause.startsWith(`${phase} `))
}
const drafts = reactive<Record<string, { decision: PhaseReviewDecision; basis: string; excluded_cause: string }>>({})
function draft(phase: string) {
  if (!drafts[phase]) drafts[phase] = { decision: 'accepted', basis: '', excluded_cause: '' }
  return drafts[phase]
}
function isBlocked(phase: string) {
  return props.analysis.pending_phase_reviews.some((item) => item.phase === phase)
}
function submit(phase: string) {
  const form = draft(phase)
  emit('submit', {
    phase, decision: form.decision, basis: form.basis.trim(),
    excluded_cause: form.decision === 'excluded' ? form.excluded_cause : undefined,
  })
}
</script>

<template>
  <section class="phase-review-panel">
    <div class="phase-review-heading">
      <ShieldAlert :size="18" />
      <span>
        <strong>高风险阶段逐项复核</strong>
        <small>加权偏差达到 20% 及以上的阶段必须逐个给出结论，退回或排除都要填写依据，排除还需选择具体原因</small>
      </span>
    </div>
    <el-alert
      v-if="analysis.pending_phase_reviews.length"
      type="warning" :closable="false" show-icon class="phase-block-alert"
      :title="`确认被挡住：还有 ${analysis.pending_phase_reviews.length} 个高风险阶段未给出结论或处于退回调查状态`"
    >
      <ul class="phase-block-list">
        <li v-for="item in analysis.pending_phase_reviews" :key="item.phase">
          <PhaseBadge :phase="item.phase" />
          <span>{{ item.reason }}</span>
        </li>
      </ul>
    </el-alert>
    <p v-else-if="highRiskScores.length" class="phase-clear-note">
      <CheckCircle2 :size="15" />所有高风险阶段均已处理，可以确认结果。
    </p>
    <div v-if="!highRiskScores.length" class="phase-review-empty">本次分析没有加权偏差达到 20% 的阶段。</div>
    <article v-for="score in highRiskScores" :key="score.phase" class="phase-review-card" :class="{ blocked: isBlocked(score.phase) }">
      <header>
        <PhaseBadge :phase="score.phase" />
        <strong :class="score.weighted_deviation >= 0.4 ? 'risk-major' : 'risk-watch'">
          {{ (score.weighted_deviation * 100).toFixed(1) }}%
        </strong>
        <span v-if="isBlocked(score.phase)" class="phase-status pending">待处理</span>
        <span v-else-if="reviewByPhase[score.phase]" class="phase-status done">
          {{ decisionLabels[reviewByPhase[score.phase].decision] }}
        </span>
      </header>
      <dl class="phase-review-metrics">
        <div><dt>曲线距离</dt><dd>{{ score.curve_distance.toFixed(3) }}</dd></div>
        <div><dt>斜率偏差</dt><dd>{{ score.slope_deviation.toFixed(3) }}</dd></div>
        <div><dt>峰值偏差</dt><dd>{{ score.peak_time_deviation.toFixed(3) }}</dd></div>
      </dl>
      <template v-if="reviewByPhase[score.phase]">
        <div class="phase-conclusion">
          <p v-if="reviewByPhase[score.phase].decision === 'excluded'" class="conclusion-cause">
            <CircleSlash :size="14" />已排除：{{ reviewByPhase[score.phase].excluded_cause }}
          </p>
          <p class="conclusion-basis">依据：{{ reviewByPhase[score.phase].basis }}</p>
          <p class="conclusion-meta">
            {{ reviewByPhase[score.phase].reviewed_by_name }} · {{ new Date(reviewByPhase[score.phase].reviewed_at).toLocaleString() }}
            更新于 {{ new Date(reviewByPhase[score.phase].updated_at).toLocaleString() }}
          </p>
        </div>
        <p v-if="editable && reviewByPhase[score.phase].reviewed_by !== user?.id" class="phase-ownership">
          该阶段已由 {{ reviewByPhase[score.phase].reviewed_by_name }} 接收，后续提交需由其本人处理。
        </p>
      </template>
      <template v-if="editable && (!reviewByPhase[score.phase] || reviewByPhase[score.phase].reviewed_by === user?.id)">
        <div class="phase-decision-form">
          <div class="phase-decision-choices">
            <button
              v-for="option in (['accepted', 'excluded', 'returned'] as PhaseReviewDecision[])"
              :key="option" type="button" class="decision-chip"
              :class="{ active: draft(score.phase).decision === option, danger: option === 'returned' }"
              @click="draft(score.phase).decision = option"
            >
              <component :is="option === 'accepted' ? CheckCircle2 : option === 'returned' ? CornerUpLeft : CircleSlash" :size="14" />
              {{ decisionLabels[option] }}
            </button>
          </div>
          <el-select
            v-if="draft(score.phase).decision === 'excluded'"
            v-model="draft(score.phase).excluded_cause"
            placeholder="选择要排除的具体疑似原因" class="cause-select"
          >
            <el-option v-for="cause in causesForPhase(score.phase)" :key="cause" :label="cause" :value="cause" />
          </el-select>
          <p v-if="draft(score.phase).decision === 'excluded' && !causesForPhase(score.phase).length" class="cause-unavailable">
            该阶段没有命中疑似原因规则，无法执行“排除疑似原因”，请选择认可证据或退回调查。
          </p>
          <el-input
            v-model="draft(score.phase).basis" type="textarea" :rows="2"
            :placeholder="draft(score.phase).decision === 'returned' ? '退回依据（必填）：需要补充哪些调查' : '处理依据（必填）'"
          />
          <div class="phase-submit-row">
            <el-button
              type="primary" size="small"
              :disabled="!draft(score.phase).basis.trim() ||
                (draft(score.phase).decision === 'excluded' && !draft(score.phase).excluded_cause)"
              @click="submit(score.phase)"
            >提交结论</el-button>
          </div>
        </div>
      </template>
    </article>
  </section>
</template>
