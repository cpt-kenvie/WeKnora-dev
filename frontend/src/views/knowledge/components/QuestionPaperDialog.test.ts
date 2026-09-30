import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import { compileScript, parse } from '@vue/compiler-sfc'
import * as vue from 'vue'
import type { QuestionPaperOptions, QuestionPaperRequest } from '../../../api/question-bank.ts'
import * as paper from '../../../utils/questionPaper.ts'

const source = readFileSync(new URL('./QuestionPaperDialog.vue', import.meta.url), 'utf8')
const script = compileScript(parse(source).descriptor, { id: 'question-paper-dialog-test', inlineTemplate: true })
const compiled = ts.transpileModule(script.content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText

type Host = { type: string; props: Record<string, unknown>; children: Host[]; text: string; parent?: Host }
const node = (type: string): Host => ({ type, props: {}, children: [], text: '' })
const renderer = vue.createRenderer<Host, Host>({
  createElement: node,
  createText: text => ({ ...node('#text'), text }),
  createComment: text => ({ ...node('#comment'), text }),
  patchProp: (element, key, _old, value) => { element.props[key] = value },
  setText: (element, text) => { element.text = text },
  setElementText: (element, text) => { element.children = []; element.text = text },
  parentNode: element => element.parent || null,
  nextSibling: element => element.parent?.children[element.parent.children.indexOf(element) + 1] || null,
  insert(element, parent, anchor) {
    if (element.parent) element.parent.children.splice(element.parent.children.indexOf(element), 1)
    element.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, element)
  },
  remove(element) { element.parent?.children.splice(element.parent.children.indexOf(element), 1); element.parent = undefined },
})
const all = (element: Host, type: string): Host[] => [...(element.type === type ? [element] : []), ...element.children.flatMap(child => all(child, type))]
const textOf = (element: Host): string => element.text + element.children.map(textOf).join('')
const flush = async () => { await new Promise(resolve => setImmediate(resolve)); await vue.nextTick() }

function fixture() {
  const state = vue.reactive({ visible: true, kbId: 'bank', kbName: '安全题库' })
  const calls: { kbId: string; request: QuestionPaperRequest }[] = []
  const downloads: string[] = []
  const errors: string[] = []
  const available: QuestionPaperOptions = { counts: { single_choice: 12, multiple_choice: 12, true_false: 12, fill_blank: 12 }, max_questions: 1000 }
  let exportError = ''
  let exportSignal: AbortSignal | undefined
  let resolveExport: ((value: Blob) => void) | undefined
  let deferred = false
  const exports: { default?: vue.Component } = {}
  const link = { href: '', download: '', style: { display: '' }, click() { downloads.push(link.download) }, remove() {} }
  runInNewContext(compiled, {
    exports, AbortController, setTimeout, clearTimeout,
    URL: { createObjectURL: () => 'blob:paper', revokeObjectURL() {} },
    document: { createElement: () => link, body: { appendChild() {} } },
    require(name: string) {
      if (name === 'vue') return vue
      if (name === 'vue-i18n') return { useI18n: () => ({ t: (key: string) => key }) }
      if (name === 'tdesign-vue-next') return { MessagePlugin: { success() {}, error(value: string) { errors.push(value) } } }
      if (name === '@/utils/questionPaper') return paper
      if (name === '@/api/question-bank') return {
        getQuestionPaperOptions: async () => structuredClone(available),
        exportQuestionPaper: async (kbId: string, request: QuestionPaperRequest, signal: AbortSignal) => {
          calls.push({ kbId, request: { sections: Array.from(request.sections, section => ({ question_type: section.question_type, count: section.count })), include_answers: request.include_answers } })
          exportSignal = signal
          if (exportError) throw { message: exportError }
          if (deferred) return new Promise<Blob>(resolve => { resolveExport = resolve })
          return new Blob(['test'])
        },
      }
      throw new Error(`Unexpected import: ${name}`)
    },
  })
  assert.ok(exports.default)
  const component = exports.default
  const root = node('root')
  const app = renderer.createApp({ setup: () => () => vue.h(component, { ...state, 'onUpdate:visible': (value: boolean) => { state.visible = value } }) })
  for (const [name, type] of [['t-dialog', 'dialog'], ['t-button', 'button'], ['t-icon', 'icon'], ['t-loading', 'loading'], ['t-select', 'select'], ['t-option', 'option'], ['t-input-number', 'input'], ['t-switch', 'switch'], ['t-alert', 'alert']]) {
    assert.ok(name && type)
    app.component(name, vue.defineComponent({ setup: (_props, { slots }) => () => vue.h(type, {}, [slots.default?.(), slots.footer?.()]) }))
  }
  app.mount(root)
  const button = (key: string) => { const element = all(root, 'button').find(item => textOf(item) === key); assert.ok(element); return element }
  const fire = async (element: Host, event = 'onClick', value?: unknown) => {
    const callback = element.props[event]
    assert.equal(typeof callback, 'function')
    if (typeof callback === 'function') await callback(value)
    await flush()
  }
  return { state, calls, downloads, errors, root, button, fire,
    close: () => app.unmount(),
    fail: (message: string) => { exportError = message },
    defer: () => { deferred = true },
    resolve: () => { resolveExport?.(new Blob(['test'])) },
    signal: () => exportSignal,
  }
}

test('弹窗多行配置、合计校验，答案开关传入同一次导出请求', async t => {
  const f = fixture(); t.after(f.close); await flush()
  await f.fire(f.button('questionBank.paper.addRow'))
  assert.equal(all(f.root, 'input').length, 2)
  await f.fire(all(f.root, 'select')[1]!, 'onUpdate:modelValue', 'single_choice')
  assert.equal(f.button('questionBank.paper.export').props.disabled, true)
  assert.ok(all(f.root, 'alert').some(element => element.props.message === 'questionBank.paper.shortage'))
  await f.fire(all(f.root, 'input')[1]!, 'onUpdate:modelValue', 2)
  assert.equal(f.button('questionBank.paper.export').props.disabled, false)
  await f.fire(all(f.root, 'switch')[0]!, 'onUpdate:modelValue', true)
  await f.fire(f.button('questionBank.paper.export'))
  assert.equal(f.calls.length, 1, JSON.stringify(all(f.root, 'alert').map(element => element.props)))
  assert.deepEqual(f.calls, [{ kbId: 'bank', request: { sections: [{ question_type: 'single_choice', count: 10 }, { question_type: 'single_choice', count: 2 }], include_answers: true } }])
  assert.deepEqual(f.downloads, ['安全题库-试卷-含答案.docx'])
  assert.equal(f.state.visible, false)
})

test('服务端数量变化错误保留在弹窗内，可修改配置重试', async t => {
  const f = fixture(); t.after(f.close); await flush()
  f.fail('单选题已核对题目不足')
  await f.fire(f.button('questionBank.paper.export'))
  assert.equal(f.state.visible, true)
  assert.deepEqual(f.downloads, [])
  assert.ok(all(f.root, 'alert').some(element => element.props.message === '单选题已核对题目不足'))
  assert.equal(all(f.root, 'input')[0]?.props.modelValue, 10)
  assert.equal(f.button('questionBank.paper.export').props.disabled, false)
})

test('切换题库时取消在途导出，防止下载上一题库的试卷', async t => {
  const f = fixture(); t.after(f.close); await flush(); f.defer()
  const pending = f.fire(f.button('questionBank.paper.export'))
  await flush()
  assert.equal(f.button('questionBank.paper.export').props.disabled, true)
  f.state.kbId = 'another-bank'
  await flush()
  assert.equal(f.signal()?.aborted, true)
  f.resolve()
  await pending
  assert.deepEqual(f.downloads, [])
})
