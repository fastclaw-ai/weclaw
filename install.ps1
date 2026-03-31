#Requires -Version 5.1
<#
.SYNOPSIS
    Install weclaw on Windows.
.DESCRIPTION
    Downloads the latest weclaw release from GitHub and installs it
    to $env:LOCALAPPDATA\weclaw. Adds the directory to the user PATH
    if not already present.
#>

$ErrorActionPreference = "Stop"

$Repo = "fastclaw-ai/weclaw"
$Binary = "weclaw"
$InstallDir = "$env:LOCALAPPDATA\weclaw"

# Detect architecture
$Arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { Write-Error "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE"; exit 1 }
}

Write-Host "Detected: windows/$Arch"

# Get latest version via redirect
Write-Host "Fetching latest release..."
try {
    $Response = Invoke-WebRequest -Uri "https://github.com/$Repo/releases/latest" `
        -MaximumRedirection 0 -ErrorAction SilentlyContinue -UseBasicParsing
} catch {
    $Response = $_.Exception.Response
}

$Location = if ($Response.Headers["Location"]) {
    $Response.Headers["Location"]
} elseif ($Response.Headers.Location) {
    $Response.Headers.Location
} else {
    $null
}

if (-not $Location) {
    Write-Error "Could not determine latest version. Is there a release on GitHub?"
    exit 1
}

$Version = ($Location -split "/tag/")[-1].Trim()
Write-Host "Latest version: $Version"

# Download
$Filename = "${Binary}_windows_${Arch}.exe"
$Url = "https://github.com/$Repo/releases/download/$Version/$Filename"

Write-Host "Downloading $Url..."
$TmpFile = Join-Path $env:TEMP $Filename

Invoke-WebRequest -Uri $Url -OutFile $TmpFile -UseBasicParsing

# Install
if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
}

$DestPath = Join-Path $InstallDir "$Binary.exe"
Move-Item -Path $TmpFile -Destination $DestPath -Force

# Add to PATH if not already present
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    Write-Host ""
    Write-Host "Added $InstallDir to user PATH."
    Write-Host "Please restart your terminal for PATH changes to take effect."
}

Write-Host ""
Write-Host "weclaw $Version installed to $DestPath"
Write-Host ""
Write-Host "Get started:"
Write-Host "  weclaw start"
