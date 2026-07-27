import { createElement, Fragment, type ReactNode } from 'react'

// Minimal Markdown renderer for the ToS document. Renders to React nodes (never
// dangerouslySetInnerHTML) so text can never be interpreted as HTML. Covers only
// what a ToS needs (headings, paragraphs, lists, bold/italic) per the minimalism ladder.
export function renderMarkdown(md: string): ReactNode[] {
  const blocks = md.replace(/\r\n/g, '\n').trim().split(/\n{2,}/)
  return blocks.map((block, i) => renderBlock(block, i))
}

function renderBlock(block: string, key: number): ReactNode {
  const heading = /^(#{1,3})\s+(.*)$/.exec(block)
  if (heading) {
    const level = heading[1].length
    const cls = ['text-[17px] font-bold', 'text-[15px] font-semibold', 'text-[14px] font-semibold'][level - 1]
    return createElement(`h${level}`, { key, className: cls }, renderInline(heading[2]))
  }

  const lines = block.split('\n')
  if (lines.every((l) => /^[-*]\s+/.test(l))) {
    return createElement(
      'ul',
      { key, className: 'list-disc pl-5 flex flex-col gap-1' },
      lines.map((l, j) => createElement('li', { key: j }, renderInline(l.replace(/^[-*]\s+/, '')))),
    )
  }

  // Paragraph; single newlines become <br> to preserve line breaks.
  return createElement(
    'p',
    { key },
    lines.flatMap((line, j) => (j === 0 ? [renderInline(line)] : [createElement('br', { key: `br${j}` }), renderInline(line)])),
  )
}

function renderInline(text: string): ReactNode {
  const parts: ReactNode[] = []
  const re = /\*\*(.+?)\*\*|\*(.+?)\*/g
  let last = 0
  let m: RegExpExecArray | null
  let key = 0
  while ((m = re.exec(text)) !== null) {
    if (m.index > last) parts.push(text.slice(last, m.index))
    if (m[1] !== undefined) parts.push(createElement('strong', { key: key++ }, m[1]))
    else parts.push(createElement('em', { key: key++ }, m[2]))
    last = re.lastIndex
  }
  if (last < text.length) parts.push(text.slice(last))
  return createElement(Fragment, null, ...parts)
}
