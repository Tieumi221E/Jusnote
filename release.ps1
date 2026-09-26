# Builds a release: runs the tests, builds bin\jusnote.exe, and packs
# dist\Jusnote-<version>-windows-x64.zip (the exe, README, LICENSE and the
# third-party notices; nothing from this machine's data) with its SHA-256.
$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot
# Go from PATH, or a usual install folder; a clear stop here rather than a
# silent failure later.
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) {
    foreach ($p in @("C:\Applications\go\bin\go.exe", "$env:ProgramFiles\Go\bin\go.exe", "$env:LOCALAPPDATA\Programs\Go\bin\go.exe")) {
        if (Test-Path -LiteralPath $p) { $go = $p; break }
    }
}
if (-not $go) { throw 'go not found' }
$env:PATH = (Split-Path $go) + ";$env:PATH"
$version = (Get-Content VERSION -Raw).Trim()

& $go vet ./...
if ($LASTEXITCODE) { exit $LASTEXITCODE }
& $go test ./...
if ($LASTEXITCODE) { exit $LASTEXITCODE }
npm --prefix web run check
if ($LASTEXITCODE) { exit $LASTEXITCODE }
npm --prefix web test
if ($LASTEXITCODE) { exit $LASTEXITCODE }
& "$PSScriptRoot\build.ps1"
if ($LASTEXITCODE) { exit $LASTEXITCODE }
# A GUI-subsystem exe: its output reaches PowerShell only through a file.
$out = New-TemporaryFile
Start-Process -FilePath "$PSScriptRoot\bin\jusnote.exe" -ArgumentList '-version' -Wait -NoNewWindow -RedirectStandardOutput $out
$v = (Get-Content $out -Raw).Trim()
Remove-Item $out
if ($v -ne "jusnote $version") { throw "the exe says '$v', expected 'jusnote $version'" }

$name = "Jusnote-$version-windows-x64"
$stage = Join-Path $PSScriptRoot "dist\$name"
if (Test-Path $stage) { Remove-Item -Recurse -Force $stage }
New-Item -ItemType Directory -Force $stage | Out-Null
Copy-Item bin\jusnote.exe, README.md, LICENSE, THIRD_PARTY_NOTICES.txt $stage
$zip = Join-Path $PSScriptRoot "dist\$name.zip"
if (Test-Path $zip) { Remove-Item -Force $zip }
Compress-Archive -Path "$stage\*" -DestinationPath $zip -CompressionLevel Optimal
$hash = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
"$hash  $name.zip" | Set-Content -NoNewline -Encoding ascii "$zip.sha256"
Get-ChildItem $stage | Format-Table Name, Length -AutoSize | Out-String
"{0}: {1:N0} bytes, sha256 {2}" -f "$name.zip", (Get-Item $zip).Length, $hash
