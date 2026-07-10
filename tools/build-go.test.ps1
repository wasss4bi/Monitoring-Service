$ErrorActionPreference = 'Stop'
$helper = Join-Path $PSScriptRoot 'build-go.ps1'
$originalToken = $env:GO_MODULE_GITHUB_TOKEN
$originalPrompt = $env:GIT_TERMINAL_PROMPT
$originalInteractive = $env:GCM_INTERACTIVE
$originalLocation = (Get-Location).Path
$previousTestState = Get-Variable -Scope Global -Name monitoring_serviceBuildHelperTestState -ErrorAction SilentlyContinue
$global:monitoring_serviceBuildHelperTestState = [pscustomobject]@{GitCalls=0;DockerCalls=@();FailBuild=$false;ExpectedToken="fixture-repository-token"}
function git {
    if (($args -join ' ') -ne 'credential fill') { throw 'Unexpected Git operation' }
    if (($input -join "`n") -notmatch 'host=github.com') { throw 'Wrong credential host' }
    if ($env:GIT_TERMINAL_PROMPT -ne '0' -or $env:GCM_INTERACTIVE -ne 'never') { throw 'Interactive credential lookup allowed' }
    $global:monitoring_serviceBuildHelperTestState.GitCalls++
    $global:LASTEXITCODE = 0
    'username=fixture-user'
    'password=fixture-repository-token'
}
function docker {
    if ($env:GO_MODULE_GITHUB_TOKEN -ne $global:monitoring_serviceBuildHelperTestState.ExpectedToken) { throw 'Build secret was not forwarded correctly' }
    $global:monitoring_serviceBuildHelperTestState.DockerCalls += ($args -join ' ')
    $global:LASTEXITCODE = $(if ($global:monitoring_serviceBuildHelperTestState.FailBuild) { 1 } else { 0 })
}
try {
    $env:GO_MODULE_GITHUB_TOKEN = $null
    & $helper
    if ($global:monitoring_serviceBuildHelperTestState.GitCalls -ne 1 -or $global:monitoring_serviceBuildHelperTestState.DockerCalls[-1] -ne 'compose -f docker-compose.yml build monitoring_service') { throw 'Production build contract changed' }
    if (-not [string]::IsNullOrEmpty($env:GO_MODULE_GITHUB_TOKEN)) { throw 'Credential remained after successful build' }
    if ((Get-Location).Path -ne $originalLocation) { throw 'Working directory was not restored' }
    $env:GO_MODULE_GITHUB_TOKEN = 'caller-provided-token'
    $global:monitoring_serviceBuildHelperTestState.ExpectedToken = 'caller-provided-token'
    & $helper -Development
    if ($global:monitoring_serviceBuildHelperTestState.GitCalls -ne 1 -or $env:GO_MODULE_GITHUB_TOKEN -ne 'caller-provided-token' -or $global:monitoring_serviceBuildHelperTestState.DockerCalls[-1] -ne 'compose -f docker-compose.yml -f docker-compose.dev.yml build monitoring_service') { throw 'Development or caller credential handling failed' }
    $env:GO_MODULE_GITHUB_TOKEN = $null
        $global:monitoring_serviceBuildHelperTestState.FailBuild = $true
    $caught = $false
    try { & $helper } catch { $caught = $true }
    if (-not $caught -or -not [string]::IsNullOrEmpty($env:GO_MODULE_GITHUB_TOKEN) -or (Get-Location).Path -ne $originalLocation) { throw 'Failed build leaked credentials or working directory' }
    if ($env:GIT_TERMINAL_PROMPT -ne $originalPrompt -or $env:GCM_INTERACTIVE -ne $originalInteractive) { throw 'Credential lookup settings were not restored' }
    Write-Output 'PASS: production/development build, cached/provided auth, failure cleanup and environment restoration'
} finally {
    $env:GO_MODULE_GITHUB_TOKEN = $originalToken
    $env:GIT_TERMINAL_PROMPT = $originalPrompt
    $env:GCM_INTERACTIVE = $originalInteractive
    Remove-Item Function:\git,Function:\docker -ErrorAction SilentlyContinue
    if ($previousTestState) { $global:monitoring_serviceBuildHelperTestState = $previousTestState.Value } else { Remove-Variable -Scope Global -Name monitoring_serviceBuildHelperTestState -ErrorAction SilentlyContinue }
    $global:LASTEXITCODE = 0
}
