[CmdletBinding()]
param(
    [ValidateSet('install', 'start', 'stop', 'status')]
    [string]$Task = 'status'
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent $PSScriptRoot
$EnvFile = Join-Path $Root '.env'
$ToolsDir = Join-Path $Root '.tools'
$RuntimeDir = Join-Path $Root '.runtime'
$ArchiveName = 'postgres-18.4-pgvector-0.8.3-win32-x64.zip'
$Archive = Join-Path $ToolsDir $ArchiveName
$PackageDir = Join-Path $ToolsDir 'postgres\win32-x64'
$DataDir = Join-Path $RuntimeDir 'postgres-data'
$LogFile = Join-Path $RuntimeDir 'postgres.log'
$ExpectedSha256 = 'dd3f5f30813b7ecc1adce3fb03ef9c138d082d43c8f55c89346033609b8d0d49'
$ReleaseUrl = 'https://github.com/YukeonWayne/pg_pgvector_binary/releases/download/v18.4-pgvector0.8.3-win32-x64/' + $ArchiveName
$DomesticProxyUrl = 'https://ghproxy.net/' + $ReleaseUrl

function Read-DotEnv {
    if (-not (Test-Path -LiteralPath $EnvFile)) {
        throw 'Missing .env. Run ./dev.ps1 init and replace the example credentials first.'
    }
    $values = @{}
    foreach ($line in Get-Content -LiteralPath $EnvFile) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#')) { continue }
        $separator = $trimmed.IndexOf('=')
        if ($separator -lt 1) { continue }
        $name = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim().Trim('"').Trim("'")
        $values[$name] = $value
    }
    return $values
}

function Pg-Bin([string]$Name) {
    $path = Join-Path $PackageDir ('bin\' + $Name)
    if (-not (Test-Path -LiteralPath $path)) {
        throw "PostgreSQL binary is missing: $path"
    }
    return $path
}

function Install-NativePostgres {
    New-Item -ItemType Directory -Path $ToolsDir, $RuntimeDir -Force | Out-Null
    $needsDownload = -not (Test-Path -LiteralPath $Archive)
    if (-not $needsDownload) {
        $actual = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
        $needsDownload = $actual -ne $ExpectedSha256
    }
    if ($needsDownload) {
        Write-Host 'Downloading PostgreSQL 18.4 + pgvector 0.8.3 from the configured domestic proxy...'
        Invoke-WebRequest -Uri $DomesticProxyUrl -OutFile $Archive -UseBasicParsing
    }
    $actual = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actual -ne $ExpectedSha256) {
        throw "PostgreSQL archive checksum mismatch. Expected $ExpectedSha256, got $actual."
    }
    if (-not (Test-Path -LiteralPath (Join-Path $PackageDir 'bin\postgres.exe'))) {
        $extractDir = Join-Path $ToolsDir 'postgres'
        Expand-Archive -LiteralPath $Archive -DestinationPath $extractDir -Force
    }

    if (-not (Test-Path -LiteralPath (Join-Path $DataDir 'PG_VERSION'))) {
        $envValues = Read-DotEnv
        $password = $envValues['POSTGRES_PASSWORD']
        if (-not $password) { throw 'POSTGRES_PASSWORD is missing from .env.' }
        $passwordFile = Join-Path $RuntimeDir 'pg-password.txt'
        try {
            Set-Content -LiteralPath $passwordFile -Value $password -NoNewline -Encoding ascii
            & (Pg-Bin 'initdb.exe') --pgdata=$DataDir --username=$($envValues['POSTGRES_USER']) --pwfile=$passwordFile --encoding=UTF8 --locale=C --auth-host=scram-sha-256 --auth-local=trust
            if ($LASTEXITCODE -ne 0) { throw "initdb failed with exit code $LASTEXITCODE" }
        } finally {
            Remove-Item -LiteralPath $passwordFile -Force -ErrorAction SilentlyContinue
        }
    }
    Write-Host 'Native PostgreSQL runtime is installed and initialized.'
}

function Start-NativePostgres {
    Install-NativePostgres
    & (Pg-Bin 'pg_ctl.exe') status -D $DataDir *> $null
    if ($LASTEXITCODE -ne 0) {
        & (Pg-Bin 'pg_ctl.exe') start -D $DataDir -l $LogFile -w -t 30
        if ($LASTEXITCODE -ne 0) { throw 'PostgreSQL failed to start.' }
    }
    $envValues = Read-DotEnv
    $env:PGPASSWORD = $envValues['POSTGRES_PASSWORD']
    $user = $envValues['POSTGRES_USER']
    $database = $envValues['POSTGRES_DB']
    $port = $envValues['POSTGRES_PORT']
    $exists = ((& (Pg-Bin 'psql.exe') -h 127.0.0.1 -p $port -U $user -d postgres -t -A -c "SELECT 1 FROM pg_database WHERE datname='$database'") | Out-String).Trim()
    if ($LASTEXITCODE -ne 0) { throw 'Unable to query PostgreSQL.' }
    if ($exists -ne '1') {
        & (Pg-Bin 'createdb.exe') -h 127.0.0.1 -p $port -U $user $database
        if ($LASTEXITCODE -ne 0) { throw "Unable to create database $database." }
    }
    & (Pg-Bin 'psql.exe') -h 127.0.0.1 -p $port -U $user -d $database -v ON_ERROR_STOP=1 -c 'CREATE EXTENSION IF NOT EXISTS vector;' *> $null
    if ($LASTEXITCODE -ne 0) { throw 'Unable to enable pgvector.' }
    Write-Host "PostgreSQL is ready on 127.0.0.1:$port (database: $database)."
}

switch ($Task) {
    'install' { Install-NativePostgres }
    'start' { Start-NativePostgres }
    'stop' {
        if (Test-Path -LiteralPath (Join-Path $PackageDir 'bin\pg_ctl.exe')) {
            & (Pg-Bin 'pg_ctl.exe') stop -D $DataDir -m fast -w
        }
    }
    'status' {
        if (-not (Test-Path -LiteralPath (Join-Path $PackageDir 'bin\pg_ctl.exe'))) {
            Write-Host 'Native PostgreSQL is not installed.'
            exit 1
        }
        & (Pg-Bin 'pg_ctl.exe') status -D $DataDir
    }
}
