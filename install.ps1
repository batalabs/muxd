# muxd installer for Windows
# Usage: irm https://raw.githubusercontent.com/batalabs/muxd/main/install.ps1 | iex

$ErrorActionPreference = "Stop"

$repo = "batalabs/muxd"
$installDir = "$env:LOCALAPPDATA\muxd"

# Detect architecture
$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") { "arm64" } else { "amd64" }
} else {
    Write-Error "32-bit Windows is not supported."
    exit 1
}

$bin = "muxd-windows-$arch.exe"

# Get latest version
if ($env:MUXD_VERSION) {
    $version = $env:MUXD_VERSION
} else {
    $release = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
    $version = $release.tag_name
    if (-not $version) {
        Write-Error "Failed to fetch latest version."
        exit 1
    }
}

$url = "https://github.com/$repo/releases/download/$version/$bin"

Write-Host "Installing muxd $version (windows/$arch)..."
Write-Host "  from: $url"
Write-Host "  to:   $installDir\muxd.exe"

# Create install directory
if (-not (Test-Path $installDir)) {
    New-Item -ItemType Directory -Path $installDir -Force | Out-Null
}

# Download
$tmp = Join-Path $env:TEMP "muxd-download.exe"
Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing

# Move to install directory
Move-Item -Path $tmp -Destination "$installDir\muxd.exe" -Force

# Add to PATH if not already there
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$installDir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$installDir", "User")
    Write-Host "  Added $installDir to PATH (restart your terminal to use 'muxd')"
}

Write-Host ""
Write-Host "muxd $version installed successfully!"
Write-Host "Run 'muxd' to get started."
