// A workload: run the unit the way a caller would, enough times that the profile has samples.
import { count, words, rangeOf } from './src/text.ts'

const text = '$5 apple and $x y, naïve café '.repeat(20)
let total = 0
for (let i = 0; i < 20000; i++) {
  total += count(text)
  total += words(text).length
  total += rangeOf(text, 'apple').start
}
console.log('done', total)
