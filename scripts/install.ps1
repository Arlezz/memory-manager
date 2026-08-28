<#
.SYNOPSIS
  Installs memory-manager and wires it into Claude Code's SessionStart hook.

.DESCRIPTION
  Downloads the release binary for this platform, places it under
  ~/.claude/memory-manager/bin, and adds a SessionStart hook to settings.json.

  The settings file is merged, never replaced: it already holds the user's model,
  theme and plugin configuration, and overwriting it would be a silent
  destructive change.
#>
[CmdletBinding()]
param(
    # Release tag to install. "latest" resolves through the GitHub API.
    [string]$Version = "latest",

    # Install a binary already built locally instead of downloading one.
    [string]$FromPath,

    # Skip editing settings.json; only install the binary.
    [switch]$NoHook
)

$ErrorActionPreference = "Stop"

$Repo = "Arlezz/memory-manager"

function Get-ClaudeRoot {
    if ($env:CLAUDE_CONFIG_DIR) { return $env:CLAUDE_CONFIG_DIR }
    return (Join-Path $HOME ".claude")
}

function Get-TargetTriple {
    $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { "amd64" }
        "ARM64" { "arm64" }
        default { throw "unsupported architecture: $($env:PROCESSOR_ARCHITECTURE)" }
    }
    return "windows_$arch"
}

$claudeRoot = Get-ClaudeRoot
$binDir = Join-Path $claudeRoot "memory-manager\bin"
$binPath = Join-Path $binDir "memory-manager.exe"

New-Item -ItemType Directory -Force -Path $binDir | Out-Null

if ($FromPath) {
    if (-not (Test-Path $FromPath)) { throw "no such file: $FromPath" }
    Copy-Item -Path $FromPath -Destination $binPath -Force
    Write-Host "Installed $FromPath -> $binPath"
}
else {
    $triple = Get-TargetTriple
    $asset = "memory-manager_$triple.exe"
    if ($Version -eq "latest") {
        $url = "https://github.com/$Repo/releases/latest/download/$asset"
    }
    else {
        $url = "https://github.com/$Repo/releases/download/$Version/$asset"
    }
    Write-Host "Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $binPath -UseBasicParsing
    Write-Host "Installed -> $binPath"
}

& $binPath version

if ($NoHook) {
    Write-Host "Skipped the hooks. Add them yourself:"
    Write-Host "  SessionStart -> `"$binPath`" sync -quiet"
    Write-Host "  SessionEnd   -> `"$binPath`" push -quiet"
    exit 0
}

$settingsPath = Join-Path $claudeRoot "settings.json"

# SessionStart pulls both layers in; SessionEnd sends back what the session wrote.
$hookCommands = [ordered]@{
    SessionStart = "`"$binPath`" sync -quiet"
    SessionEnd   = "`"$binPath`" push -quiet"
}

# Read the existing settings, preserving every key we do not touch.
if (Test-Path $settingsPath) {
    $raw = Get-Content -Raw -Path $settingsPath
    try {
        $settings = $raw | ConvertFrom-Json -AsHashtable
    }
    catch {
        throw "settings.json is not valid JSON; fix it before installing so nothing is lost: $settingsPath"
    }
    # Keep a copy: this is the file that controls the user's whole CLI.
    $backup = "$settingsPath.memory-manager-backup"
    Set-Content -Path $backup -Value $raw -Encoding utf8
    Write-Host "Backed up settings to $backup"
}
else {
    $settings = @{}
}

if (-not $settings.ContainsKey("hooks")) { $settings["hooks"] = @{} }

foreach ($event in $hookCommands.Keys) {
    $command = $hookCommands[$event]
    if (-not $settings["hooks"].ContainsKey($event)) { $settings["hooks"][$event] = @() }

    # Replace only our own entry, so re-running the installer is idempotent and
    # other hooks on the same event (the caveman plugin, for one) survive.
    $existing = @($settings["hooks"][$event] | Where-Object {
        $entry = $_
        -not ($entry.hooks | Where-Object { $_.command -like "*memory-manager*" })
    })

    $entry = @{
        hooks = @(
            @{
                type    = "command"
                command = $command
            }
        )
    }

    $settings["hooks"][$event] = @($existing + $entry)
    Write-Host "Wired $event hook: $command"
}

$settings | ConvertTo-Json -Depth 12 | Set-Content -Path $settingsPath -Encoding utf8
Write-Host ""
Write-Host "Next:"
Write-Host "  1. memory-manager config -personal-repo <your private memory repo URL>"
Write-Host "  2. memory-manager migrate           # review the plan"
Write-Host "  3. memory-manager migrate -apply    # adopt the existing memory"
