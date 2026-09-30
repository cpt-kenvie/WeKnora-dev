<script setup lang="ts">
import { computed, onBeforeUnmount, ref, toRaw, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin } from 'tdesign-vue-next'
import QuestionCard from '@/components/QuestionCard.vue'
import { deleteQuestion, listQuestions, retryQuestionIndex, updateQuestion, type Question, type QuestionType } from '@/api/question-bank'
import { uploadKnowledgeFile } from '@/api/knowledge-base'

const props = defineProps<{ kbId: string; canEdit: boolean; processing?: boolean }>()
const emit = defineEmits<{ 'show-sources': []; uploaded: [] }>()
const { t } = useI18n()
const types: QuestionType[] = ['single_choice', 'multiple_choice', 'true_false', 'fill_blank']
const questions = ref<Question[]>([])
const total = ref(0)
const processingSources = ref(0)
const failedSources = ref(0)
const page = ref(1)
const pageSize = 20
const keyword = ref('')
const questionType = ref<QuestionType | ''>('')
const reviewStatus = ref<Question['review_status'] | ''>('')
const loading = ref(false)
const error = ref('')
const uploadInput = ref<HTMLInputElement>()
const uploading = ref(false)
const uploadResults = ref<{ name: string; success: boolean; error?: string }[]>([])
const editor = ref<Question | null>(null)
const saving = ref(false)
const choiceKeys = ref<string[]>([])
const blanksText = ref('')
const truthValue = ref('')
let listVersion = 0
let poll: ReturnType<typeof setTimeout> | undefined
let alive = true

function errorText(value: unknown): string { return value instanceof Error ? value.message : t('questionBank.requestFailed') }
async function load(silent = false) {
  const version = ++listVersion
  if (!silent) loading.value = true
  error.value = ''
  try {
    const result = await listQuestions(props.kbId, { page: page.value, page_size: pageSize, keyword: keyword.value, question_type: questionType.value, review_status: reviewStatus.value })
    if (version !== listVersion || !alive) return
    questions.value = result.items
    total.value = result.total
    processingSources.value = result.processing_sources
    failedSources.value = result.failed_sources
  } catch (cause) {
    if (version === listVersion && alive) error.value = errorText(cause)
  } finally {
    if (version === listVersion && alive) loading.value = false
  }
}
function filter() { page.value = 1; void load() }
function schedulePoll() {
  if (poll) clearTimeout(poll)
  // 只在已知有识别或索引任务时轮询，离开页面即停止。
  if (!alive || (!processingSources.value && !questions.value.some(item => item.index_status === 'processing'))) return
  poll = setTimeout(async () => { await load(true); schedulePoll() }, 4000)
}
watch(() => props.kbId, () => { page.value = 1; editor.value = null; uploadResults.value = []; void load() }, { immediate: true })
watch([() => props.processing, questions], schedulePoll)
onBeforeUnmount(() => { alive = false; listVersion++; if (poll) clearTimeout(poll) })

async function upload(event: Event) {
  if (!(event.target instanceof HTMLInputElement) || !event.target.files) return
  const files = Array.from(event.target.files)
  event.target.value = ''
  uploading.value = true
  uploadResults.value = []
  const kbId = props.kbId
  for (const file of files) {
    if (!alive || props.kbId !== kbId) break
    if (!/\.(png|jpe?g|webp|bmp)$/i.test(file.name) || file.size > 30 * 1024 * 1024) {
      uploadResults.value.push({ name: file.name, success: false, error: t('questionBank.imageLimit') }); continue
    }
    try {
      await uploadKnowledgeFile(kbId, { file })
      uploadResults.value.push({ name: file.name, success: true })
    } catch (cause) { uploadResults.value.push({ name: file.name, success: false, error: errorText(cause) }) }
  }
  uploading.value = false
  if (alive && props.kbId === kbId) { emit('uploaded'); await load() }
}
function edit(question: Question) {
  editor.value = structuredClone(toRaw(question))
  choiceKeys.value = [...(question.answer.option_keys || [])]
  blanksText.value = (question.answer.blanks || []).join('\n')
  truthValue.value = typeof question.answer.truth === 'boolean' ? String(question.answer.truth) : ''
}
function changeType() {
  choiceKeys.value = []; blanksText.value = ''; truthValue.value = ''
  if (editor.value?.question_type === 'fill_blank') editor.value.options = []
}
function addOption() {
  if (!editor.value || editor.value.options.length >= 26) return
  const used = new Set(editor.value.options.map(option => option.key))
  const key = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'.split('').find(candidate => !used.has(candidate))
  if (key) editor.value.options.push({ key, text: '' })
}
const optionChoices = computed(() => (editor.value?.options || []).map(option => ({ label: `${option.key}：${option.text}`, value: option.key })))
async function save() {
  if (!editor.value) return
  saving.value = true
  const question = structuredClone(toRaw(editor.value))
  question.answer = question.question_type === 'fill_blank'
    ? { blanks: blanksText.value.split('\n').map(value => value.trim()) }
    : question.question_type === 'true_false'
      ? { option_keys: choiceKeys.value, truth: truthValue.value === '' ? null : truthValue.value === 'true' }
      : { option_keys: choiceKeys.value }
  if (question.question_type !== 'fill_blank') question.blank_count = 0
  try {
    const saved = await updateQuestion(props.kbId, question)
    editor.value = null
    if (saved.index_status === 'failed') MessagePlugin.warning(t('questionBank.savedIndexFailed'))
    else MessagePlugin.success(t('questionBank.saved'))
    await load()
  } catch (cause) { MessagePlugin.error(errorText(cause)) }
  finally { saving.value = false }
}
async function retry(question: Question) {
  try {
    const result = await retryQuestionIndex(props.kbId, question.id)
    if (result.index_status === 'failed') MessagePlugin.error(result.index_error)
    await load()
  } catch (cause) { MessagePlugin.error(errorText(cause)) }
}
async function remove(question: Question) {
  try { await deleteQuestion(props.kbId, question); await load() }
  catch (cause) { MessagePlugin.error(errorText(cause)) }
}
</script>

<template>
  <section class="question-bank">
    <div class="question-bank__toolbar">
      <div><h2>{{ t('questionBank.title') }}</h2><p>{{ t('questionBank.description') }}</p></div>
      <t-button variant="outline" @click="emit('show-sources')">{{ t('questionBank.sources') }}</t-button>
      <t-button v-if="canEdit" :loading="uploading" @click="uploadInput?.click()"><template #icon><t-icon name="upload" /></template>{{ t('questionBank.upload') }}</t-button>
      <input ref="uploadInput" type="file" accept=".png,.jpg,.jpeg,.webp,.bmp" multiple hidden @change="upload" />
    </div>
    <t-alert v-if="processingSources" theme="info" :message="t('questionBank.processing')" />
    <t-alert v-if="failedSources" theme="warning" :message="t('questionBank.sourceFailures', { count: failedSources })" />
    <div v-if="uploadResults.length" class="question-bank__uploads" aria-live="polite">
      <div v-for="(result, index) in uploadResults" :key="index"><t-icon :name="result.success ? 'check-circle' : 'error-circle'" /> {{ result.name }} — {{ result.success ? t('questionBank.queued') : result.error }}</div>
    </div>
    <div class="question-bank__filters">
      <t-input v-model="keyword" :placeholder="t('questionBank.search')" clearable @enter="filter" @clear="filter" />
      <t-select v-model="questionType" :placeholder="t('questionBank.allTypes')" clearable @change="filter"><t-option v-for="type in types" :key="type" :value="type" :label="t(`questionBank.types.${type}`)" /></t-select>
      <t-select v-model="reviewStatus" :placeholder="t('questionBank.allStatuses')" clearable @change="filter"><t-option value="ready" :label="t('questionBank.ready')" /><t-option value="needs_review" :label="t('questionBank.needsReview')" /></t-select>
      <t-button variant="outline" :loading="loading" @click="load()">{{ t('questionBank.refresh') }}</t-button>
    </div>
    <t-alert v-if="error" theme="error" :message="error" />
    <t-loading :loading="loading" class="question-bank__list">
      <div v-if="!questions.length && !loading" class="question-bank__empty"><t-icon name="file-search" size="36px" /><p>{{ t('questionBank.empty') }}</p></div>
      <QuestionCard v-for="question in questions" :key="question.id" :question="question" show-image collapsible>
        <template #actions>
          <t-tag :theme="question.review_status === 'ready' && question.is_enabled ? 'success' : 'warning'" variant="light">{{ t(question.review_status === 'needs_review' ? 'questionBank.needsReview' : question.is_enabled ? 'questionBank.ready' : 'questionBank.disabled') }}</t-tag>
          <t-button v-if="canEdit" size="small" variant="text" :disabled="question.index_status === 'processing'" @click="edit(question)">{{ t('questionBank.edit') }}</t-button>
          <t-popconfirm v-if="canEdit" :content="t('questionBank.deleteConfirm')" @confirm="remove(question)"><t-button size="small" theme="danger" variant="text" :disabled="question.index_status === 'processing'">{{ t('questionBank.delete') }}</t-button></t-popconfirm>
        </template>
        <t-alert v-if="question.issues.length" theme="warning" :message="question.issues.join('；')" class="question-bank__notice" />
        <div v-if="question.index_status !== 'ready'" class="question-bank__notice">
          <t-alert :theme="question.index_status === 'failed' ? 'error' : 'info'" :message="question.index_status === 'failed' ? question.index_error : t('questionBank.indexing')" />
          <t-button v-if="canEdit" variant="text" @click="retry(question)">{{ t('questionBank.retryIndex') }}</t-button>
        </div>
      </QuestionCard>
    </t-loading>
    <t-pagination v-if="total" v-model:current="page" :total="total" :page-size="pageSize" :show-page-size="false" @current-change="load()" />

    <t-dialog :visible="editor !== null" :header="t('questionBank.edit')" top="5vh" width="min(1180px, 96vw)" :confirm-loading="saving" :close-on-overlay-click="false" @confirm="save" @close="editor = null">
      <div v-if="editor" class="question-bank__editor">
        <QuestionCard :question="editor" show-image full-image />
        <t-form label-align="top" class="question-bank__form">
          <t-form-item :label="t('questionBank.questionType')"><t-select v-model="editor.question_type" @change="changeType"><t-option v-for="type in types" :key="type" :value="type" :label="t(`questionBank.types.${type}`)" /></t-select></t-form-item>
          <t-form-item :label="t('questionBank.stem')"><t-textarea v-model="editor.stem" :autosize="{ minRows: 3, maxRows: 9 }" /></t-form-item>
          <template v-if="editor.question_type !== 'fill_blank'">
            <t-form-item :label="t('questionBank.options')"><div class="question-bank__option-editor"><div v-for="(option, index) in editor.options" :key="index"><t-input v-model="option.key" class="question-bank__key" :maxlength="4" /><t-input v-model="option.text" /><t-button variant="text" theme="danger" @click="editor.options.splice(index, 1)" :aria-label="t('questionBank.deleteOption')"><t-icon name="close" /></t-button></div><t-button variant="outline" @click="addOption">{{ t('questionBank.addOption') }}</t-button></div></t-form-item>
            <t-form-item :label="t('questionBank.correctOptions')"><t-select v-model="choiceKeys" multiple :options="optionChoices" /></t-form-item>
          </template>
          <t-form-item v-if="editor.question_type === 'true_false'" :label="t('questionBank.judgment')"><t-select v-model="truthValue"><t-option value="true" :label="t('questionBank.true')" /><t-option value="false" :label="t('questionBank.false')" /></t-select></t-form-item>
          <template v-if="editor.question_type === 'fill_blank'"><t-form-item :label="t('questionBank.blankCount')"><t-input-number v-model="editor.blank_count" :min="1" :max="100" /></t-form-item><t-form-item :label="t('questionBank.blankAnswers')"><t-textarea v-model="blanksText" :autosize="{ minRows: 2, maxRows: 8 }" /></t-form-item></template>
          <t-form-item :label="t('questionBank.answerRaw')"><t-textarea v-model="editor.answer_raw" /></t-form-item>
          <t-form-item :label="t('questionBank.review')"><t-select v-model="editor.review_status"><t-option value="ready" :label="t('questionBank.reviewed')" /><t-option value="needs_review" :label="t('questionBank.needsReview')" /></t-select></t-form-item>
          <t-form-item :label="t('questionBank.enabled')"><t-switch v-model="editor.is_enabled" :disabled="editor.review_status !== 'ready'" /></t-form-item>
        </t-form>
      </div>
    </t-dialog>
  </section>
</template>

<style scoped lang="less">
.question-bank { padding: 24px 32px; overflow: auto; flex: 1; min-height: 0; }
.question-bank__toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; margin-bottom: 20px; }
.question-bank__toolbar > div { flex: 1; min-width: 240px; }
.question-bank__toolbar h2 { font-size: 20px; margin: 0 0 6px; }
.question-bank__toolbar p { color: var(--td-text-color-secondary); margin: 0; line-height: 1.6; }
.question-bank__filters { display: flex; gap: 12px; margin: 20px 0; }
.question-bank__filters > .t-input__wrap { flex: 2; }
.question-bank__filters > .t-select__wrap { flex: 1; min-width: 120px; }
.question-bank__list { display: grid; gap: 16px; margin-bottom: 20px; }
.question-bank__uploads { margin: 14px 0; max-height: 140px; overflow: auto; font-size: 13px; line-height: 1.9; }
.question-bank__empty { text-align: center; padding: 64px 16px; color: var(--td-text-color-secondary); }
.question-bank__notice { margin-top: 12px; }
.question-bank__editor { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; max-height: 65vh; overflow: auto; }
.question-bank__editor > .question-card { align-self: start; }
.question-bank__form { padding: 0 12px 0 0; }
.question-bank__option-editor { width: 100%; display: grid; gap: 8px; }
.question-bank__option-editor > div { display: flex; gap: 8px; }
.question-bank__key { max-width: 60px; }
@media (max-width: 760px) { .question-bank { padding: 16px; } .question-bank__filters { flex-wrap: wrap; } .question-bank__editor { grid-template-columns: 1fr; } }
</style>
