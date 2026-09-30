<#
.SYNOPSIS
  Checks prerequisites and starts Mediarium natively on Windows.

.DESCRIPTION
  Downloads and installs NOTHING. It only:
    1. looks for 7z.exe (only needed for .7z archives) and par2.exe (needed to
       repair Usenet downloads), and prints what to do if one is missing;
    2. sets the folder environment variables Mediarium reads
       (CONFIG_DIR, DOWNLOADS_DIR, MOVIES_DIR, TV_DIR, APP_PORT);
    3. starts mediarium.exe from the same folder as this script.

  STATUS: run once on Windows 11 with Windows PowerShell 5.1 against a CGO-free
  windows/amd64 build: it parsed cleanly, found 7z.exe in Program Files (which
  was NOT on PATH), reported par2.exe missing, created the folders, started the
  app, and /api/version answered. NOT tested: the path where par2 is present,
  PowerShell 7, or running it from a folder with spaces.

.PARAMETER DataRoot
  Parent folder for everything. Default: %USERPROFILE%\Mediarium.
  Keep downloads, movies and tv under ONE drive so imports can hardlink
  (NTFS only; a second drive means files are copied instead of hardlinked).

.PARAMETER Port
  Web UI port. Default 8264.

.EXAMPLE
  powershell -ExecutionPolicy Bypass -File .\run-mediarium.ps1
  powershell -ExecutionPolicy Bypass -File .\run-mediarium.ps1 -DataRoot D:\Media -Port 8181
#>
[CmdletBinding()]
param(
    [string]$DataRoot = (Join-Path $env:USERPROFILE 'Mediarium'),
    [int]$Port = 8264
)

$ErrorActionPreference = 'Stop'
$here = Split-Path -Parent $MyInvocation.MyCommand.Path
$exe = Join-Path $here 'mediarium.exe'

if (-not (Test-Path $exe)) {
    Write-Error "Could not find mediarium.exe next to this script ($here)."
    exit 1
}

# ---- 7z ---------------------------------------------------------------------
# The 7-Zip installer does not add itself to PATH, so also look in the default
# install folders and, if found there, add that folder to PATH for this run only.
$sevenZip = Get-Command 7z -ErrorAction SilentlyContinue
if (-not $sevenZip) {
    foreach ($dir in @("$env:ProgramFiles\7-Zip", "${env:ProgramFiles(x86)}\7-Zip")) {
        if ($dir -and (Test-Path (Join-Path $dir '7z.exe'))) {
            $env:PATH = "$dir;$env:PATH"
            Write-Host "Found 7z.exe in $dir and added it to PATH for this session." -ForegroundColor Yellow
            Write-Host "To make that permanent, add it to your user PATH (Settings > System > About > Advanced system settings > Environment Variables)." -ForegroundColor Yellow
            $sevenZip = Get-Command 7z -ErrorAction SilentlyContinue
            break
        }
    }
}
if (-not $sevenZip) {
    Write-Host ''
    Write-Host 'MISSING: 7z.exe (only needed for .7z archives; RAR and ZIP are unpacked by Mediarium itself).' -ForegroundColor Red
    Write-Host '  Install:   winget install 7zip.7zip'
    Write-Host '  Then add   C:\Program Files\7-Zip   to your PATH and re-run this script.'
}

# ---- par2 -------------------------------------------------------------------
$par2 = Get-Command par2 -ErrorAction SilentlyContinue
if (-not $par2) {
    Write-Host ''
    Write-Host 'MISSING: par2.exe (needed to repair Usenet downloads with missing articles).' -ForegroundColor Red
    Write-Host '  No winget package for par2cmdline was found when this was written.'
    Write-Host '  Download a Windows build of par2cmdline (or par2cmdline-turbo) from its'
    Write-Host '  GitHub releases page, unzip it, and put the folder containing par2.exe on your PATH.'
    Write-Host '  Check it works with:  par2 --version'
    Write-Host '  Without it, Usenet downloads that need repair will fail; torrents are unaffected.'
}

if (-not $sevenZip -or -not $par2) {
    Write-Host ''
    Write-Host 'Starting anyway. Fix the items above and restart Mediarium when ready.' -ForegroundColor Yellow
    Write-Host ''
}

# ---- folders ----------------------------------------------------------------
$env:CONFIG_DIR    = Join-Path $DataRoot 'config'
$env:DOWNLOADS_DIR = Join-Path $DataRoot 'downloads'
$env:MOVIES_DIR    = Join-Path $DataRoot 'movies'
$env:TV_DIR        = Join-Path $DataRoot 'tv'
$env:APP_PORT      = "$Port"

foreach ($d in @($env:CONFIG_DIR, $env:DOWNLOADS_DIR, $env:MOVIES_DIR, $env:TV_DIR)) {
    New-Item -ItemType Directory -Force -Path $d | Out-Null
}

Write-Host "Config     : $env:CONFIG_DIR   (back this up: app.db + secret.key)"
Write-Host "Downloads  : $env:DOWNLOADS_DIR"
Write-Host "Movies     : $env:MOVIES_DIR"
Write-Host "TV         : $env:TV_DIR"
Write-Host "Web UI     : http://localhost:$Port"
Write-Host 'Windows Firewall may ask whether to allow Mediarium on your network; that is only needed to reach it from other devices.'
Write-Host 'Press Ctrl+C to stop.'
Write-Host ''

& $exe
exit $LASTEXITCODE
