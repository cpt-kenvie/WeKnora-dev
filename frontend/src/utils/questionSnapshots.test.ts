import assert from 'node:assert/strict'
import test from 'node:test'
import { citedQuestionSnapshots, isQuestionSnapshot, questionSnapshots } from './questionSnapshots'

const original = { id: 'q', revision: 1, knowledge_base_id: 'bank', knowledge_id: 'image', image_ref: 'resource://original', question_type: 'fill_blank', stem: '旅客持有____。', options: [], answer: { blanks: ['铁路有效乘车凭证'] }, answer_raw: '铁路有效乘车凭证', blank_count: 1 }
test('题目快照支持历史JSON恢复与同题去重', () => {
  const restored: unknown = JSON.parse(JSON.stringify([{ question: original }, { question: original }]))
  assert.equal(questionSnapshots(restored).length, 1)
  assert.deepEqual(questionSnapshots(restored)[0], original)
})

test('召回三题时仅展示模型实际引用的一题，原图和标准答案保持快照内容', () => {
  const references = [
    { id: 'passenger', question: original },
    { id: 'fare', question: { ...original, id: 'fare-question', stem: '无票乘车拒绝补票应追补什么？' } },
    { id: 'luggage', question: { ...original, id: 'luggage-question', stem: '禁止携带什么？' } },
  ]
  const content = '须持有铁路有效乘车凭证。<kb doc="原图" chunk_id="passenger" kb_id="bank" />'
  assert.deepEqual(citedQuestionSnapshots(references, content), [original])
  assert.deepEqual(citedQuestionSnapshots(JSON.parse(JSON.stringify(references)), content), [original])
  // 同图多题也只能通过分块引用选中，正文出现图名或原图链接不代表选用了所有题。
  assert.deepEqual(citedQuestionSnapshots(references, '![原图](resource://original)'), [])
  assert.deepEqual(citedQuestionSnapshots(references, '没有匹配的原题。'), [])
  assert.deepEqual(citedQuestionSnapshots(references, '<kb doc="原图" chunk_id="unknown" />'), [])
  assert.deepEqual(citedQuestionSnapshots(references, '<kb chunk_id="passenger"'), [])
  assert.deepEqual(citedQuestionSnapshots(references, '`<kb chunk_id="passenger"/>`'), [])
  assert.deepEqual(citedQuestionSnapshots(references, '```html\n<kb chunk_id="passenger"/>\n```'), [])
  assert.deepEqual(citedQuestionSnapshots(references, '<kb data-chunk_id="passenger"/>'), [])
})

test('多题回答按引用顺序展示并去重，不采用无效或普通文档快照', () => {
  const second = { ...original, id: 'q2', revision: 2 }
  const refs = [{ id: 'c1', question: original }, { id: 'c2', question: second }, { id: 'doc', content: '文档' }, { id: 'invalid', question: {} }]
  assert.deepEqual(citedQuestionSnapshots(refs, '<kb chunk_id="c2"/><kb chunk_id="c1"/><kb chunk_id="c2"/><kb chunk_id="doc"/><kb chunk_id="invalid"/>'), [second, original])
  assert.deepEqual(citedQuestionSnapshots(null, '<kb chunk_id="c1"/>'), [])
})
test('损坏的历史或外部工具元数据不会作为题目卡片渲染', () => {
  for (const value of [null, {}, { ...original, options: null }, { ...original, answer: { truth: 'true' } }, { ...original, question_type: 'essay' }, { ...original, image_ref: null }]) {
    assert.equal(isQuestionSnapshot(value), false)
  }
  assert.deepEqual(questionSnapshots([{ content: '普通文档' }, { question: {} }]), [])
})
