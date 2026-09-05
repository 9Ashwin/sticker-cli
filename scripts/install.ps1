[CmdletBinding()]
param(
    [string]$Version = '',
    [string]$InstallDir = ''
)

$ErrorActionPreference = 'Stop'
$repo = '9Ashwin/sticker-cli'

function Fail([string]$Message) {
    throw "sticker install: $Message"
}

if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    if ([string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        $InstallDir = Join-Path $HOME '.local\bin'
    } else {
        $InstallDir = Join-Path $env:LOCALAPPDATA 'sticker\bin'
    }
}
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = (Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest").tag_name
}
$Version = "v$($Version.TrimStart('v'))"
if ($Version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$') {
    Fail 'version must be a release version such as v1.2.3'
}

$asset = 'sticker_windows_amd64.zip'
$baseUrl = "https://github.com/$repo/releases/download/$Version"
$tempRoot = Join-Path ([IO.Path]::GetTempPath()) ("sticker-install-" + [Guid]::NewGuid().ToString('N'))
$temporary = $null
New-Item -ItemType Directory -Path $tempRoot | Out-Null
try {
    $archive = Join-Path $tempRoot $asset
    $releaseChecksums = Join-Path $tempRoot 'checksums.txt'
    Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/$asset" -OutFile $archive
    Invoke-WebRequest -UseBasicParsing -Uri "$baseUrl/checksums.txt" -OutFile $releaseChecksums

    $expected = $null
    foreach ($line in Get-Content -LiteralPath $releaseChecksums) {
        $parts = $line -split '\s+'
        if ($parts.Count -ge 2 -and (($parts[1] -eq $asset) -or ($parts[1] -eq "*$asset"))) {
            $expected = $parts[0].ToLowerInvariant()
            break
        }
    }
    if ($expected -notmatch '^[0-9a-f]{64}$') { Fail "release checksum is missing for $asset" }
    $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash.ToLowerInvariant()
    if ($actual -ne $expected) { Fail 'release checksum verification failed' }

    $zip = [System.IO.Compression.ZipFile]::OpenRead($archive)
    try {
        $allowed = @('sticker.exe', 'LICENSE', 'VERSION', 'checksums.txt')
        foreach ($entry in $zip.Entries) {
            if ($entry.FullName -notin $allowed) { Fail "release archive contains an unexpected path: $($entry.FullName)" }
        }
    } finally {
        $zip.Dispose()
    }
    $unpacked = Join-Path $tempRoot 'unpacked'
    Expand-Archive -LiteralPath $archive -DestinationPath $unpacked -Force
    $binary = Join-Path $unpacked 'sticker.exe'
    $innerChecksums = Join-Path $unpacked 'checksums.txt'
    if (-not (Test-Path -LiteralPath $binary) -or -not (Test-Path -LiteralPath $innerChecksums)) { Fail 'release archive is incomplete' }

    $expectedBinary = $null
    foreach ($line in Get-Content -LiteralPath $innerChecksums) {
        $parts = $line -split '\s+'
        if ($parts.Count -ge 2 -and (($parts[1] -eq 'sticker.exe') -or ($parts[1] -eq '*sticker.exe'))) {
            $expectedBinary = $parts[0].ToLowerInvariant()
            break
        }
    }
    if ($expectedBinary -notmatch '^[0-9a-f]{64}$') { Fail 'binary checksum is missing from the archive' }
    $actualBinary = (Get-FileHash -Algorithm SHA256 -LiteralPath $binary).Hash.ToLowerInvariant()
    if ($actualBinary -ne $expectedBinary) { Fail 'binary checksum verification failed' }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $temporary = Join-Path $InstallDir ('.sticker-install-' + $PID + '.exe')
    Copy-Item -LiteralPath $binary -Destination $temporary -Force
    Move-Item -LiteralPath $temporary -Destination (Join-Path $InstallDir 'sticker.exe') -Force
    Write-Output "installed sticker $Version to $(Join-Path $InstallDir 'sticker.exe')"
    if ((';{0};' -f $env:PATH) -notlike "*;$InstallDir;*") {
        Write-Output "add $InstallDir to PATH to run sticker directly"
    }
} finally {
    if ($null -ne $temporary) {
        Remove-Item -LiteralPath $temporary -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $tempRoot -Recurse -Force -ErrorAction SilentlyContinue
}
