# Measures Jusnote on this machine and writes bench\<date>-<time>.md (not
# tracked: it names the hardware). One table per part, speed next to what
# was checked:
#
#   1. the machine (CPU, memory, OS, Go, WebView2 page as the selftest sees it)
#   2. backend benchmarks on synthetic notebooks of 100 / 1 000 / 5 000 notes
#   3. CLI cold start (process start to output) and peak memory, per command
#   4. the page selftest in a real window (correctness checks + latencies)
#
#   ./tools/bench.ps1            everything
#   ./tools/bench.ps1 -Quick     skip the 5 000-note backend benchmarks
param([switch]$Quick)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
Set-Location $root
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { foreach ($p in @("C:\Applications\go\bin\go.exe", "$env:ProgramFiles\Go\bin\go.exe")) { if (Test-Path $p) { $go = $p; break } } }
if (-not $go) { throw 'go not found' }
$env:PATH = (Split-Path $go) + ";$env:PATH"

$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
New-Item -ItemType Directory -Force bench | Out-Null
$out = Join-Path $root "bench\$stamp.md"
$work = Join-Path ([IO.Path]::GetTempPath()) "jusnote-bench-$stamp"
New-Item -ItemType Directory -Force $work | Out-Null
$lines = [System.Collections.Generic.List[string]]::new()
$env:JUSNOTE_STATS = "1"  # the CLI reports its own peak memory on stderr
function Say([string]$s) { $lines.Add($s); Write-Host $s }

# 1. The machine.
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$cs = Get-CimInstance Win32_ComputerSystem
$os = Get-CimInstance Win32_OperatingSystem
$version = (Get-Content VERSION -Raw).Trim()
Say "# Jusnote $version — measurements $stamp"
Say ""
Say "| machine | |"
Say "|---|---|"
Say "| CPU | $($cpu.Name.Trim()) ($($cpu.NumberOfCores) cores / $($cpu.NumberOfLogicalProcessors) threads) |"
Say "| memory | $([math]::Round($cs.TotalPhysicalMemory / 1GB, 1)) GB |"
Say "| OS | $($os.Caption) $($os.Version) |"
Say "| Go | $((& $go version) -replace '^go version ', '') |"
Say ""

# 2. Backend benchmarks.
$bench = if ($Quick) { '-bench=/notes=(100|1000)$' } else { '-bench=.' }
Say "## Backend (internal/service, synthetic notebooks, ~2 KB per note, all committed)"
Say ""
Say "| operation | notes | time / op | allocs / op | bytes / op |"
Say "|---|---:|---:|---:|---:|"
$raw = & $go test ./internal/service -run '^$' $bench -benchmem -benchtime 5x 2>&1
foreach ($l in $raw) {
    if ($l -match '^Benchmark(\w+)/notes=(\d+)-\d+\s+\d+\s+(\d+) ns/op\s+(\d+) B/op\s+(\d+) allocs/op') {
        $ms = [math]::Round([double]$Matches[3] / 1e6, 2)
        Say "| $($Matches[1]) | $($Matches[2]) | $ms ms | $($Matches[5]) | $([math]::Round([double]$Matches[4] / 1MB, 1)) MB |"
    }
}
Say ""

# 3. CLI cold start: a console build, so stdout is a pipe we can time.
$cli = Join-Path $work 'jusnote-cli.exe'
& $go build -trimpath -ldflags "-s -w -X main.version=$version" -o $cli ./cmd/jusnote
$nb = Join-Path $work 'nb'
Copy-Item -Recurse testdata\notebook $nb
& $cli init -notebook $nb | Out-Null
function TimeCli([string]$label, [string[]]$cliArgs, [int]$n = 10) {
    $ms = @(); $peak = 0; $code = 0
    for ($i = 0; $i -lt $n; $i++) {
        $psi = [Diagnostics.ProcessStartInfo]::new($cli)
        foreach ($a in $cliArgs) { $psi.ArgumentList.Add($a) }
        $psi.RedirectStandardOutput = $true; $psi.RedirectStandardError = $true; $psi.RedirectStandardInput = $true; $psi.UseShellExecute = $false
        $sw = [Diagnostics.Stopwatch]::StartNew()
        $p = [Diagnostics.Process]::Start($psi)
        $p.StandardInput.Close()
        $null = $p.StandardOutput.ReadToEnd(); $err = $p.StandardError.ReadToEnd()
        $p.WaitForExit()
        $sw.Stop()
        $ms += $sw.Elapsed.TotalMilliseconds
        if ($err -match 'jusnote-stats peak-working-set (\d+)') { $peak = [math]::Max($peak, [long]$Matches[1]) }
        $code = $p.ExitCode
    }
    $s = $ms | Sort-Object
    $p50 = [math]::Round($s[[int][math]::Floor(($s.Count - 1) / 2)], 1)
    $p95 = [math]::Round($s[[int][math]::Floor(($s.Count - 1) * 0.95)], 1)
    $peakMB = if ($peak) { [math]::Round($peak / 1MB, 1) } else { 'n/a' }
    Say "| $label | $n | $p50 ms | $p95 ms | $peakMB MB | $code |"
}
Say "## CLI cold start (process start to exit, test notebook, 10 runs each)"
Say ""
Say "| command | runs | p50 | p95 | peak memory | exit |"
Say "|---|---:|---:|---:|---:|---:|"
TimeCli 'version' @('version')
TimeCli 'help -json' @('help', '-json')
TimeCli 'list -json' @('list', '-notebook', $nb, '-json')
TimeCli 'read' @('read', 'README.md', '-notebook', $nb)
TimeCli 'search' @('search', '日志', '-notebook', $nb, '-json')
TimeCli 'status -json' @('status', '-notebook', $nb, '-json')
TimeCli 'links' @('links', 'README.md', '-notebook', $nb, '-json')
TimeCli 'write -no-commit' @('write', 'bench.md', '-text', 'x', '-no-commit', '-notebook', $nb)
TimeCli 'write + commit' @('write', 'bench.md', '-text', 'y', '-notebook', $nb) 5
Say ""

# 4. The page selftest in a real window.
Say "## Page selftest (real WebView2 window, test notebook copy)"
Say ""
& "$root\build.ps1" | Out-Null

# The process tree of a window: jusnote.exe and everything under it (the
# WebView2 browser, renderer, GPU and utility processes).
function TreeOf([int]$rootPid) {
    $all = Get-CimInstance Win32_Process -Property ProcessId, ParentProcessId, WorkingSetSize, PrivatePageCount
    $tree = @{ [uint32]$rootPid = $true }
    do {
        $grew = $false
        foreach ($p in $all) { if ($tree.ContainsKey([uint32]$p.ParentProcessId) -and -not $tree.ContainsKey([uint32]$p.ProcessId)) { $tree[[uint32]$p.ProcessId] = $true; $grew = $true } }
    } while ($grew)
    @($all | Where-Object { $tree.ContainsKey([uint32]$_.ProcessId) })
}

# Idle: the window with a note open, 10 s after it announced itself.
$inb = Join-Path $work 'idle'
Copy-Item -Recurse testdata\notebook $inb
$idata = Join-Path $work 'idle-data'
$iproc = Start-Process "$root\bin\jusnote.exe" -ArgumentList 'gui', '-notebook', $inb, '-data', $idata -PassThru
for ($i = 0; $i -lt 100 -and -not (Test-Path (Join-Path $idata 'instance.json')); $i++) { Start-Sleep -Milliseconds 100 }
Start-Sleep -Seconds 10
$idle = TreeOf $iproc.Id
$idleWS = ($idle | Measure-Object WorkingSetSize -Sum).Sum
$idlePriv = ($idle | Measure-Object PrivatePageCount -Sum).Sum
$idleApp = ($idle | Where-Object ProcessId -eq $iproc.Id | Measure-Object WorkingSetSize -Sum).Sum
foreach ($p in $idle) { Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue }

$snb = Join-Path $work 'selftest'
Copy-Item -Recurse testdata\notebook $snb
$report = Join-Path $work 'selftest.json'
$proc = Start-Process "$root\bin\jusnote.exe" -ArgumentList 'gui', '-selftest', '-notebook', $snb, '-data', (Join-Path $work 'data') -RedirectStandardOutput $report -RedirectStandardError (Join-Path $work 'selftest.err') -PassThru
# The window's memory while the selftest runs: jusnote.exe and every
# process under it (the WebView2 browser, renderer, GPU and utility
# processes), sampled every 250 ms; the peak of the sum.
$peakWS = 0; $peakPriv = 0; $peakApp = 0; $procs = 0; $samples = 0
while (-not $proc.HasExited) {
    $mine = TreeOf $proc.Id
    $ws = ($mine | Measure-Object WorkingSetSize -Sum).Sum
    $priv = ($mine | Measure-Object PrivatePageCount -Sum).Sum
    $app = ($mine | Where-Object ProcessId -eq $proc.Id | Measure-Object WorkingSetSize -Sum).Sum
    if ($ws -gt $peakWS) { $peakWS = $ws; $procs = $mine.Count }
    if ($priv -gt $peakPriv) { $peakPriv = $priv }
    if ($app -gt $peakApp) { $peakApp = $app }
    $samples++
    Start-Sleep -Milliseconds 250
}
$j = Get-Content $report -Raw | ConvertFrom-Json
Say "Environment: $($j.env.ua -replace '.*(Edg/[\d.]+).*', '$1'), DPR $($j.env.dpr), viewport $($j.env.viewport), JS heap $($j.env.jsHeapMB) MB (of $($j.env.jsHeapTotalMB) MB). $($j.note)."
Say ""
Say "| check | result |"
Say "|---|---|"
foreach ($c in $j.checks) { Say "| $($c.name) | $(if ($c.ok) { 'ok' } else { 'FAILED' }) $($c.detail) |" }
foreach ($e in $j.errors) { Say "| error | $e |" }
Say ""
Say "| timing | n | p50 | p95 | max |"
Say "|---|---:|---:|---:|---:|"
foreach ($t in $j.timings.PSObject.Properties) { Say "| $($t.Name) | $($t.Value.n) | $($t.Value.p50) ms | $($t.Value.p95) ms | $($t.Value.max) ms |" }
Say ""
$mb = { param($b) [math]::Round($b / 1MB, 1) }
Say "| window memory (jusnote.exe + WebView2) | idle, a note open ($($idle.Count) processes) | peak during the selftest ($procs processes, $samples samples) |"
Say "|---|---:|---:|"
Say "| working set | $(& $mb $idleWS) MB | $(& $mb $peakWS) MB |"
Say "| private bytes | $(& $mb $idlePriv) MB | $(& $mb $peakPriv) MB |"
Say "| jusnote.exe alone, working set | $(& $mb $idleApp) MB | $(& $mb $peakApp) MB |"
Say ""
Say "The selftest peak includes a 1 MB note, its preview (1 MB of rendered Markdown) and every kind of operation; it is a stress figure, not daily use."
# The selftest leaves one edit written but uncommitted; closing the window
# must have committed it.
$left = & $cli status -notebook $snb -json | ConvertFrom-Json
$last = & $cli show HEAD selftest/scratch.md -notebook $snb
$closed = (-not ($left | Where-Object path -like 'selftest/*')) -and (($last -join "`n") -match 'closing edit')
Say ""
Say "| check (outside the page) | result |"
Say "|---|---|"
Say "| closing the window commits the editor's last edit | $(if ($closed) { 'ok' } else { 'FAILED' }) |"
Say ""
Say "Selftest overall: $(if ($j.ok -and $closed) { 'ok' } else { 'FAILED' }), $($j.totalMs) ms."

[IO.File]::WriteAllLines($out, $lines)
Write-Host "`nwrote $out"
