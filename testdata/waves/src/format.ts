import { pad, repeat } from './util'

export function box(label: string, width: number): string {
  if (width < 4) throw new RangeError('box: width must be at least 4')
  const inner = pad(label, width - 2)
  return repeat('-', 1) + inner + repeat('-', 1)
}
