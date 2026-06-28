param(
    [string]$Output = ".\dist\MiniDBStudio.exe",
    [string]$ZigRoot = "C:\zig-local"
)

$ErrorActionPreference = "Stop"

# This script builds the real Fyne desktop executable using Zig as the CGO C/C++ compiler.
$zigExe = Join-Path $ZigRoot "zig.exe"
if (-not (Test-Path $zigExe)) {
    throw "Zig compiler not found at $zigExe. Run scripts/setup-local-zig.ps1 first."
}

$outputDirectory = Split-Path -Parent $Output
if ($outputDirectory -and -not (Test-Path $outputDirectory)) {
    New-Item -ItemType Directory -Path $outputDirectory | Out-Null
}

$env:CGO_ENABLED = "1"
$env:CC = "$zigExe cc"
$env:CXX = "$zigExe c++"

Write-Host "Building MiniDB Studio desktop executable..."
go build -tags desktop -o $Output ./cmd/minidb

Write-Host "Build complete: $Output"
