$ErrorActionPreference = 'Stop'
if (-not [Environment]::Is64BitOperatingSystem) { throw 'Mio requires Windows AMD64' }
$tools = Split-Path -Parent $MyInvocation.MyCommand.Definition
$record = Get-Content (Join-Path $tools 'release.json') -Raw | ConvertFrom-Json
$archive = Join-Path $tools 'download.zip'
$stage = Join-Path $tools 'unpack'
$payload = Join-Path $tools 'payload'
try {
    Get-ChocolateyWebFile -PackageName $env:ChocolateyPackageName -FileFullPath $archive -Url64bit $record.url -Checksum64 $record.sha -ChecksumType64 'sha256'
    # Enforce integrity even if the caller disabled Chocolatey's checksum feature.
    if ((Get-FileHash $archive -Algorithm SHA256).Hash -ne $record.sha) { throw 'Mio archive SHA-256 mismatch' }
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
    Get-ChocolateyUnzip -FileFullPath $archive -Destination $stage
    $root = Join-Path $stage $record.root
    $manifest = Get-Content (Join-Path $root 'manifest.json') -Raw | ConvertFrom-Json
    if ($manifest.target -ne 'windows-amd64' -or $manifest.flavor -ne $record.flavor -or $manifest.version -ne $record.version) { throw 'Mio payload identity mismatch' }
    $files = @(Get-ChildItem $root -Recurse -File | Where-Object { $_.FullName -ne (Join-Path $root 'manifest.json') })
    if ($files.Count -ne @($manifest.files.PSObject.Properties).Count) { throw 'Mio inventory count mismatch' }
    foreach ($file in $files) {
        $relative = $file.FullName.Substring($root.Length + 1).Replace('\', '/')
        $expected = $manifest.files.PSObject.Properties[$relative].Value
        if (-not $expected -or $file.Length -ne $expected.size -or (Get-FileHash $file.FullName -Algorithm SHA256).Hash -ne $expected.sha256) { throw "Mio installed file mismatch: $relative" }
    }
    # Only this manager-owned payload is replaced; projects/config belong outside it.
    if (Test-Path $payload) { Remove-Item $payload -Recurse -Force }
    Move-Item $root $payload
    $exe = Join-Path $payload 'bin\mio.exe'
    # Prevent auto-shimming Core as mio, or accidentally shimming future tools.
    Get-ChildItem $payload -Recurse -Filter '*.exe' | ForEach-Object { New-Item ($_.FullName + '.ignore') -ItemType File -Force | Out-Null }
    Install-BinFile -Name $record.command -Path $exe
} finally {
    if (Test-Path $archive) { Remove-Item $archive -Force }
    if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
}
