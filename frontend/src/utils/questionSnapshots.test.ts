import assert from 'node:assert/strict'
import test from 'node:test'
import { isQuestionSnapshot, questionSnapshots } from './questionSnapshots'

const original = { id: 'q', revision: 1, knowledge_base_id: 'bank', knowledge_id: 'image', image_ref: 'resource://original', question_type: 'fill_blank', stem: '旅客持有____。', options: [], answer: { blanks: ['铁路有效乘车凭证'] }, answer_raw: '铁路有效乘车凭证', blank_count: 1 }
test('题目快照支持历史JSON恢复与同题去重', () => {
  const restored: unknown = JSON.parse(JSON.stringify([{ question: original }, { question: original }]))
  assert.equal(questionSnapshots(restored).length, 1)
  assert.deepEqual(questionSnapshots(restored)[0], original)
})
test('损坏的历史或外部工具元数据不会作为题目卡片渲染', () => {
  for (const value of [null, {}, { ...original, options: null }, { ...original, answer: { truth: 'true' } }, { ...original, question_type: 'essay' }, { ...original, image_ref: null }]) {
    assert.equal(isQuestionSnapshot(value), false)
  }
  assert.deepEqual(questionSnapshots([{ content: '普通文档' }, { question: {} }]), [])
})
