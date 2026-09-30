<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import { exportQuestionPaper, getQuestionPaperOptions, type QuestionPaperOptions, type QuestionType } from '@/api/question-bank'
import { QUESTION_PAPER_TYPES, questionPaperFilename, validateQuestionPaper } from '@/utils/questionPaper'

const props = defineProps<{ visible: boolean; kbId: string; kbName: string }>()
const emit = defineEmits<{ 'update:visible': [value: boolean] }>()
const { t } = useI18n()
type PaperRow = { id: number; questionType: QuestionType; count: number | string }
const rows = ref<PaperRow[]>([])
const includeAnswers = ref(false)
const options = ref<QuestionPaperOptions | null>(null)
const loading = ref(false)
const exporting = ref(false)
const error = ref('')
let rowId = 0
let pending: AbortController | undefined
let downloadUrl = ''
let releaseTimer: ReturnType<typeof setTimeout> | undefined

const sections = computed(() => rows.value.map(row => ({ question_type: row.questionType, count: Number(row.count) })))
const total = computed(() => sections.value.reduce((sum, section) => sum + (Number.isInteger(section.count) && section.count > 0 ? section.count : 0), 0))
const validation = computed(() => {
  if (!options.value) return ''
  const issue = validateQuestionPaper(sections.value, options.value)
  if (!issue) return ''
  if (issue.kind === 'count') return t('questionBank.paper.invalidCount')
  if (issue.kind === 'limit') return t('questionBank.paper.limit', { count: issue.max })
  return t('questionBank.paper.shortage', { type: t(`questionBank.types.${issue.questionType}`), requested: issue.requested, available: issue.available })
})
const canExport = computed(() => !!options.value && !loading.value && !exporting.value && !validation.value)

function errorText(cause: unknown): string {
  if (typeof cause === 'object' && cause !== null && 'message' in cause && typeof cause.message === 'string') return cause.message
  return t('questionBank.requestFailed')
}

function addRow() {
  const questionType = QUESTION_PAPER_TYPES.find(type => !rows.value.some(row => row.questionType === type)) || 'single_choice'
  rows.value.push({ id: ++rowId, questionType, count: 10 })
}

async function loadOptions() {
  pending?.abort()
  const controller = new AbortController()
  pending = controller
  loading.value = true
  error.value = ''
  try {
    const result = await getQuestionPaperOptions(props.kbId, controller.signal)
    if (!controller.signal.aborted) options.value = result
  } catch (cause) {
    if (!controller.signal.aborted) error.value = errorText(cause)
  } finally {
    if (!controller.signal.aborted) loading.value = false
  }
}

function releaseDownload() {
  if (releaseTimer) clearTimeout(releaseTimer)
  if (downloadUrl) URL.revokeObjectURL(downloadUrl)
  downloadUrl = ''
}

async function exportPaper() {
  if (!canExport.value) return
  const controller = new AbortController()
  pending = controller
  exporting.value = true
  error.value = ''
  const request = { sections: sections.value, include_answers: includeAnswers.value }
  const filename = questionPaperFilename(props.kbName, request.include_answers)
  try {
    const file = await exportQuestionPaper(props.kbId, request, controller.signal)
    if (controller.signal.aborted) return
    releaseDownload()
    downloadUrl = URL.createObjectURL(file)
    const link = document.createElement('a')
    link.href = downloadUrl
    link.download = filename
    link.style.display = 'none'
    document.body.appendChild(link)
    link.click()
    link.remove()
    // 下载启动后再释放 URL，避免浏览器尚未读取文件时被回收。
    releaseTimer = setTimeout(releaseDownload, 1000)
    MessagePlugin.success(t('questionBank.paper.exported'))
    emit('update:visible', false)
  } catch (cause) {
    if (!controller.signal.aborted) {
      error.value = errorText(cause)
      // 核对状态可能在弹窗打开后变化；保留配置，让用户调整数量后重新出卷。
      try {
        const result = await getQuestionPaperOptions(props.kbId, controller.signal)
        if (!controller.signal.aborted) options.value = result
      } catch { /* 保留本次出卷错误，数量校验仍由服务端执行。 */ }
    }
  } finally {
    if (!controller.signal.aborted) exporting.value = false
  }
}

watch(() => [props.visible, props.kbId], () => {
  pending?.abort()
  loading.value = false
  exporting.value = false
  options.value = null
  error.value = ''
  if (!props.visible) return
  rows.value = []
  addRow()
  includeAnswers.value = false
  void loadOptions()
}, { immediate: true })
onBeforeUnmount(() => { pending?.abort(); releaseDownload() })
</script>

<template>
  <t-dialog :visible="visible" :header="t('questionBank.paper.title')" width="min(640px, 94vw)"
    :close-on-overlay-click="false" :close-on-esc-keydown="!exporting" :close-btn="!exporting"
    @close="emit('update:visible', false)">
    <div class="question-paper">
      <p class="question-paper__hint">{{ t('questionBank.paper.description') }}</p>
      <t-loading :loading="loading">
        <div class="question-paper__rows">
          <div v-for="(row, index) in rows" :key="row.id" class="question-paper__row">
            <span class="question-paper__number">{{ index + 1 }}.</span>
            <t-input-number v-model="row.count" :min="1" :max="options?.max_questions" :decimal-places="0"
              :disabled="loading || exporting" :aria-label="t('questionBank.paper.countLabel', { index: index + 1 })" />
            <span>{{ t('questionBank.paper.unit') }}</span>
            <div class="question-paper__type">
              <t-select v-model="row.questionType" :disabled="loading || exporting" :aria-label="t('questionBank.paper.typeLabel', { index: index + 1 })">
                <t-option v-for="type in QUESTION_PAPER_TYPES" :key="type" :value="type" :label="t(`questionBank.types.${type}`)" />
              </t-select>
              <span v-if="options" class="question-paper__available">{{ t('questionBank.paper.available', { count: options.counts[row.questionType] }) }}</span>
            </div>
            <t-button variant="text" shape="square" :disabled="rows.length === 1 || exporting" :aria-label="t('questionBank.paper.removeRow', { index: index + 1 })" @click="rows.splice(index, 1)">
              <t-icon name="minus-circle" />
            </t-button>
          </div>
        </div>
      </t-loading>
      <t-button variant="outline" :disabled="loading || exporting || rows.length >= (options?.max_questions || 1)" @click="addRow">
        <template #icon><t-icon name="add" /></template>{{ t('questionBank.paper.addRow') }}
      </t-button>
      <div class="question-paper__answers">
        <div><label for="question-paper-answers">{{ t('questionBank.paper.includeAnswers') }}</label><p>{{ t('questionBank.paper.answersHint') }}</p></div>
        <t-switch id="question-paper-answers" v-model="includeAnswers" :disabled="exporting" :aria-label="t('questionBank.paper.includeAnswers')" />
      </div>
      <p class="question-paper__total">{{ t('questionBank.paper.total', { count: total }) }}</p>
      <t-alert v-if="validation && !loading" theme="warning" :message="validation" />
      <t-alert v-if="error" theme="error" :message="error" />
      <t-button v-if="error && !options" variant="text" :loading="loading" @click="loadOptions">{{ t('questionBank.refresh') }}</t-button>
    </div>
    <template #footer>
      <t-button variant="outline" :disabled="exporting" @click="emit('update:visible', false)">{{ t('common.cancel') }}</t-button>
      <t-button :loading="exporting" :disabled="!canExport" @click="exportPaper">{{ t('questionBank.paper.export') }}</t-button>
    </template>
  </t-dialog>
</template>

<style scoped lang="less">
.question-paper { display: flex; flex-direction: column; gap: 16px; }
.question-paper__hint { margin: 0; line-height: 1.6; color: var(--td-text-color-secondary); }
.question-paper__rows { display: grid; gap: 16px; max-height: 36vh; overflow: auto; padding: 4px 0; }
.question-paper__row { display: flex; align-items: flex-start; gap: 10px; }
.question-paper__number { min-width: 22px; text-align: right; }
.question-paper__row > span { line-height: 32px; }
.question-paper__row :deep(.t-input-number) { width: 120px; flex-shrink: 0; }
.question-paper__type { flex: 1; min-width: 0; }
.question-paper__available { display: block; margin-top: 4px; font-size: 12px; color: var(--td-text-color-secondary); }
.question-paper > .t-button { align-self: flex-start; }
.question-paper__answers { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding-top: 16px; border-top: 1px solid var(--td-component-stroke); }
.question-paper__answers p { margin: 6px 0 0; font-size: 12px; line-height: 1.6; color: var(--td-text-color-secondary); }
.question-paper__total { margin: 0; font-weight: 500; }
@media (max-width: 520px) {
  .question-paper__row { gap: 6px; }
  .question-paper__row :deep(.t-input-number) { width: 88px; }
  .question-paper__number { min-width: 16px; }
}
</style>
