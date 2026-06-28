param(
    [string]$Version = "0.14.1",
    [string]$InstallRoot = "C:\zig-local"
)

$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

# This script downloads a local Zig toolchain for Windows builds when no system C compiler is installed.
$downloadUrl = "https://ziglang.org/download/$Version/zig-x86_64-windows-$Version.zip"
$archivePath = Join-Path $env:TEMP "zig-x86_64-windows-$Version.zip"
$extractParent = Split-Path -Parent $InstallRoot
$extractFolder = Join-Path $extractParent "zig-x86_64-windows-$Version"

Write-Host "Downloading Zig $Version from $downloadUrl"
Invoke-WebRequest -UseBasicParsing $downloadUrl -OutFile $archivePath

if (Test-Path $InstallRoot) {
    Remove-Item -Recurse -Force $InstallRoot
}

if (Test-Path $extractFolder) {
    Remove-Item -Recurse -Force $extractFolder
}

Expand-Archive -Path $archivePath -DestinationPath $extractParent -Force
Rename-Item -Path $extractFolder -NewName (Split-Path -Leaf $InstallRoot)

Write-Host "Installed Zig to $InstallRoot"
Write-Host "Verify with: & '$InstallRoot\\zig.exe' version"
