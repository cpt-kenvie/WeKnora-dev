import assert from 'node:assert/strict'
import test from 'node:test'
import { hasQuestionImageParser } from './questionParser.ts'

test('题库的默认与内置图片规则仍需视觉模型', () => {
  assert.equal(hasQuestionImageParser(), false)
  for (const engine of ['', ' ', 'simple', 'builtin']) {
    assert.equal(hasQuestionImageParser([{ file_types: ['jpg'], engine }]), false)
  }
})

test('仅支持图片的有效解析规则可以替代视觉模型', () => {
  assert.equal(hasQuestionImageParser([{ file_types: ['pdf'], engine: 'mineru_cloud' }]), false)
  assert.equal(hasQuestionImageParser([{ file_types: ['.JPG', 'png'], engine: 'mineru_cloud' }]), true)
  assert.equal(hasQuestionImageParser([
    { file_types: ['jpg'], engine: 'simple' },
    { file_types: ['jpg'], engine: 'mineru_cloud' },
  ]), false)
  assert.equal(hasQuestionImageParser([
    { file_types: ['webp'], engine: 'simple' },
    { file_types: ['png'], engine: 'paddleocr_vl' },
  ]), true)
})
