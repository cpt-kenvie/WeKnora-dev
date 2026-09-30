import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import vm from 'node:vm'
import ts from 'typescript'
import { computed, reactive, ref } from 'vue'

const source = readFileSync(new URL('./Input-field.vue', import.meta.url), 'utf8')
const imageCheck = source.slice(source.indexOf('const isImageFile ='), source.indexOf('const handleDroppedFiles ='))
const sendCode = source.slice(source.indexOf('const hasImageInput ='), source.indexOf('const updateAgentModeDropdownPosition ='))
const chat = readFileSync(new URL('../views/chat/index.vue', import.meta.url), 'utf8')
const firstMessageCode = chat.slice(chat.indexOf('const hasFirstMessage ='), chat.indexOf('const { onChunk,'))

function fixture() {
  const query = ref('')
  const uploadedImages = ref([])
  const uploadedAttachments = ref([])
  const props = reactive({ isReplying: false, canSteer: false, composerLocked: false, embeddedMode: false })
  const events = [], notices = []
  const api = vm.runInNewContext(ts.transpile(`${imageCheck}\n${sendCode}\n({ canSend, createSession })`), {
    computed, query, uploadedImages, uploadedAttachments, props,
    MessagePlugin: { info: value => notices.push(value), warning: value => notices.push(value), error: value => notices.push(value) },
    t: key => key, emit: (...args) => events.push(args),
    selectedAgent: ref({ is_builtin: false, config: { agent_mode: 'quick-answer' } }),
    selectedModelId: ref('model'), settingsStore: {}, allSelectedItems: ref([]),
    chatResources: { isLoaded: () => true },
    collectAgentNotReadyReasons: () => ({ keys: [], labels: [] }),
    attachmentUploadRef: ref(), URL: { revokeObjectURL() {} },
    clearvalue: () => { query.value = '' }, focusInput: async () => {},
  })
  return { query, uploadedImages, uploadedAttachments, props, events, notices, ...api }
}

const picture = () => new File(['image bytes'], 'question.png', { type: 'image/png' })

test('pure image enables sending, carries the file and keeps query empty', async () => {
  const f = fixture()
  assert.equal(f.canSend.value, false)
  const file = picture()
  f.uploadedImages.value = [{ file, preview: 'blob:question' }]
  assert.equal(f.canSend.value, true)
  await f.createSession('')
  assert.equal(f.events.length, 1)
  assert.equal(f.events[0][0], 'send-msg')
  assert.equal(f.events[0][1], '')
  assert.equal(f.events[0][4][0], file)
  assert.equal(f.uploadedImages.value.length, 0)
  assert.equal(f.canSend.value, false)
})

test('image attachment accepts no text and retains upload failure and pending guards', async () => {
  for (const status of ['local', 'processing', 'ready', 'uploading', 'failed']) {
    const f = fixture()
    f.uploadedAttachments.value = [{ id: 'image', file: picture(), status, name: 'question.png' }]
    await f.createSession('')
    assert.equal(f.events.length, ['uploading', 'failed'].includes(status) ? 0 : 1, status)
    if (f.events.length) assert.equal(f.events[0][5][0].id, 'image')
  }
})

test('removing the last image disables empty sends, while ordinary text still sends', async () => {
  const f = fixture()
  f.uploadedImages.value.push({ file: picture(), preview: 'blob:question' })
  f.uploadedImages.value.splice(0)
  f.query.value = ' \n\u3000'
  assert.equal(f.canSend.value, false)
  await f.createSession(f.query.value)
  assert.equal(f.events.length, 0)
  f.query.value = '回答这道题'
  assert.equal(f.canSend.value, true)
  await f.createSession(f.query.value)
  assert.equal(f.events[0][1], '回答这道题')
})

test('image-only sends respect composer lock and running-turn attachment restrictions', async () => {
  for (const state of [{ composerLocked: true }, { isReplying: true, canSteer: true }, { isReplying: true, canSteer: false }]) {
    const f = fixture()
    Object.assign(f.props, state)
    f.uploadedImages.value.push({ file: picture(), preview: 'blob:question' })
    await f.createSession('')
    assert.equal(f.events.length, 0)
    assert.equal(f.uploadedImages.value.length, 1)
  }
})

test('new sessions send their first image even when there is no first query', async () => {
  const start = chat.indexOf('onMounted(async () => {', chat.indexOf('onBeforeMount('))
  const end = chat.indexOf('const clearData =', start)
  assert.ok(start >= 0 && end > start)
  const file = picture()
  const imageFiles = ref([file])
  const sends = []
  let mounted
  vm.runInNewContext(ts.transpile(firstMessageCode + chat.slice(start, end)), {
    computed,
    onMounted: fn => { mounted = fn }, window: { addEventListener() {} },
    SESSION_MUTATION_EVENT: 'session', handleSessionMutation() {},
    messagesList: [], steerQueue: ref([]), loading: ref(false), isReplying: ref(false),
    firstQuery: ref(''), firstImageFiles: imageFiles, firstAttachmentFiles: ref([]),
    firstModelId: ref(''), firstMentionedItems: ref([]), firstQuestionOrigin: ref(null),
    scrollLock: ref(false), historyLoading: ref(false),
    sendMsg: (...args) => sends.push(args), usemenuStore: { changeFirstQuery() {} },
  })
  await mounted()
  assert.equal(sends.length, 1)
  assert.equal(sends[0][0], '')
  assert.equal(sends[0][3][0], file)
})

test('image-only draft keeps its selected agent and knowledge base during delayed session restoration', async () => {
  const start = chat.indexOf('const loadSessionAndHydrate =')
  const end = chat.indexOf('const inputFieldRef =', start)
  const imageFiles = ref([picture()])
  let resolveSession
  const session = new Promise(resolve => { resolveSession = resolve })
  const hydrated = []
  const api = vm.runInNewContext(ts.transpile(firstMessageCode + chat.slice(start, end) + '\n({ loadSessionAndHydrate })'), {
    computed, firstQuery: ref(''), firstImageFiles: imageFiles, firstAttachmentFiles: ref([]),
    props: { embeddedMode: false }, session_id: ref('new-session'), currentSession: ref(null),
    getSession: () => session, useSettingsStoreInstance: { hydrateSessionInputState: (...args) => hydrated.push(args) },
  })
  const pending = api.loadSessionAndHydrate('new-session')
  imageFiles.value = []
  resolveSession({ data: { last_request_state: null } })
  await pending
  assert.equal(hydrated.length, 1)
  assert.equal(hydrated[0][1], true)
})
