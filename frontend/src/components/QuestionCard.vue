<script setup lang="ts">
import { computed, onBeforeUnmount, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { QuestionSnapshot } from '@/api/question-bank'
import { buildProtectedFileRequest, resolveProtectedFileAccess } from '@/utils/protectedFileAccess'

const props = defineProps<{ question: QuestionSnapshot; sessionId?: string; messageId?: string; showImage?: boolean; fullImage?: boolean; collapsible?: boolean }>()
const { t } = useI18n()
const imageUrl = ref('')
const failed = ref(false)
const expanded = ref(false)
const detailsExpanded = ref(false)
const detailsId = useId()
const showDetails = computed(() => !props.collapsible || detailsExpanded.value)
let pending: AbortController | undefined
let objectUrl = ''
const answerText = computed(() => {
  const { answer, question_type, options } = props.question
  if (question_type === 'fill_blank') return (answer.blanks || []).join('；')
  if (question_type === 'true_false' && typeof answer.truth === 'boolean') return t(answer.truth ? 'questionBank.true' : 'questionBank.false')
  return (answer.option_keys || []).map(key => {
    const option = options.find(item => item.key === key)
    return option ? `${key}：${option.text}` : key
  }).join('；')
})

async function loadImage() {
  pending?.abort()
  if (objectUrl) URL.revokeObjectURL(objectUrl)
  objectUrl = ''; imageUrl.value = ''; failed.value = false
  if (!props.showImage || !showDetails.value || !props.question.image_ref) return
  const controller = new AbortController()
  pending = controller
  const access = props.sessionId && props.messageId
    ? resolveProtectedFileAccess({ mode: 'message', sessionId: props.sessionId, messageId: props.messageId })
    : resolveProtectedFileAccess({ mode: 'knowledgeBase', kbId: props.question.knowledge_base_id })
  const request = buildProtectedFileRequest(props.question.image_ref, access)
  if (!request) {
    if (/^https?:\/\//i.test(props.question.image_ref)) imageUrl.value = props.question.image_ref
    else failed.value = true
    return
  }
  try {
    const response = await fetch(request.url, { headers: request.headers, signal: controller.signal })
    if (!response.ok) throw new Error(String(response.status))
    const blob = await response.blob()
    if (controller.signal.aborted) return
    objectUrl = URL.createObjectURL(blob)
    imageUrl.value = objectUrl
  } catch {
    if (!controller.signal.aborted) failed.value = true
  }
}
watch(() => [props.question.image_ref, props.question.knowledge_base_id, props.showImage, props.sessionId, props.messageId, showDetails.value], loadImage, { immediate: true })
onBeforeUnmount(() => { pending?.abort(); if (objectUrl) URL.revokeObjectURL(objectUrl) })
</script>

<template>
  <article class="question-card" :class="{ 'question-card--compact': collapsible }">
    <div class="question-card__heading">
      <t-tag theme="primary" variant="light">{{ t(`questionBank.types.${question.question_type}`) }}</t-tag>
      <span class="question-card__version">{{ t('questionBank.version', { version: question.revision }) }}</span>
      <slot name="actions" />
      <t-button v-if="collapsible" variant="text" size="small" :aria-expanded="showDetails" :aria-controls="detailsId" @click="detailsExpanded = !detailsExpanded">
        <template #icon><t-icon :name="showDetails ? 'chevron-up' : 'chevron-down'" /></template>
        {{ t(showDetails ? 'questionBank.collapse' : 'questionBank.expand') }}
      </t-button>
    </div>
    <p class="question-card__stem" :class="{ 'question-card__stem--summary': !showDetails }">{{ question.stem }}</p>
    <div v-if="!showDetails" class="question-card__summary-answer"><strong>{{ t('questionBank.correctAnswer') }}</strong><span>{{ answerText || t('questionBank.answerMissing') }}</span></div>
    <div v-show="showDetails" :id="detailsId">
      <ol v-if="question.options.length" class="question-card__options">
        <li v-for="option in question.options" :key="option.key" :class="{ correct: question.answer.option_keys?.includes(option.key) }">
          <strong>{{ option.key }}.</strong><span>{{ option.text }}</span>
          <t-icon v-if="question.answer.option_keys?.includes(option.key)" name="check-circle" :aria-label="t('questionBank.correctAnswer')" />
        </li>
      </ol>
      <div class="question-card__answer"><strong>{{ t('questionBank.correctAnswer') }}</strong><span>{{ answerText || t('questionBank.answerMissing') }}</span></div>
      <div v-if="showImage && showDetails" class="question-card__image">
        <button v-if="imageUrl" type="button" class="question-card__thumbnail" :class="{ 'question-card__thumbnail--full': fullImage }" @click="expanded = true" :aria-label="t('questionBank.viewOriginal')">
          <img :src="imageUrl" :alt="t('questionBank.originalImage')" loading="lazy" @error="imageUrl = ''; failed = true" />
          <span>{{ t('questionBank.viewOriginal') }}</span>
        </button>
        <t-button v-else-if="failed" variant="text" @click="loadImage">{{ t('questionBank.retryImage') }}</t-button>
        <t-loading v-else size="small" />
      </div>
    </div>
    <slot />
    <t-dialog v-model:visible="expanded" :header="t('questionBank.originalImage')" :footer="false" width="min(900px, 94vw)" attach="body">
      <img v-if="imageUrl" :src="imageUrl" class="question-card__original" :alt="t('questionBank.originalImage')" />
    </t-dialog>
  </article>
</template>

<style scoped lang="less">
.question-card { padding: 20px; border: 1px solid var(--td-component-border); border-radius: 12px; background: var(--td-bg-color-container); color: var(--td-text-color-primary); }
.question-card__heading { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.question-card--compact { padding: 12px 16px; }
.question-card--compact .question-card__stem { margin: 10px 0; }
.question-card__stem--summary { overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
.question-card__summary-answer { display: flex; align-items: baseline; gap: 10px; font-size: 13px; color: var(--td-text-color-secondary); }
.question-card__summary-answer strong { flex-shrink: 0; }
.question-card__summary-answer span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.question-card__version { color: var(--td-text-color-secondary); font-size: 12px; margin-right: auto; }
.question-card__stem { white-space: pre-wrap; overflow-wrap: anywhere; line-height: 1.8; margin: 18px 0; font-size: 15px; }
.question-card__options { list-style: none; padding: 0; margin: 0 0 16px; display: grid; gap: 8px; }
.question-card__options li { display: flex; gap: 10px; padding: 10px 12px; border-radius: 6px; line-height: 1.6; white-space: pre-wrap; }
.question-card__options li span { flex: 1; overflow-wrap: anywhere; }
.question-card__options li.correct { background: var(--td-success-color-light); color: var(--td-success-color); }
.question-card__answer { display: flex; gap: 12px; flex-wrap: wrap; padding-top: 14px; border-top: 1px solid var(--td-component-border); line-height: 1.7; }
.question-card__answer span { white-space: pre-wrap; overflow-wrap: anywhere; }
.question-card__image { margin-top: 16px; }
.question-card__thumbnail { display: flex; align-items: center; gap: 14px; padding: 8px; background: transparent; color: var(--td-brand-color); border: 0; border-radius: 8px; cursor: pointer; }
.question-card__thumbnail:focus-visible { outline: 2px solid var(--td-brand-color); }
.question-card__thumbnail img { width: 78px; height: 118px; object-fit: cover; object-position: top; border-radius: 4px; outline: 1px solid oklch(0 0 0 / 0.1); }
:global([theme-mode='dark']) .question-card__thumbnail img { outline-color: oklch(1 0 0 / 0.1); }
.question-card__original { display: block; max-width: 100%; height: auto; margin: auto; }
.question-card__thumbnail--full { display: block; width: 100%; }
.question-card__thumbnail--full img { width: 100%; height: auto; object-fit: contain; }
@media (max-width: 640px) { .question-card { padding: 14px; } }
</style>
