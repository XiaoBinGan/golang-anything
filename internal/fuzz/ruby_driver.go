package fuzz

// rubyOriginal answers a calls file on the original Ruby unit: the same job fuzz-original.mjs does for
// node, so a Ruby crate can be fuzzed with fresh inputs instead of only replayed.
//
//	ruby fuzz-original.rb <unit.rb> <calls.jsonl> <out.jsonl>
//
// The answers are marked the way the wire spells what JSON cannot carry, because diff compares them with the
// port's answers through the same markers.
const rubyOriginal = `require 'json'

unit, calls_path, out_path = ARGV[0], ARGV[1], ARGV[2]
load unit

def mark(value)
  case value
  when nil, true, false then value
  when Float
    return { '$number' => 'NaN' } if value.nan?
    return { '$number' => value.positive? ? 'Infinity' : '-Infinity' } if value.infinite?
    return { '$number' => '-0' } if value.zero? && (1.0 / value).negative?
    value == value.to_i && value.abs < 2**53 ? value.to_i : value
  when Integer
    value.abs >= 2**53 ? { '$bigint' => value.to_s } : value
  when Array then value.map { |v| mark(v) }
  when Hash then value.transform_values { |v| mark(v) }
  when String then value
  else { '$repr' => value.inspect }
  end
end

# The plan's path is Owner.method for a module function and plain name for a top-level def.
def target_for(path)
  return Object unless path.include?('.')
  path.split('.')[0..-2].inject(Object) { |mod, part| mod.const_get(part) }
end

File.open(out_path, 'w') do |out|
  File.readlines(calls_path).each do |line|
    next if line.strip.empty?
    c = JSON.parse(line)
    name = c['fn']
    path = c['path'] || name
    args = c['args'] || []
    answer = { 'fn' => name, 'args' => args }
    begin
      fn = path.include?('.') ? path.split('.').last : name
      answer['ok'] = mark(target_for(path).send(fn, *args))
    rescue StandardError, TypeError => e
      answer['error'] = "#{e.class}: #{e.message}"
    end
    out.puts(JSON.generate(answer))
  end
end
`
