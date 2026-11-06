$ErrorActionPreference = 'Stop'
$tools = Split-Path -Parent $MyInvocation.MyCommand.Definition
$record = Get-Content (Join-Path $tools 'release.json') -Raw | ConvertFrom-Json
Uninstall-BinFile -Name $record.command -Path (Join-Path $tools ('payload\bin\' + $record.command + '.exe'))
# Chocolatey removes its package directory. Never delete user profiles/projects.
