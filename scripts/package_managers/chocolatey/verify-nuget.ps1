# Windows PowerShell (.NET Framework) is required by the NuGet 2 OPC reader.
param([Parameter(Mandatory = $true)][string]$Packages)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName WindowsBase
Add-Type -AssemblyName System.IO.Compression
Add-Type -AssemblyName System.IO.Compression.FileSystem
$work = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid())
New-Item -Type Directory $work | Out-Null
try {
    # Pinned, test-only official NuGet library. Modern readers can open ZIPs
    # that omit the manifest relationship and would miss this regression.
    $library = Join-Path $work 'nuget.core.2.14.0.nupkg'
    Invoke-WebRequest -UseBasicParsing -Uri 'https://api.nuget.org/v3-flatcontainer/nuget.core/2.14.0/nuget.core.2.14.0.nupkg' -OutFile $library
    $sha256 = [Security.Cryptography.SHA256]::Create()
    try {
        $hash = [BitConverter]::ToString($sha256.ComputeHash([IO.File]::ReadAllBytes($library))).Replace('-', '')
        if ($hash -ne 'cc282153f8f6b4c1270c2a3946de1ce08c18f5f5995d5735e62680e9f3489b86') {
            throw 'NuGet.Core 2.14.0 checksum mismatch'
        }
    } finally {
        $sha256.Dispose()
    }
    [IO.Compression.ZipFile]::ExtractToDirectory($library, $work)
    Add-Type -Path (Join-Path $work 'lib/net40-Client/NuGet.Core.dll')

    $inputs = @(Get-ChildItem -LiteralPath $Packages -Filter '*.nupkg' -File)
    if ($inputs.Count -lt 2) { throw 'Expected Core and Full packages' }
    foreach ($inputPackage in $inputs) {
        $path = $inputPackage.FullName
        $package = [NuGet.ZipPackage]::new($path)
        $expectedFiles = @('tools/chocolateyInstall.ps1', 'tools/chocolateyUninstall.ps1', 'tools/release.json')
        $files = @($package.GetFiles() | ForEach-Object { $_.Path.Replace('\', '/') })
        if ($inputPackage.Name -cne "$($package.Id).$($package.Version).nupkg" -or
            (Compare-Object $expectedFiles $files -CaseSensitive) -or
            $package.Id -cnotin @('mio-core', 'mio') -or
            $package.Description.Length -lt 1 -or $package.Description.Length -gt 4000 -or
            $package.ProjectUrl.Scheme -ne 'https' -or $package.LicenseUrl.Scheme -ne 'https') {
            throw "Unexpected package metadata or inventory in $path"
        }
        $opc = [IO.Packaging.Package]::Open($path, [IO.FileMode]::Open, [IO.FileAccess]::Read)
        try {
            $relationships = @($opc.GetRelationships())
            if ($relationships.Count -ne 2 -or $opc.PackageProperties.Identifier -cne $package.Id -or
                $opc.PackageProperties.Version -cne $package.Version.ToString()) {
                throw "Unexpected OPC relationships or properties in $path"
            }
            $manifest = @($opc.GetRelationshipsByType('http://schemas.microsoft.com/packaging/2010/07/manifest'))
            $stream = $opc.GetPart($manifest[0].TargetUri).GetStream()
            try {
                [NuGet.Manifest]::ReadFrom($stream, $true) | Out-Null
            } finally {
                $stream.Dispose()
            }
        } finally {
            $opc.Close()
        }

        $reference = Join-Path $work $inputPackage.BaseName
        [IO.Compression.ZipFile]::ExtractToDirectory($path, $reference)
        $nuspec = Join-Path $reference "$($package.Id).nuspec"
        Remove-Item -LiteralPath (Join-Path $reference '_rels'), (Join-Path $reference 'package') -Recurse
        Remove-Item -LiteralPath (Join-Path $reference '[Content_Types].xml')

        # Negative control with the same files and metadata. A readable ZIP
        # without its relationship part must still be rejected.
        $negative = Join-Path $work 'missing-manifest-relationship.nupkg'
        Copy-Item -LiteralPath $path -Destination $negative -Force
        $zip = [IO.Compression.ZipFile]::Open($negative, [IO.Compression.ZipArchiveMode]::Update)
        try {
            $zip.GetEntry('_rels/.rels').Delete()
        } finally {
            $zip.Dispose()
        }
        try {
            [NuGet.ZipPackage]::new($negative) | Out-Null
            throw 'NuGet accepted a package without the manifest relationship'
        } catch {
            if ($_.Exception.GetBaseException().Message -notmatch 'does not contain a manifest') { throw }
        }

        # Compare to Chocolatey's own packer using the same nuspec and tools.
        choco pack $nuspec --outputdirectory $reference --no-progress
        if ($LASTEXITCODE -ne 0) { throw "choco pack failed for $path" }
        $referencePath = Join-Path $reference $inputPackage.Name
        $zip = [IO.Compression.ZipFile]::OpenRead($referencePath)
        try {
            if (-not $zip.GetEntry('_rels/.rels') -or -not $zip.GetEntry('[Content_Types].xml') -or
                @($zip.Entries | Where-Object FullName -Match '^package/services/metadata/core-properties/[^/]+\.psmdcp$').Count -ne 1) {
                throw "Unexpected OPC layout from choco pack for $path"
            }
        } finally {
            $zip.Dispose()
        }
        Write-Host "NuGet OPC reader, nuspec schema and choco pack reference passed for $($inputPackage.Name)"
    }
} finally {
    Remove-Item -LiteralPath $work -Recurse -Force
}
