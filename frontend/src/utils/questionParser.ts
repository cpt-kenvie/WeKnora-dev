import type { ParserEngineRule } from '@/views/knowledge/settings/KBParserSettings.vue'

export const questionImageExtensions = ['jpg', 'jpeg', 'png', 'bmp', 'webp']

// 与后端一致：未指定引擎及内置图片解析走视觉识别，外部解析器提供 OCR 原文。
export function hasQuestionImageParser(rules: ParserEngineRule[] = []): boolean {
  return questionImageExtensions.some(extension => {
    const rule = rules.find(item => item.file_types.some(type => type.trim().toLowerCase().replace(/^\./, '') === extension))
    return !!rule?.engine.trim() && !['simple', 'builtin'].includes(rule.engine.trim())
  })
}
