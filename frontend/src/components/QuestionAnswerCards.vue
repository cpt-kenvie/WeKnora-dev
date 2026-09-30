<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import QuestionCard from './QuestionCard.vue'
import { questionSnapshots } from '@/utils/questionSnapshots'

const props = defineProps<{ references: unknown; content?: string; sessionId?: string; messageId?: string }>()
const { t } = useI18n()
const questions = computed(() => questionSnapshots(props.references))
</script>

<template>
  <div class="question-answer-cards">
    <p>{{ t(content?.startsWith('找到以下原题：') ? 'questionBank.exactMatch' : 'questionBank.candidates') }}</p>
    <QuestionCard v-for="question in questions" :key="question.id" :question="question" :session-id="sessionId" :message-id="messageId" show-image />
  </div>
</template>

<style scoped>
.question-answer-cards { display: grid; gap: 16px; }
.question-answer-cards > p { margin: 0; line-height: 1.7; }
</style>
