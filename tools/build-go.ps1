[CmdletBinding()]
param([switch]$Development)
$ErrorActionPreference = 'Stop'
$project = Split-Path -Parent $PSScriptRoot
$previousToken = $env:GO_MODULE_GITHUB_TOKEN
$previousPrompt = $env:GIT_TERMINAL_PROMPT
$previousInteractive = $env:GCM_INTERACTIVE
try {
    if ([string]::IsNullOrWhiteSpace($env:GO_MODULE_GITHUB_TOKEN)) {
        $env:GIT_TERMINAL_PROMPT = '0'
        $env:GCM_INTERACTIVE = 'never'
        $lines = "protocol=https`nhost=github.com`n`n" | git credential fill
        if ($LASTEXITCODE -ne 0) { throw 'GitHub credentials unavailable; authenticate Git or set GO_MODULE_GITHUB_TOKEN with repository read access.' }
        $password = $null
        foreach ($line in $lines) {
            if ($line.StartsWith('password=')) { $password = $line.Substring(9) }
        }
        if ([string]::IsNullOrWhiteSpace($password)) { throw 'GitHub credentials did not provide a token.' }
        $env:GO_MODULE_GITHUB_TOKEN = $password
    }
    Push-Location $project
    try {
        $composeArguments = @('compose', '-f', 'docker-compose.yml')
        if ($Development) { $composeArguments += @('-f', 'docker-compose.dev.yml') }
        & docker @composeArguments build monitoring_service
        if ($LASTEXITCODE -ne 0) { throw 'Monitoring-Service image build failed.' }
    } finally { Pop-Location }
} finally {
    $env:GO_MODULE_GITHUB_TOKEN = $previousToken
    $env:GIT_TERMINAL_PROMPT = $previousPrompt
    $env:GCM_INTERACTIVE = $previousInteractive
    $password = $null
    $lines = $null
}
