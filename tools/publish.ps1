# Prepares the public repository's next commit — one commit per release on
# top of the public history — from this private repository's HEAD, leaving
# out what stays local. Nothing is pushed unless -Push is given.
#
#   ./tools/publish.ps1                 prepare, show what would be published
#   ./tools/publish.ps1 -Push           prepare, then push to the public main
#
# Public:     what the program needs and what it takes to build and test it —
#             code, tests, synthetic fixtures, README, LICENSE, notices,
#             tools, icon sources.
# Local only: docs/ (design notes and plans: tracked here, never published),
#             and what is not tracked at all: bench/, bin/, dist/.
param(
    [switch]$Push,
    [string]$Remote = 'git@github.com:Tieumi221E/Jusnote.git',
    [string]$App = 'Jusnote'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot
Set-Location $root

# Tracked folders that must not reach the public repository.
$private = @('docs')

if (git status --porcelain) { throw 'commit or stash your changes first: the public tree is made from HEAD' }
$version = (Get-Content VERSION -Raw).Trim()
$head = (git rev-parse --short HEAD).Trim()
$work = Join-Path ([IO.Path]::GetTempPath()) "$($App.ToLower())-public-$version-$(Get-Date -Format HHmmss)"

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
    if (Test-Path -LiteralPath $f) { Remove-Item -Recurse -Force -LiteralPath $f }
}
# Nothing may point at what stays local: no private folder's path (a
# docs/ that is not part of a web address), and no name of a file in it.
$paths = @($private | ForEach-Object { '(?<![\w./-])' + [regex]::Escape("$_/") })
$names = @(git ls-files $private | ForEach-Object { Split-Path $_ -Leaf })
$files = Get-ChildItem -Recurse -File $work -Include *.md, *.go, *.ts, *.css, *.html, *.ps1, *.py, *.mjs |
    Where-Object { $_.FullName -notmatch '\\.git\\' -and $_.Name -ne 'publish.ps1' }
$leaks = @($files | Select-String -Pattern $paths) + @($files | Select-String -SimpleMatch -Pattern $names)
if ($leaks) { $leaks | ForEach-Object { Write-Host "  $_" }; throw 'the public tree still points at something local' }

Push-Location $work
git add -A
$name = git -C $root config user.name
$mail = git -C $root config user.email
git -c "user.name=$name" -c "user.email=$mail" commit --quiet -m "$App $version"
$pub = (git rev-parse --short HEAD).Trim()
git --no-pager log --oneline -3
git --no-pager show --stat --oneline HEAD | Select-Object -Last 1
if ($Push) {
    git push origin main
    if ($LASTEXITCODE) { Pop-Location; throw 'push failed' }
    Write-Host "pushed $pub. Tag the private repository:"
    Write-Host "  git tag -a v$version -m `"$App $version (public repo: $Remote, commit $pub)`" $head"
} else {
    Write-Host "prepared $pub in $work (private HEAD $head); nothing pushed. Push with:"
    Write-Host "  git -C `"$work`" push origin main"
}
Pop-Location
