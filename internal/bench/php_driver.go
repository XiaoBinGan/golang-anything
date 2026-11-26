package bench

// phpDriver times a PHP unit over its recorded cases: load the file, call the function in a loop, report
// nanoseconds per call.
const phpDriver = `<?php
// php bench.php <unit.php> <cases.jsonl> <out.json> <min_ms> <repeat>
[$script, $unit, $cases_path, $out_path, $min_ms, $repeat] = $argv + [null, null, null, null, 100, 1];
$min_ms = (float) $min_ms;
$repeat = (int) $repeat;
require $unit;

$byFn = [];
foreach (file($cases_path, FILE_IGNORE_NEW_LINES | FILE_SKIP_EMPTY_LINES) as $line) {
    $c = json_decode($line, true);
    $byFn[$c['fn']][] = $c['args'] ?? [];
}

$rows = [];
foreach ($byFn as $name => $argsets) {
    if (!function_exists($name)) { continue; }
    foreach ($argsets as $args) {
        try { $name(...$args); } catch (Throwable $e) {}
    }
    $rounds = 0;
    $start = hrtime(true);
    do {
        for ($r = 0; $r < $repeat; $r++) {
            foreach ($argsets as $args) {
                try { $name(...$args); } catch (Throwable $e) {}
            }
        }
        $rounds++;
        $elapsed = hrtime(true) - $start;
    } while ($elapsed / 1e6 < $min_ms && $rounds < 10000);
    $rows[] = ['name' => $name, 'cases' => count($argsets),
        'ns' => $elapsed / ($rounds * $repeat * count($argsets))];
}
file_put_contents($out_path, json_encode($rows));
`
