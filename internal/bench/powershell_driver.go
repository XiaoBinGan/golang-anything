package bench

// powershellDriver times a PowerShell unit over its recorded cases.
const powershellDriver = `# pwsh -NoLogo -File bench.ps1 <unit.ps1> <cases.jsonl> <out.json> <min_ms> <repeat>
param(
  [string]$Unit,
  [string]$CasesPath,
  [string]$OutPath,
  [double]$MinMs = 100,
  [int]$Repeat = 1
)
$ErrorActionPreference = 'Stop'
. $Unit

$byFn = @{}
foreach ($line in Get-Content $CasesPath) {
  if ([string]::IsNullOrWhiteSpace($line)) { continue }
  $c = $line | ConvertFrom-Json
  if (-not $byFn.ContainsKey($c.fn)) { $byFn[$c.fn] = New-Object System.Collections.ArrayList }
  [void]$byFn[$c.fn].Add(@($c.args))
}

$rows = @()
foreach ($name in $byFn.Keys) {
  if (-not (Get-Command $name -ErrorAction SilentlyContinue)) { continue }
  $argsets = $byFn[$name]
  foreach ($a in $argsets) { try { & $name @a } catch {} }
  $rounds = 0
  $start = [System.Diagnostics.Stopwatch]::StartNew()
  do {
    for ($r = 0; $r -lt $Repeat; $r++) {
      foreach ($a in $argsets) { try { & $name @a } catch {} }
    }
    $rounds++
  } while ($start.Elapsed.TotalMilliseconds -lt $MinMs -and $rounds -lt 10000)
  $ns = ($start.Elapsed.TotalMilliseconds * 1e6) / ($rounds * $Repeat * $argsets.Count)
  $rows += [pscustomobject]@{ name = $name; cases = $argsets.Count; ns = $ns }
}
$json = '[' + (($rows | ForEach-Object {
  '{"name":"' + $_.name + '","cases":' + $_.cases + ',"ns":' + $_.ns.ToString([System.Globalization.CultureInfo]::InvariantCulture) + '}'
}) -join ',') + ']'
Set-Content -Path $OutPath -Value $json -Encoding utf8
`
