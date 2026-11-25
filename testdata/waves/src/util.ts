export function pad(text: string, width: number): string {
  return text.padStart(width)
}

export function repeat(text: string, times: number): string {
  let out = ''
  for (let i = 0; i < times; i++) out += text
  return out
}
