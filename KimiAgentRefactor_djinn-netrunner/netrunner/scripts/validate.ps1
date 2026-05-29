#!/usr/bin/env pwsh
[CmdletBinding()]
param(
    [switch]$SkipVulnCheck
)

$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent $PSScriptRoot
$backendDir = Join-Path $repoRoot "backend"

Write-Host "[validate] backend dir: $backendDir"
Push-Location $backendDir
try {
    Write-Host "[validate] go vet ./..."
    go vet ./...

    Write-Host "[validate] go test ./..."
    go test ./...

    Write-Host "[validate] go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent"
    go build ./cmd/server ./cmd/worker ./cmd/cli ./cmd/agent

    if (-not $SkipVulnCheck) {
        if (Get-Command govulncheck -ErrorAction SilentlyContinue) {
            Write-Host "[validate] govulncheck ./..."
            govulncheck ./...
        } else {
            Write-Warning "govulncheck is not installed. Install with: go install golang.org/x/vuln/cmd/govulncheck@latest"
        }
    }

    Write-Host "[validate] success"
}
finally {
    Pop-Location
}
