import { del, get, post, put } from '@/utils/request'

export type QuestionType = 'single_choice' | 'multiple_choice' | 'true_false' | 'fill_blank'
export interface QuestionOption { key: string; text: string }
export interface QuestionAnswer { option_keys?: string[]; truth?: boolean | null; blanks?: string[] }
export interface QuestionContent {
  question_type: QuestionType
  stem: string
  options: QuestionOption[]
  answer: QuestionAnswer
  answer_raw: string
  blank_count: number
}
export interface QuestionSnapshot extends QuestionContent {
  id: string
  revision: number
  knowledge_base_id: string
  knowledge_id: string
  image_ref: string
}
export interface Question extends QuestionSnapshot {
  source_index: number
  review_status: 'ready' | 'needs_review'
  issues: string[]
  is_enabled: boolean
  index_status: 'ready' | 'processing' | 'failed'
  index_error: string
  manually_edited: boolean
}
export interface QuestionFilter {
  page: number
  page_size: number
  keyword?: string
  question_type?: QuestionType | ''
  review_status?: Question['review_status'] | ''
}
export interface QuestionPaperSection { question_type: QuestionType; count: number }
export interface QuestionPaperOptions { counts: Record<QuestionType, number>; max_questions: number }
export interface QuestionPaperRequest { sections: QuestionPaperSection[]; include_answers: boolean }
interface Result<T> { success: boolean; data: T }

export async function getQuestionPaperOptions(kbId: string, signal?: AbortSignal) {
  return (await get<Result<QuestionPaperOptions>>(`/api/v1/knowledge-bases/${kbId}/questions/paper-options`, { signal })).data
}
export async function exportQuestionPaper(kbId: string, request: QuestionPaperRequest, signal?: AbortSignal): Promise<Blob> {
  return post<Blob>(`/api/v1/knowledge-bases/${kbId}/questions/paper`, request, { responseType: 'blob', signal })
}

export async function listQuestions(kbId: string, params: QuestionFilter) {
  return (await get<Result<{ items: Question[]; total: number; processing_sources: number; failed_sources: number }>>(`/api/v1/knowledge-bases/${kbId}/questions`, { params })).data
}
export async function updateQuestion(kbId: string, question: Question) {
  return (await put<Result<Question>>(`/api/v1/knowledge-bases/${kbId}/questions/${question.id}`, question)).data
}
export async function retryQuestionIndex(kbId: string, id: string) {
  return (await post<Result<Question>>(`/api/v1/knowledge-bases/${kbId}/questions/${id}/reindex`)).data
}
export async function deleteQuestion(kbId: string, question: Question) {
  return del(`/api/v1/knowledge-bases/${kbId}/questions/${question.id}`, { revision: question.revision })
}
