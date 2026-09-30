<script setup lang="ts">
import { computed } from 'vue'
import QuestionCard from './QuestionCard.vue'
import { citedQuestionSnapshots } from '@/utils/questionSnapshots'

const props = defineProps<{ references: unknown; content?: string; sessionId?: string; messageId?: string }>()
const questions = computed(() => citedQuestionSnapshots(props.references, props.content || ''))
</script>

<template>
  <div v-if="questions.length" class="question-answer-cards">
    <QuestionCard v-for="question in questions" :key="question.id" :question="question" :session-id="sessionId" :message-id="messageId" show-image />
  </div>
</template>

<style scoped>
.question-answer-cards { display: grid; gap: 16px; margin-top: 16px; }
</style>
