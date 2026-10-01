# Installs mmt.exe into %LOCALAPPDATA%\Programs\mmt and adds it to your PATH.
$ErrorActionPreference = 'Stop'
$dest = Join-Path $env:LOCALAPPDATA 'Programs\mmt'
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Copy-Item -Force (Join-Path $PSScriptRoot 'mmt.exe') $dest
# files from a browser or chat app carry a "downloaded" mark; this build is not signed
Unblock-File (Join-Path $dest 'mmt.exe')
$path = [Environment]::GetEnvironmentVariable('Path', 'User')
if (-not (($path -split ';') -contains $dest)) {
    [Environment]::SetEnvironmentVariable('Path', (($path.TrimEnd(';') + ';' + $dest).TrimStart(';')), 'User')
    Write-Host "Added $dest to your PATH. Open a new terminal window before running mmt."
}
Write-Host "Installed: $(& (Join-Path $dest 'mmt.exe') --version)"
Write-Host "Next: mmt login"
