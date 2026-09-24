<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CheckCircle2, FileSearch, Play, RefreshCw, RotateCcw, SearchCheck } from 'lucide-vue-next'
import AnalysisExplanationDrawer from '../components/common/AnalysisExplanationDrawer.vue'
import AppShell from '../components/common/AppShell.vue'
import DeviationBadge from '../components/common/DeviationBadge.vue'
import KineticsChart from '../components/common/KineticsChart.vue'
import PageHeader from '../components/common/PageHeader.vue'
import PhaseBadge from '../components/common/PhaseBadge.vue'
import PhaseReviewPanel from '../components/common/PhaseReviewPanel.vue'
import StateBadge from '../components/common/StateBadge.vue'
import { useAnalysisRun } from '../hooks/useAnalysisRun'
import { useAuth } from '../hooks/useAuth'
import { useAnalysisStore } from '../stores/deviation-analysis'
import { useSeriesStore } from '../stores/sensor-series'
import { ApiError } from '../api/client'
import type { AnalysisState } from '../types/deviation-analysis'

const analyses = useAnalysisStore()
const series = useSeriesStore()
const { auth, canRunAnalysis, canReview, canConfirm } = useAuth()
const runner = useAnalysisRun()
const drawer = ref(false)
const reviewComment = ref('')
const confirmDialog = ref(false)
const canSelfConfirm = computed(() => analyses.selected?.initiated_by !== auth.user?.id)
const selected = computed(() => analyses.selected)
const pendingPhases = computed(() => selected.value?.pending_review_phases ?? [])
const reviewableStates = ['completed', 'reviewed', 'investigating']
const canReviewNow = computed(() =>
  Boolean(selected.value && reviewableStates.includes(selected.value.analysis_state)),
)

async function selectAnalysis(id: number) {
  await analyses.hydrate(id)
}
async function run() {
  try { await runner.run(); ElMessage.success('分析已完成或返回现有幂等结果') }
  catch (error) { ElMessage.error(error instanceof Error ? error.message : '分析运行失败') }
}
async function transition(state: AnalysisState) {
  try {
    await analyses.transition(state, reviewComment.value)
    reviewComment.value = ''
    ElMessage.success('分析状态已更新')
  } catch (error) {
    if (error instanceof ApiError) ElMessage.error(error.message)
    else ElMessage.error('状态更新失败')
  }
}
async function confirmResult() {
  if (pendingPhases.value.length) {
    ElMessage.error(`确认被阻塞，请先处理高风险阶段：${pendingPhases.value.join('、')}`)
    return
  }
  confirmDialog.value = true
}
async function performConfirm() {
  confirmDialog.value = false
  await transition('confirmed')
}
async function replay() {
  try { await analyses.replay(); ElMessage.success('冻结输入重放一致') }
  catch (error) { ElMessage.error(error instanceof Error ? error.message : '重放失败') }
}
onMounted(async () => {
  await Promise.all([series.load(), analyses.load()])
  runner.seriesId.value = runner.readySeries.value[0]?.id
})
</script>

<template>
  <AppShell>
    <div class="page-wrap">
      <PageHeader eyebrow="PHASE-CONSTRAINED DTW" title="偏差分析" description="对齐实测与配方参考曲线，逐阶段呈现持续时间、斜率、峰值时间与曲线距离证据。">
        <el-tooltip content="刷新数据"><el-button circle aria-label="刷新" @click="analyses.load()"><RefreshCw :size="17" /></el-button></el-tooltip>
        <el-button v-if="selected" @click="drawer = true"><FileSearch :size="16" />查看解释</el-button>
      </PageHeader>
      <section v-if="canRunAnalysis" class="run-band">
        <div><Play :size="20" /><span><strong>运行冻结分析</strong><small>相同输入哈希与算法版本返回同一历史结果</small></span></div>
        <el-select v-model="runner.seriesId.value" placeholder="选择就绪时序">
          <el-option v-for="item in runner.readySeries.value" :key="item.id" :label="`${item.run_code} · ${item.recipe?.recipe_code ?? ''}`" :value="item.id" />
        </el-select>
        <el-button type="primary" :loading="runner.running.value" :disabled="!runner.seriesId.value" @click="run"><Play :size="16" />运行分析</el-button>
      </section>
      <el-alert v-if="analyses.error" :title="analyses.error" type="error" :closable="false" show-icon />
      <div class="analysis-workspace">
        <section class="analysis-list">
          <div class="section-heading"><div><h2>历史结果</h2><p>{{ analyses.items.length }} 条不可覆盖记录</p></div></div>
          <el-skeleton v-if="analyses.loading" :rows="6" animated />
          <button v-for="item in analyses.items" v-else :key="item.id" class="analysis-row" :class="{ selected: selected?.id === item.id }" @click="selectAnalysis(item.id)">
            <span>#{{ item.id }}</span>
            <span><strong>{{ item.sensor_series?.run_code ?? 'Series ' + item.sensor_series_id }}</strong><small>{{ new Date(item.analyzed_at).toLocaleString() }}</small></span>
            <DeviationBadge :level="item.deviation_level" />
            <StateBadge :state="item.analysis_state" />
          </button>
          <div v-if="!analyses.loading && !analyses.items.length" class="empty-inline">暂无分析结果</div>
        </section>
        <section class="analysis-detail">
          <template v-if="selected">
            <div class="analysis-summary">
              <div><p class="eyebrow">DEVIATION RESULT</p><h2>{{ selected.sensor_series?.run_code }}</h2><small>{{ selected.algorithm_version }}</small></div>
              <DeviationBadge :level="selected.deviation_level" />
              <StateBadge :state="selected.analysis_state" />
              <el-tooltip v-if="canRunAnalysis" content="重放冻结输入"><el-button circle aria-label="重放分析" @click="replay"><RotateCcw :size="17" /></el-button></el-tooltip>
            </div>
            <KineticsChart :aligned="selected.aligned_curve_json" :height="380" />
            <div class="phase-score-grid">
              <article v-for="score in selected.phase_scores_json" :key="score.phase" :class="{ 'phase-high-risk': score.weighted_deviation >= 0.2 - 1e-9 }">
                <PhaseBadge :phase="score.phase" />
                <strong :class="{ 'score-danger': score.weighted_deviation >= 0.2 - 1e-9 }">{{ (score.weighted_deviation * 100).toFixed(1) }}%</strong>
                <dl>
                  <div><dt>曲线</dt><dd>{{ score.curve_distance.toFixed(3) }}</dd></div>
                  <div><dt>斜率</dt><dd>{{ score.slope_deviation.toFixed(3) }}</dd></div>
                  <div><dt>峰值</dt><dd>{{ score.peak_time_deviation.toFixed(3) }}</dd></div>
                </dl>
                <small v-if="score.weighted_deviation >= 0.2 - 1e-9" class="phase-high-risk-flag">高风险 · 须逐项复核</small>
              </article>
            </div>

            <PhaseReviewPanel
              :analysis="selected"
              :current-user-id="auth.user?.id"
              :can-review="canReviewNow && canReview"
            />

            <section v-if="canReview" class="review-band">
              <div><SearchCheck :size="19" /><span><strong>总体复核</strong><small>确认动作要求与发起人分离；高风险阶段未清前确认会被拦截</small></span></div>
              <el-input v-model="reviewComment" placeholder="总复核意见（可选，逐阶段依据请在上方各阶段卡片填写）" />
              <el-alert
                v-if="pendingPhases.length && selected.analysis_state === 'reviewed'"
                type="error"
                :closable="false"
                show-icon
                class="confirm-blocker"
                title="确认被阻塞，请先处理以下待处理项"
                :description="`高风险阶段未结论或处于退回调查：${pendingPhases.join('、')}`"
              />
              <div class="review-actions">
                <el-button v-if="selected.analysis_state === 'completed'" @click="transition('reviewed')">标记已复核</el-button>
                <el-button v-if="selected.analysis_state === 'reviewed'" @click="transition('investigating')">整体退回调查</el-button>
                <el-button
                  v-if="selected.analysis_state === 'investigating'"
                  type="success"
                  plain
                  @click="transition('reviewed')"
                >调查完成，回到已复核</el-button>
                <el-button
                  v-if="canConfirm && canSelfConfirm && selected.analysis_state === 'reviewed'"
                  type="primary"
                  :disabled="Boolean(pendingPhases.length)"
                  @click="confirmResult"
                ><CheckCircle2 :size="16" />确认结果</el-button>
                <el-button v-if="['completed','reviewed','investigating','confirmed'].includes(selected.analysis_state)" text @click="transition('voided')">作废</el-button>              </div>
            </section>
          </template>
          <div v-else class="empty-state"><h2>选择历史结果</h2><p>对齐曲线和阶段证据将在此显示。</p></div>
        </section>
      </div>
    </div>
    <AnalysisExplanationDrawer v-model="drawer" :analysis="selected" />
    <el-dialog v-model="confirmDialog" title="确认偏差分析结果" width="min(460px, 92vw)">
      <p>所有加权偏差 ≥ 20% 的阶段均已逐项结论，且与发起人（{{ selected?.initiated_by_name }}）身份分离。</p>
      <p class="muted">确认后结果进入最终归档；仍可作废但不可再修改阶段结论。</p>
      <template #footer>
        <el-button @click="confirmDialog = false">取消</el-button>
        <el-button type="primary" @click="performConfirm"><CheckCircle2 :size="15" />确认</el-button>
      </template>
    </el-dialog>
  </AppShell>
</template>
