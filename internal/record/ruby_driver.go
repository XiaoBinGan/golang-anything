package record

// rubyDriver records a Ruby unit: the same generated calls the other recorders make, answered by loading the
// file and sending the method name.
const rubyDriver = `# The recorder for Ruby units.
#
#   ruby record.rb <unit.rb> <out.jsonl> <plan.json> [extra.jsonl] [only]
require 'json'

unit_path, out_path, plan_path = ARGV[0], ARGV[1], ARGV[2]
extra_path = ARGV[3].to_s
only = (ARGV[4].to_s.split(',') - [''])

MAX_BYTES = 64 * 1024
PAIRWISE_HEAD = 6

STRINGS = ['', 'a', 'abc', '  ', 'a b  c', "it's", 'a/b', '~0~1', '中文 字', 'a\\b', 'a|b', 'a;b',
           'cmd --flag | tee', '$(id)', '"', "'", '--flag', '0', 'héllo', '😀😀'].freeze
NUMBERS = [0, 1, -1, 2, 0.5, -0.5, 1_000_000, -1_000_000, Float::NAN, Float::INFINITY, -Float::INFINITY,
           2**53, 3.0].freeze
BOOLEANS = [true, false].freeze
LISTS = [[], [1], ['a'], [1, 'a'], [nil], [[1], [2]]].freeze
NONES = [nil].freeze

def values_for(type)
  case type.to_s
  when 'str', 'string', 'String', 'text' then STRINGS
  when 'int', 'float', 'number', 'Integer', 'Float' then NUMBERS
  when 'bool', 'boolean', 'TrueClass', 'FalseClass' then BOOLEANS
  when 'list', 'Array', 'array' then LISTS
  when 'nil', 'None', 'NilClass' then NONES
  else STRINGS.first(8) + NUMBERS.first(8) + BOOLEANS + LISTS.first(3) + NONES
  end.uniq { |v| v.inspect }
end

def plan_calls(plan, only)
  calls = []
  plan.each do |name, entry|
    next if !only.empty? && !only.include?(name)
    types = entry.is_a?(Hash) ? (entry['types'] || []) : entry
    path = entry.is_a?(Hash) ? (entry['path'] || name) : name
    if types.empty?
      calls << [name, path, []]
      next
    end
    tables = types.map { |t| values_for(t) }
    longest = tables.map(&:length).max
    (0...longest).each do |i|
      calls << [name, path, (0...tables.length).map { |j| tables[j][i % tables[j].length] }]
    end
    head = tables.map { |t| t.first(PAIRWISE_HEAD) }
    if head.length <= 3
      head.inject([[]]) { |acc, col| acc.flat_map { |prefix| col.map { |v| prefix + [v] } } }.each do |args|
        calls << [name, path, args]
      end
    else
      # every position against a plain argument, so a bug in the last parameter is still reachable
      tables.each_with_index do |t, j|
        t.each do |value|
          args = tables.map(&:first)
          args[j] = value
          calls << [name, path, args]
        end
      end
    end
  end
  calls
end

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

load unit_path
plan = JSON.parse(File.read(plan_path))
calls = plan_calls(plan, only)
unless extra_path.empty?
  File.readlines(extra_path).each do |line|
    next if line.strip.empty?
    c = JSON.parse(line)
    calls << [c['fn'], c['fn'], c['args'] || []]
  end
end

written = 0
File.open(out_path, 'w') do |out|
  calls.each do |name, path, args|
    # A dotted path is Owner.method: the owner is a constant, the last segment is the method.
    target = if path.include?('.')
               owner = path.split('.')[0..-2].inject(Object) { |mod, part| mod.const_get(part) }
               owner
             else
               Object
             end
    method_name = path.include?('.') ? path.split('.').last : name
    # A top-level def is a private method of Object, so method_defined? is false and respond_to? is false
    # too; send is the only way to reach it, and that is what a caller of the original does.
    visible = target.method_defined?(method_name) || target.private_method_defined?(method_name) ||
              target.respond_to?(method_name)
    next unless visible
    case_ = { 'fn' => name, 'args' => args.map { |a| mark(a) } }
    begin
      answer = mark(target.send(method_name, *args))
      next if JSON.generate(answer).bytesize > MAX_BYTES
      case_['ok'] = answer
    rescue StandardError, TypeError => e
      case_['error'] = "#{e.class}: #{e.message}"
    end
    out.puts(JSON.generate(case_))
    written += 1
  end
end
warn "#{written} cases from #{unit_path}"
`
