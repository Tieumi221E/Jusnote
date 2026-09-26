# Builds bin\jusnote.exe: the editor page bundle, then the Go program.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) {
    foreach ($p in @("C:\Applications\go\bin\go.exe", "$env:ProgramFiles\Go\bin\go.exe", "$env:LOCALAPPDATA\Programs\Go\bin\go.exe")) {
        if (Test-Path -LiteralPath $p) { $go = $p; break }
    }
}
if (-not $go) { throw 'go not found' }
if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { throw 'npm not found' }
npm --prefix web run build
if ($LASTEXITCODE) { exit $LASTEXITCODE }
$version = (Get-Content VERSION -Raw).Trim()
& $go build -trimpath -ldflags "-s -w -H=windowsgui -X main.version=$version" -o bin\jusnote.exe ./cmd/jusnote
exit $LASTEXITCODE
