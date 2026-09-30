import type { QuestionSnapshot } from '../api/question-bank'

function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function strings(value: unknown): value is string[] {
  return Array.isArray(value) && value.every(item => typeof item === 'string')
}

// 历史消息及工具结果属于外部输入，确认完整协议后再交给题目组件。
export function isQuestionSnapshot(value: unknown): value is QuestionSnapshot {
  if (!record(value) || !record(value.answer)) return false
  const answer = value.answer
  return typeof value.id === 'string' && value.id !== ''
    && typeof value.revision === 'number' && value.revision > 0
    && typeof value.knowledge_base_id === 'string' && typeof value.knowledge_id === 'string'
    && typeof value.image_ref === 'string' && typeof value.stem === 'string'
    && typeof value.answer_raw === 'string' && typeof value.blank_count === 'number'
    && ['single_choice', 'multiple_choice', 'true_false', 'fill_blank'].includes(String(value.question_type))
    && Array.isArray(value.options) && value.options.every(option => record(option) && typeof option.key === 'string' && typeof option.text === 'string')
    && (answer.option_keys === undefined || strings(answer.option_keys))
    && (answer.blanks === undefined || strings(answer.blanks))
    && (answer.truth === undefined || answer.truth === null || typeof answer.truth === 'boolean')
}

export function questionSnapshots(references: unknown): QuestionSnapshot[] {
  if (!Array.isArray(references)) return []
  const found = new Map<string, QuestionSnapshot>()
  for (const reference of references) {
    if (record(reference) && isQuestionSnapshot(reference.question)) found.set(reference.question.id, reference.question)
  }
  return [...found.values()]
}
