package record

// phpDriver records a PHP unit: it includes the file, calls every planned function with the values the plan
// asks for, and writes one JSON line per call.
//
// The values come from the same table the other recorders use, printed as PHP literals by this file, so a
// case is generated here and not by hand.
const phpDriver = `<?php
// The recorder for PHP units.
//
//   php record.php <unit.php> <out.jsonl> <plan.json> [extra.jsonl] [only]

$unit = $argv[1];
$out_path = $argv[2];
$plan_path = $argv[3];
$extra_path = $argv[4] ?? '';
$only = array_filter(explode(',', $argv[5] ?? ''));

const MAX_BYTES = 65536;
const PAIRWISE_HEAD = 6;

function values_for(string $type): array {
    $strings = ['', 'a', 'abc', '  ', 'a b  c', "it's", 'a/b', '~0~1', '中文 字', 'a\\\\b', 'a|b', 'a;b',
        'cmd --flag | tee', '$(id)', '"', "'", '--flag', '0', 'héllo', '😀😀'];
    switch ($type) {
        case 'int': return [0, 1, -1, 2, 7, -13, 100, 1048576, 2147483647];
        case 'float': return [0.0, 1.0, -1.0, 2.0, 0.5, -0.5, 1e6, NAN, INF, -INF];
        case 'boolean': return [true, false];
        case 'String': return $strings;
        case 'char': return ['a', 'Z', '0', ' ', 'é'];
        default: return array_merge(array_slice($strings, 0, 8), [0, 1, -1, true, false, null]);
    }
}

function plan_calls(array $plan, array $only): array {
    $calls = [];
    foreach ($plan as $name => $entry) {
        if ($only && !in_array($name, $only, true)) { continue; }
        // The plan keeps both names: the export name is what a case is filed under, the path is what can be called.
        // Using the first for both called a function that does not exist, and every call was skipped.
        $target = $entry['path'] ?? $name;
        $types = $entry['types'] ?? [];
        if (!$types) { $calls[] = [$name, $target, []]; continue; }
        $tables = array_map('values_for', $types);
        $longest = max(array_map('count', $tables));
        for ($i = 0; $i < $longest; $i++) {
            $args = [];
            foreach ($tables as $j => $t) { $args[] = $t[$i % count($t)]; }
            $calls[] = [$name, $target, $args];
        }
        $head = array_map(fn($t) => array_slice($t, 0, PAIRWISE_HEAD), $tables);
        if (count($head) <= 3) {
            $stack = [[]];
            foreach ($head as $column) {
                $next = [];
                foreach ($stack as $prefix) {
                    foreach ($column as $v) { $next[] = array_merge($prefix, [$v]); }
                }
                $stack = $next;
            }
            foreach ($stack as $args) { $calls[] = [$name, $target, $args]; }
        }
    }
    return $calls;
}

// mark is the wire: what JSON cannot carry, spelled the way both sides agree on.
function mark($v) {
    if (is_float($v)) {
        if (is_nan($v)) return ['$number' => 'NaN'];
        if (is_infinite($v)) return ['$number' => $v > 0 ? 'Infinity' : '-Infinity'];
        if ($v == 0 && fdiv(1, $v) < 0) return ['$number' => '-0'];
        return $v;
    }
    if (is_array($v)) return array_map('mark', $v);
    return $v;
}

require $unit;

$plan = json_decode(file_get_contents($plan_path), true);
$calls = plan_calls($plan, $only);
if ($extra_path) {
    foreach (file($extra_path, FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) as $line) {
        $c = json_decode($line, true);
        $calls[] = [$c['fn'], $c['args'] ?? []];
    }
}

$out = fopen($out_path, 'w');
$written = 0;
foreach ($calls as [$name, $target, $args]) {
    // Class.method is not a callable name; Class::method is. function_exists was false for every method, so the
    // recorder wrote zero cases and said nothing else.
    if (strpos($target, '.') !== false) { $target = str_replace('.', '::', $target); }
    if (!is_callable($target)) { continue; }
    $case = ['fn' => $name, 'args' => array_map('mark', $args)];
    try {
        $answer = mark(call_user_func_array($target, $args));
        if (strlen(json_encode($answer)) > MAX_BYTES) { continue; }
        $case['ok'] = $answer;
    } catch (Throwable $e) {
        $case['error'] = get_class($e) . ': ' . $e->getMessage();
    }
    fwrite($out, json_encode($case, JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION) . "\n");
    $written++;
}
fclose($out);
fwrite(STDERR, "$written cases from $unit\n");
`
