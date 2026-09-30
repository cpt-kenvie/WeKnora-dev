import type { QuestionPaperOptions, QuestionPaperSection, QuestionType } from '@/api/question-bank'

export const QUESTION_PAPER_TYPES: QuestionType[] = ['single_choice', 'multiple_choice', 'true_false', 'fill_blank']

export type QuestionPaperIssue =
  | { kind: 'count' }
  | { kind: 'limit'; max: number }
  | { kind: 'shortage'; questionType: QuestionType; requested: number; available: number }

export function validateQuestionPaper(sections: QuestionPaperSection[], options: QuestionPaperOptions): QuestionPaperIssue | null {
  if (!sections.length || sections.some(section => !Number.isInteger(section.count) || section.count < 1)) return { kind: 'count' }
  const total = sections.reduce((sum, section) => sum + section.count, 0)
  if (total > options.max_questions) return { kind: 'limit', max: options.max_questions }
  const requested = new Map<QuestionType, number>()
  for (const section of sections) requested.set(section.question_type, (requested.get(section.question_type) || 0) + section.count)
  // 相同题型可以分多行配置，可用数量必须按所有行的合计校验。
  for (const [questionType, count] of requested) {
    if (count > options.counts[questionType]) return { kind: 'shortage', questionType, requested: count, available: options.counts[questionType] }
  }
  return null
}

export function questionPaperFilename(name: string, includeAnswers: boolean): string {
  const safeName = name.replace(/[<>:"/\\|?*\u0000-\u001f]/g, '_').trim().replace(/[. ]+$/, '').slice(0, 100) || '题库'
  return `${safeName}-试卷${includeAnswers ? '-含答案' : ''}.docx`
}
