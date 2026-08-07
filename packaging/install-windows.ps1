<#
.SYNOPSIS
    Locksmith installer for Windows
.DESCRIPTION
    Downloads and installs Locksmith - Serbian ID Card Middleware
    Requires: Smart card reader driver + PC/SC service
.NOTES
    Run as Administrator: Right-click PowerShell → Run as Administrator
#>
param(
    [string]$Version = "v1.0.0",
    [string]$InstallDir = "$env:LOCALAPPDATA\Locksmith"
)

$ErrorActionPreference = "Stop"

Write-Host ""
Write-Host "🔐 Locksmith Installer (Windows)" -ForegroundColor Cyan
Write-Host "================================" -ForegroundColor Cyan
Write-Host ""

# Check PC/SC service
$pcscService = Get-Service -Name "SCardSvr" -ErrorAction SilentlyContinue
if (-not $pcscService) {
    Write-Host "❌ Smart Card service (SCardSvr) not found." -ForegroundColor Red
    Write-Host "   Please install your card reader driver first." -ForegroundColor Yellow
    exit 1
}
if ($pcscService.Status -ne "Running") {
    Write-Host "⚠️  Starting Smart Card service..." -ForegroundColor Yellow
    Start-Service "SCardSvr" -ErrorAction SilentlyContinue
}

# Create install directory
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

# Download
$asset = "locksmith-windows-amd64"
$url = "https://github.com/milentijev1c/locksmith/releases/download/$Version/$asset.zip"
$tmpZip = "$env:TEMP\locksmith-windows.zip"

Write-Host "📥 Downloading $asset.zip..." -ForegroundColor Cyan
Invoke-WebRequest -Uri $url -OutFile $tmpZip -UseBasicParsing

# Extract
Write-Host "📦 Extracting..." -ForegroundColor Cyan
Expand-Archive -Path $tmpZip -DestinationPath $InstallDir -Force
Remove-Item $tmpZip -Force

# Add to PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$InstallDir", "User")
    $env:Path += ";$InstallDir"
    Write-Host "📁 Added to PATH" -ForegroundColor Green
}

# Create Start Menu shortcut
$startMenu = "$env:APPDATA\Microsoft\Windows\Start Menu\Programs"
$shortcutPath = "$startMenu\Locksmith.lnk"
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = "$InstallDir\locksmith.exe"
$shortcut.WorkingDirectory = $InstallDir
$shortcut.Description = "Serbian ID Card Middleware"
$shortcut.Save()

Write-Host "✅ Locksmith installed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "Installed to: $InstallDir\locksmith.exe" -ForegroundColor DarkGray
Write-Host "Start Menu:   Locksmith" -ForegroundColor DarkGray
Write-Host ""
Write-Host "Usage:" -ForegroundColor White
Write-Host "  1. Insert your Serbian ID card into a card reader" -ForegroundColor White
Write-Host "  2. Open http://127.0.0.1:19711/ in your browser" -ForegroundColor White
Write-Host "  3. Upload a PDF, enter your PIN, and sign" -ForegroundColor White
Write-Host ""
Write-Host "Start now: locksmith" -ForegroundColor Yellow