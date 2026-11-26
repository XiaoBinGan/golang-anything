package record

// powershellDriver records a PowerShell unit: it dot-sources the script, calls every planned function with
// literal arguments, and writes one JSON line per call. PowerShell has ConvertTo-Json, so the values go in as
// literals and the answers come back through the same marker dialect the other recorders use.
const powershellDriver = `# The recorder for PowerShell units.
#
#   pwsh -NoLogo -File record.ps1 <unit.ps1> <out.jsonl> <plan.json> [extra.jsonl] [only]
param(
  [string]$Unit,
  [string]$OutPath,
  [string]$PlanPath,
  [string]$ExtraPath = '',
  [string]$Only = ''
)

$ErrorActionPreference = 'Stop'
. $Unit

$plan = Get-Content $PlanPath -Raw | ConvertFrom-Json
$onlyList = @($Only -split ',' | Where-Object { $_ -ne '' })

function ValuesFor([string]$type) {
  $strings = @('', 'a', 'abc', '  ', 'a b  c', "it's", 'a/b', '~0~1', '中文 字', 'a\b', 'a|b', 'a;b', '--flag', '0')
  switch ($type) {
    'int'     { return @(0, 1, -1, 2, 7, -13, 100) }
    'float'   { return @(0.0, 1.0, -1.0, 2.0, 0.5, -0.5) }
    'boolean' { return @($true, $false) }
    'String'  { return $strings }
    'char'    { return @('a', 'Z', '0') }
    default   { return @($strings[0..7] + @(0, 1, -1, $true, $false)) }
  }
}

function Literal($value, [string]$type) {
  if ($null -eq $value) { return '$null' }
  if ($value -is [string]) { return "'" + $value.Replace("'", "''") + "'" }
  if ($value -is [bool]) { return $(if ($value) { '$true' } else { '$false' }) }
  if ($value -is [double]) {
    if ([double]::IsNaN($value)) { return '[double]::NaN' }
    if ([double]::IsPositiveInfinity($value)) { return '[double]::PositiveInfinity' }
    if ([double]::IsNegativeInfinity($value)) { return '[double]::NegativeInfinity' }
    return $value.ToString('R', [System.Globalization.CultureInfo]::InvariantCulture)
  }
  return $value.ToString()
}

function Wire($value) {
  if ($null -eq $value) { return 'null' }
  if ($value -is [string]) { return (ConvertTo-Json $value -Compress) }
  if ($value -is [bool]) { return $(if ($value) { 'true' } else { 'false' }) }
  if ($value -is [double] -or $value -is [int] -or $value -is [long]) {
    if ($value -is [double] -and [double]::IsNaN($value)) { return '{"$number":"NaN"}' }
    if ($value -is [double] -and [double]::IsPositiveInfinity($value)) { return '{"$number":"Infinity"}' }
    if ($value -is [double] -and [double]::IsNegativeInfinity($value)) { return '{"$number":"-Infinity"}' }
    return $value.ToString('R', [System.Globalization.CultureInfo]::InvariantCulture)
  }
  if ($value -is [System.Collections.IEnumerable]) {
    $parts = @()
    foreach ($item in $value) { $parts += Wire $item }
    return '[' + ($parts -join ',') + ']'
  }
  return (ConvertTo-Json ([string]$value) -Compress)
}

$lines = New-Object System.Collections.Generic.List[string]
$written = 0
foreach ($entry in $plan.PSObject.Properties) {
  $name = $entry.Name
  if ($onlyList.Count -gt 0 -and $onlyList -notcontains $name) { continue }
  $types = @($entry.Value.types)
  $tables = @()
  foreach ($t in $types) { $tables += ,(ValuesFor $t) }
  if ($tables.Count -eq 0) { $tables = @() }

  $calls = @()
  if ($tables.Count -eq 0) {
    $calls += ,@()
  } else {
    $longest = ($tables | ForEach-Object { $_.Count } | Measure-Object -Maximum).Maximum
    for ($i = 0; $i -lt $longest; $i++) {
      $args = @()
      for ($j = 0; $j -lt $tables.Count; $j++) {
        $col = $tables[$j]
        $args += $col[$i % $col.Count]
      }
      $calls += ,$args
    }
  }

  foreach ($call in $calls) {
    $lits = @()
    $wires = @()
    for ($k = 0; $k -lt $call.Count; $k++) {
      $lits += (Literal $call[$k] $types[$k])
      $wires += (Wire $call[$k])
    }
    $argsJson = '[' + ($wires -join ',') + ']'
    # Splatting, not "$name(a, b)": PowerShell separates arguments with spaces, so a comma builds one array
    # argument. That mistake recorded the call as a single array argument, which bound to
    # $Text and left $Limit at 0 — every case then looked like "limit must be positive".
    try {
      $answer = & $name @call
      $lines.Add('{"fn":"' + $name + '","args":' + $argsJson + ',"ok":' + (Wire $answer) + '}')
      $written++
    } catch {
      $kind = $_.Exception.GetType().Name
      $msg = $_.Exception.Message.Replace('\', '\\').Replace('"', '\"').Replace([string][char]10, '\n')
      $lines.Add('{"fn":"' + $name + '","args":' + $argsJson + ',"error":"' + $kind + ': ' + $msg + '"}')
      $written++
    }
  }
}
Set-Content -Path $OutPath -Value $lines -Encoding utf8
[Console]::Error.WriteLine("$written cases from $Unit")
`
