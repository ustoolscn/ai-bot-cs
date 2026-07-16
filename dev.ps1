[CmdletBinding()]
param(
    [ValidateSet('init', 'doctor', 'infra-up', 'infra-down', 'infra-logs', 'native-db-install', 'native-db-start', 'native-db-stop', 'native-db-status', 'backend', 'frontend')]
    [string]$Task = 'doctor'
)

$ErrorActionPreference = 'Stop'
$Root = $PSScriptRoot
$EnvFile = Join-Path $Root '.env'
$ComposeFile = Join-Path $Root 'deploy/docker-compose.dev-db.yml'

function Require-Command {
    param([Parameter(Mandatory)][string]$Name)

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command '$Name' was not found in PATH."
    }
}

function Require-EnvFile {
    if (-not (Test-Path -LiteralPath $EnvFile)) {
        throw 'Missing .env. Run: ./dev.ps1 init, then replace all example credentials.'
    }
}

function Import-DotEnv {
    Require-EnvFile

    foreach ($line in Get-Content -LiteralPath $EnvFile) {
        $trimmed = $line.Trim()
        if (-not $trimmed -or $trimmed.StartsWith('#')) {
            continue
        }

        $separator = $trimmed.IndexOf('=')
        if ($separator -lt 1) {
            continue
        }

        $name = $trimmed.Substring(0, $separator).Trim()
        $value = $trimmed.Substring($separator + 1).Trim()
        if (($value.StartsWith('"') -and $value.EndsWith('"')) -or
            ($value.StartsWith("'") -and $value.EndsWith("'"))) {
            $value = $value.Substring(1, $value.Length - 2)
        }

        [Environment]::SetEnvironmentVariable($name, $value, 'Process')
    }
}

switch ($Task) {
    'init' {
        if (Test-Path -LiteralPath $EnvFile) {
            Write-Host '.env already exists; it was not changed.'
            break
        }

        Copy-Item -LiteralPath (Join-Path $Root '.env.example') -Destination $EnvFile
        Write-Host 'Created .env from .env.example. Replace APP_MASTER_KEY and all example passwords before starting.'
    }
    'doctor' {
        foreach ($command in @('go', 'pnpm', 'docker')) {
            if (Get-Command $command -ErrorAction SilentlyContinue) {
                Write-Host "[ok] $command"
            } else {
                Write-Warning "[missing] $command"
            }
        }

        if (Test-Path -LiteralPath (Join-Path $Root '.tools/postgres/win32-x64/bin/postgres.exe')) {
            Write-Host '[ok] native-postgres'
        } else {
            Write-Warning '[missing] native-postgres (optional: run ./dev.ps1 native-db-install)'
        }

        if (Test-Path -LiteralPath $EnvFile) {
            Write-Host '[ok] .env'
        } else {
            Write-Warning '[missing] .env (run ./dev.ps1 init)'
        }
    }
    'infra-up' {
        Require-Command 'docker'
        Require-EnvFile
        docker compose --env-file $EnvFile -f $ComposeFile up -d --wait
    }
    'infra-down' {
        Require-Command 'docker'
        Require-EnvFile
        docker compose --env-file $EnvFile -f $ComposeFile down
    }
    'infra-logs' {
        Require-Command 'docker'
        Require-EnvFile
        docker compose --env-file $EnvFile -f $ComposeFile logs -f postgres
    }
    'native-db-install' {
        & (Join-Path $Root 'deploy/native-postgres.ps1') -Task install
    }
    'native-db-start' {
        & (Join-Path $Root 'deploy/native-postgres.ps1') -Task start
    }
    'native-db-stop' {
        & (Join-Path $Root 'deploy/native-postgres.ps1') -Task stop
    }
    'native-db-status' {
        & (Join-Path $Root 'deploy/native-postgres.ps1') -Task status
    }
    'backend' {
        Require-Command 'go'
        Import-DotEnv
        Push-Location (Join-Path $Root 'backend')
        try {
            go run ./cmd/server
        } finally {
            Pop-Location
        }
    }
    'frontend' {
        Require-Command 'pnpm'
        Push-Location (Join-Path $Root 'frontend')
        try {
            pnpm dev
        } finally {
            Pop-Location
        }
    }
}
