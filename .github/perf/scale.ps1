# The memory workflow's Windows leg (perf.yml's scale-windows job): what
# scale.sh does on Linux, on the windows-latest runner. The runner has no
# GPU: Gio's Direct3D 11 device lands on the Microsoft Basic Render Driver
# (WARP behind a WDDM adapter), so texture memory is system memory there,
# as on a laptop's integrated GPU. adapter.txt names what the runner had.
#
# Besides the driver's own files, each stage gets <stage>.external.txt:
# Get-Process and the "Working Set - Private" counter (Task Manager's
# Memory column) read from outside as the stage lands.
#
# usage: scale.ps1 -Fake <fakeslackd.exe> -Out <dir> -Sides head[,base]
param(
  [Parameter(Mandatory)] [string]$Fake,
  [Parameter(Mandatory)] [string]$Out,
  [Parameter(Mandatory)] [string[]]$Sides
)
$ErrorActionPreference = 'Stop'
$work = Join-Path $env:RUNNER_TEMP 'scale-work'
New-Item -ItemType Directory -Force $work | Out-Null
Get-CimInstance Win32_VideoController | Format-List Name, AdapterCompatibility, DriverVersion, AdapterRAM, VideoModeDescription | Out-File (Join-Path $Out 'adapter.txt')
Get-Content (Join-Path $Out 'adapter.txt')

$fakeArgs = @('-scale', '-addr', '127.0.0.1:8931', '-creds', "$work\creds.json", '-userids', "$work\users.txt")
$fp = Start-Process -PassThru -WindowStyle Hidden -FilePath $Fake -ArgumentList $fakeArgs -RedirectStandardError (Join-Path $Out 'fakeslackd-scale.log')
for ($i = 0; $i -lt 600 -and -not (Test-Path "$work\creds.json"); $i++) { Start-Sleep -Milliseconds 200 }
if (-not (Test-Path "$work\creds.json")) { throw 'fakeslackd -scale wrote no credentials' }
Write-Host "directory users: $(@(Get-Content "$work\users.txt").Count)"

$lib = Join-Path $Out 'library'
function Seed-Home([string]$h) {
  '{"route":"c/C0FAKE00000","last_channel":"C0FAKE00000"}' | Out-File -Encoding ascii (Join-Path $h 'ui.json')
  if (-not (Test-Path (Join-Path $lib 'index.json'))) { 'plugins: none' | Out-File (Join-Path $Out 'scale-plugins.txt'); return }
  New-Item -ItemType Directory -Force (Join-Path $h 'taut\plugins'), (Join-Path $h 'slick\plugins') | Out-Null
  $tauts = 'HumanCount', 'LastSeen', 'WhoReacted', 'NoTrack', 'ShutUpSlackbot'
  $cfg = @()
  foreach ($id in $tauts) {
    $f = Join-Path $lib "taut-$id.js"
    if (-not (Test-Path $f)) { continue }
    Copy-Item $f (Join-Path $h "taut\plugins\$id.js")
    $cfg += "`"$id`":{`"enabled`":true}"
  }
  "{`"plugins`":{$($cfg -join ',')}}" | Out-File -Encoding ascii (Join-Path $h 'taut\config.json')
  $ct = Join-Path $lib 'slick-ChatTabs.js'
  if (Test-Path $ct) {
    Copy-Item $ct (Join-Path $h 'slick\plugins\ChatTabs.js')
    $ctm = Join-Path $lib 'slick-ChatTabs.main.js'
    if (Test-Path $ctm) { Copy-Item $ctm (Join-Path $h 'slick\plugins\ChatTabs.main.js') }
    '{"enabled":true,"plugins":{"ChatTabs":{"enabled":true}}}' | Out-File -Encoding ascii (Join-Path $h 'slick\settings.json')
  }
  "plugins: ChatTabs + $($tauts -join ' ')" | Out-File (Join-Path $Out 'scale-plugins.txt')
}

function Run-Clack([string]$side, [string]$home_, [string]$name, [string]$script, [int]$limit) {
  $d = Join-Path $Out "$side\scale\$name"
  New-Item -ItemType Directory -Force $d | Out-Null
  $env:CLACK_HOME = $home_
  $env:CLACK_LOG_DIR = $d
  $env:CLACK_PERF_DRIVE = $d
  $env:CLACK_PERF_SCRIPT = $script
  $env:CLACK_PERF_USERS = "$work\users.txt"
  $env:CLACK_TIMING = '1'
  $exe = Join-Path $Out "$side\clack-perf.exe"
  $p = Start-Process -PassThru -FilePath $exe
  $seen = @{}
  $t0 = Get-Date
  while (-not $p.HasExited) {
    foreach ($f in @(Get-ChildItem -Path $d -Filter '*.mem.txt' -ErrorAction SilentlyContinue)) {
      $st = $f.Name -replace '\.mem\.txt$', ''
      if ($seen.ContainsKey($st)) { continue }
      $seen[$st] = $true
      $p.Refresh()
      $pws = 'n/a'
      try {
        $pws = (Get-Counter "\Process(clack-perf)\Working Set - Private" -ErrorAction Stop).CounterSamples[0].CookedValue
      } catch { $pws = "counter error: $($_.Exception.Message)" }
      $pwsMB = if ($pws -is [double]) { [math]::Round($pws / 1MB, 1) } else { $pws }
      @(
        "stage $st",
        "ext_working_set $([math]::Round($p.WorkingSet64 / 1MB, 1)) MB",
        "ext_private_bytes $([math]::Round($p.PrivateMemorySize64 / 1MB, 1)) MB",
        "ext_peak_working_set $([math]::Round($p.PeakWorkingSet64 / 1MB, 1)) MB",
        "ext_private_working_set $pwsMB MB",
        "ext_handles $($p.HandleCount)",
        "ext_threads $($p.Threads.Count)"
      ) | Out-File (Join-Path $d "$st.external.txt")
      Write-Host "$side ${name}: stage $st at $([int]((Get-Date) - $t0).TotalSeconds) s: private working set $pwsMB MB"
    }
    if (Test-Path (Join-Path $d 'done')) { break }
    if (((Get-Date) - $t0).TotalSeconds -gt $limit) { Write-Host "::warning::$side ${name}: no done after ${limit}s"; break }
    Start-Sleep -Seconds 1
  }
  if (-not $p.HasExited) { Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue; $p.WaitForExit(15000) | Out-Null }
  $log = Join-Path $d 'clack.log'
  if (Test-Path $log) { Get-Content $log | Select-String -SimpleMatch 'perfdrive', 'core ready', 'first frame' | Select-Object -Last 40 | ForEach-Object { $_.Line } }
}

try {
  foreach ($s in $Sides) {
    if (-not (Test-Path (Join-Path $Out "$s\clack-perf.exe"))) { continue }
    $h = Join-Path $env:RUNNER_TEMP "scale-home-$s"
    New-Item -ItemType Directory -Force $h | Out-Null
    Copy-Item "$work\creds.json" (Join-Path $h 'creds.json')
    Seed-Home $h
    Run-Clack $s $h 'prime' 'users,exit' 900
    $warm = if ($env:CLACK_PERF_WARM_SCRIPT) { $env:CLACK_PERF_WARM_SCRIPT } else { 'startup,picker,picker-all,scroll,channels,idle' }
    Run-Clack $s $h 'warm' $warm 4200
  }
} finally {
  Stop-Process -Id $fp.Id -Force -ErrorAction SilentlyContinue
}
exit 0
