export const maxGroupNoteLength = 200
const maxGroupNotesBytes = 2 * 1024 * 1024
const encoder = new TextEncoder()
const escapedByGo = new RegExp(
  `[<>&${String.fromCodePoint(0x2028, 0x2029)}]`,
  'g'
)

export type GroupNotesError = 'format' | 'noteLength' | 'size'

type ParsedGroupNotes =
  | { notes: Map<string, string>; json: string }
  | { error: GroupNotesError }

// Match Go's strings.TrimSpace, rather than JS trim (which handles BOM/NEL differently).
export function trimGroupNote(text: string): string {
  return text.replaceAll(/^\p{White_Space}+|\p{White_Space}+$/gu, '')
}

export function parseGroupNotes(value: string): ParsedGroupNotes {
  if (encoder.encode(value).byteLength > maxGroupNotesBytes) {
    return { error: 'size' }
  }

  let parsed: unknown
  try {
    parsed = JSON.parse(value)
  } catch {
    return { error: 'format' }
  }
  if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { error: 'format' }
  }

  const notes = new Map<string, string>()
  for (const [name, text] of Object.entries(parsed)) {
    if (!name || name !== trimGroupNote(name) || typeof text !== 'string') {
      return { error: 'format' }
    }
    const note = trimGroupNote(text)
    if ([...note].length > maxGroupNoteLength) return { error: 'noteLength' }
    if (note) notes.set(name, note)
  }
  const json = JSON.stringify(
    Object.fromEntries(
      [...notes.keys()].sort().map((name) => [name, notes.get(name)])
    )
  )
  // Go's JSON encoder escapes these characters; include that expansion in the limit.
  const encoded = json.replaceAll(
    escapedByGo,
    (char) => `\\u${char.charCodeAt(0).toString(16).padStart(4, '0')}`
  )
  if (encoder.encode(encoded).byteLength > maxGroupNotesBytes) {
    return { error: 'size' }
  }
  return { notes, json }
}

export function getPricingGroupNames(
  groupRatio: string,
  topupGroupRatio: string
): Set<string> {
  const names = new Set<string>()
  for (const raw of [groupRatio, topupGroupRatio]) {
    try {
      const parsed: unknown = JSON.parse(raw)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        continue
      }
      for (const name of Object.keys(parsed)) names.add(name)
    } catch {
      // The existing ratio fields are validated separately by the form.
    }
  }
  return names
}
