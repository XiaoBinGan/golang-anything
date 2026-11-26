package bench

// rubyDriver times a Ruby unit over its recorded cases, the way originalScript does for node.
const rubyDriver = `require 'json'

unit, cases_path, out_path, min_ms, repeat = ARGV[0], ARGV[1], ARGV[2], ARGV[3].to_f, ARGV[4].to_i
load unit

by_fn = Hash.new { |h, k| h[k] = [] }
File.readlines(cases_path).each do |line|
  next if line.strip.empty?
  c = JSON.parse(line)
  by_fn[c['fn']] << (c['args'] || [])
end

def quietly(name, args)
  Object.send(name, *args)
rescue StandardError, TypeError
  nil
end

rows = []
by_fn.each do |name, argsets|
  next unless Object.private_method_defined?(name) || Object.method_defined?(name) || Object.respond_to?(name)
  argsets.each { |a| quietly(name, a) }
  rounds = 0
  elapsed = 0
  started = Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond)
  loop do
    repeat.times do
      argsets.each { |a| quietly(name, a) }
    end
    rounds += 1
    elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC, :nanosecond) - started
    break if elapsed / 1e6 >= min_ms || rounds >= 10_000
  end
  rows << { 'name' => name, 'cases' => argsets.length, 'ns' => elapsed.to_f / (rounds * repeat * argsets.length) }
end

File.write(out_path, JSON.generate(rows))
`
