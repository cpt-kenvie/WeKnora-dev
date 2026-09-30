import assert from 'node:assert/strict'
import test from 'node:test'
import { questionPaperFilename, validateQuestionPaper } from './questionPaper.ts'
import type { QuestionPaperOptions } from '../api/question-bank.ts'

const options: QuestionPaperOptions = { counts: { single_choice: 12, multiple_choice: 8, true_false: 0, fill_blank: 10 }, max_questions: 1000 }

test('同题型多行累计校验，题量不足时指出具体题型和数量', () => {
  assert.deepEqual(validateQuestionPaper([{ question_type: 'single_choice', count: 10 }, { question_type: 'single_choice', count: 3 }], options),
    { kind: 'shortage', questionType: 'single_choice', requested: 13, available: 12 })
  assert.deepEqual(validateQuestionPaper([{ question_type: 'true_false', count: 1 }], options),
    { kind: 'shortage', questionType: 'true_false', requested: 1, available: 0 })
  assert.equal(validateQuestionPaper([{ question_type: 'single_choice', count: 10 }, { question_type: 'fill_blank', count: 10 }], options), null)
})

test('禁止空配置、无效数量和超过上限的试卷', () => {
  assert.deepEqual(validateQuestionPaper([], options), { kind: 'count' })
  for (const count of [0, -1, 1.5, NaN, Infinity]) {
    assert.deepEqual(validateQuestionPaper([{ question_type: 'single_choice', count }], options), { kind: 'count' })
  }
  assert.deepEqual(validateQuestionPaper([{ question_type: 'single_choice', count: 1001 }], options), { kind: 'limit', max: 1000 })
})

test('下载文件名保留中文并移除路径和非法字符', () => {
  assert.equal(questionPaperFilename('安全题库', false), '安全题库-试卷.docx')
  assert.equal(questionPaperFilename('安全题库', true), '安全题库-试卷-含答案.docx')
  assert.equal(questionPaperFilename('题库/文件\\"名:<>?*|\u0000', false), '题库_文件__名_______-试卷.docx')
  assert.equal(questionPaperFilename('', false), '题库-试卷.docx')
})
