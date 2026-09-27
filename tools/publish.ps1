# Prepares the public repository's next commit — one commit per release on
# top of the public history — from this private repository's HEAD, leaving
# out what stays local. Nothing is pushed unless -Push is given.
#
#   ./tools/publish.ps1                 prepare, show what would be published
#   ./tools/publish.ps1 -Push           prepare, then push to the public main
#
# Public:     code, tests, synthetic fixtures, README, LICENSE, notices,
#             docs/architecture.md, tools, icon sources.
# Local only: docs/plan.md (tracked here, never published), and what is not
#             tracked at all: docs/research-log/, bench/, bin/, dist/.
param(
    [switch]$Push,
    [string]$Remote = 'git@github.com:Tieumi221E/Jusnote.git'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
Set-Location $root

# Tracked files that must not reach the public repository.
$private = @('docs/plan.md')

if (git status --porcelain) { throw 'commit or stash your changes first: the public tree is made from HEAD' }
$version = (Get-Content VERSION -Raw).Trim()
$head = (git rev-parse --short HEAD).Trim()
$work = Join-Path ([IO.Path]::GetTempPath()) "jusnote-public-$version-$(Get-Date -Format HHmmss)"

git clone --quiet $Remote $work
if ($LASTEXITCODE) { throw "clone of $Remote failed" }
# Empty the worktree (keep .git), then lay HEAD's files in.
Get-ChildItem -Force $work | Where-Object Name -ne '.git' | ForEach-Object { Remove-Item -Recurse -Force -LiteralPath $_.FullName }
$tar = "$work.tar"
git archive --format=tar -o $tar HEAD
tar -xf $tar -C $work
Remove-Item -LiteralPath $tar
foreach ($p in $private) {
    $f = Join-Path $work $p
    if (Test-Path -LiteralPath $f) { Remove-Item -Force -LiteralPath $f }
}
# Nothing that links to a private file may remain.
$leaks = Get-ChildItem -Recurse -File $work -Include *.md, *.go, *.ts, *.ps1 | Where-Object { $_.FullName -notmatch '\\.git\\' -and $_.Name -ne 'publish.ps1' } |
    Select-String -SimpleMatch -Pattern $private
if ($leaks) { $leaks | ForEach-Object { Write-Host "  $_" }; throw 'the public tree still mentions a private file' }

Push-Location $work
git add -A
$name = git -C $root config user.name
$mail = git -C $root config user.email
git -c "user.name=$name" -c "user.email=$mail" commit --quiet -m "Jusnote $version"
$pub = (git rev-parse --short HEAD).Trim()
git --no-pager log --oneline -3
git --no-pager show --stat --oneline HEAD | Select-Object -Last 1
if ($Push) {
    git push origin main
    if ($LASTEXITCODE) { Pop-Location; throw 'push failed' }
    Write-Host "pushed $pub. Tag the private repository:"
    Write-Host "  git tag -a v$version -m `"Jusnote $version (public repo: github.com/Tieumi221E/Jusnote, commit $pub)`" $head"
} else {
    Write-Host "prepared $pub in $work (private HEAD $head); nothing pushed. Push with:"
    Write-Host "  git -C `"$work`" push origin main"
}
Pop-Location
