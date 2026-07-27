<#
One-time, opt-in migration of the Postgres role/database name inside an
existing data volume from "telesrv" to "owpengram".

docker-compose.yml's POSTGRES_USER/POSTGRES_DB env vars only take effect when
Postgres bootstraps a brand-new, empty data volume -- they're silently
ignored against a pre-existing volume, so changing the compose file alone
does nothing for an install that already has data on disk. This script talks
to the already-running Postgres container directly (run it after "docker
compose up" plus a readiness wait, not before) and renames the role/database
in place via ALTER ROLE / ALTER DATABASE.

The existing password is left untouched -- renaming doesn't change it. Only
the TELESRV_POSTGRES_DSN line in .env is patched (never any other line, and
never if that line was already customized away from the plain telesrv
defaults) so the app can still connect afterwards.

Prints nothing when there's nothing to do. Prompts interactively at most
once; the decision is cached in a state file next to .secrets/.public_ip, so
this is never asked again. Declining is permanent: the install keeps the
"telesrv" role/database name forever.
#>
[CmdletBinding()]
param(
    [string]$ContainerName = "owpengram-postgres",
    [string]$EnvFile = ".env",
    [string]$StateFile = ".db_naming"
)

function Write-Info([string]$Message) {
    [Console]::Error.WriteLine($Message)
}

function Test-PgRoleExists([string]$Role) {
    $out = docker exec $ContainerName psql -U telesrv -d postgres -tAc "SELECT 1 FROM pg_roles WHERE rolname='$Role'" 2>$null
    return ($LASTEXITCODE -eq 0) -and ($out -match '1')
}

$state = $null
if (Test-Path $StateFile) {
    $state = (Get-Content $StateFile -Raw -ErrorAction SilentlyContinue)
    if ($state) { $state = $state.Trim() }
}
if ($state -eq "owpengram" -or $state -eq "telesrv") {
    exit 0
}

$oldExists = Test-PgRoleExists "telesrv"
$newExists = Test-PgRoleExists "owpengram"

if (-not $oldExists -or $newExists) {
    # Nothing to migrate: either already "owpengram"-named (fresh install or
    # already renamed), or connecting as "telesrv" didn't work at all (custom
    # credentials already in place) -- leave it alone either way.
    Set-Content -Path $StateFile -Value "owpengram" -NoNewline
    exit 0
}

if ([Console]::IsInputRedirected) {
    Write-Info "[cfg] Postgres role/database still named 'telesrv' but running non-interactively; keeping old naming for now"
    exit 0
}

Write-Info ""
Write-Info "== Postgres role/database naming =="
Write-Info "The Postgres role and database inside this install's data volume are still"
Write-Info "named 'telesrv' (renaming docker-compose.yml alone doesn't affect existing"
Write-Info "data). Renaming them to 'owpengram' keeps everything consistent. Recommended."
$reply = Read-Host "Rename Postgres role/database from 'telesrv' to 'owpengram' now? [Y/n]"

if ($reply -match '^[Nn]') {
    Set-Content -Path $StateFile -Value "telesrv" -NoNewline
    Write-Info "[cfg] keeping 'telesrv' Postgres role/database naming (won't ask again)"
    exit 0
}

Write-Info "[cfg] renaming Postgres role/database: telesrv -> owpengram"

# Postgres refuses "ALTER ROLE <session user> RENAME" (session user cannot be
# renamed), so telesrv can't rename itself. Do the database rename directly
# as telesrv, then use a short-lived second superuser to rename the telesrv
# role, then drop that temporary role from the now-renamed "owpengram" role's
# own session. Each step is a separate connection/transaction on purpose:
# combining the ALTER DATABASE and ALTER ROLE in one multi-statement call
# rolls the database rename back too when the role rename fails (Postgres
# treats a semicolon-separated -c string as one implicit transaction).
docker exec $ContainerName psql -U telesrv -d postgres -c "ALTER DATABASE telesrv RENAME TO owpengram; CREATE ROLE _telesrv_migrate SUPERUSER LOGIN PASSWORD 'telesrv_migrate';" *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Info "[ERROR] failed to rename Postgres database / create temporary migration role"
    exit 1
}
docker exec $ContainerName psql -U _telesrv_migrate -d owpengram -c "ALTER ROLE telesrv RENAME TO owpengram;" *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Info "[ERROR] failed to rename Postgres role"
    exit 1
}
docker exec $ContainerName psql -U owpengram -d owpengram -c "DROP ROLE _telesrv_migrate;" *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Info "[WARN] renamed role/database but failed to drop the temporary _telesrv_migrate role - remove it by hand"
}

Set-Content -Path $StateFile -Value "owpengram" -NoNewline
Write-Info "[ok] Postgres role/database renamed to 'owpengram'"

if (Test-Path $EnvFile) {
    $lines = Get-Content $EnvFile
    $pattern = '^TELESRV_POSTGRES_DSN=postgres://telesrv:([^@]*)@([^/]+)/telesrv(\?.*)?$'
    $patched = $false
    $newLines = foreach ($line in $lines) {
        if (-not $patched -and $line -match $pattern) {
            $patched = $true
            "TELESRV_POSTGRES_DSN=postgres://owpengram:$($Matches[1])@$($Matches[2])/owpengram$($Matches[3])"
        } else {
            $line
        }
    }
    if ($patched) {
        Set-Content -Path $EnvFile -Value $newLines
        Write-Info "[ok] updated the TELESRV_POSTGRES_DSN line in $EnvFile (only that one line)"
    } else {
        Write-Info "[WARN] TELESRV_POSTGRES_DSN in $EnvFile doesn't match the plain telesrv defaults - update it by hand:"
        Write-Info "       TELESRV_POSTGRES_DSN=postgres://owpengram:<your-password>@<host>:<port>/owpengram?sslmode=disable"
    }
} else {
    Write-Info "[WARN] $EnvFile not found - if you create one, point TELESRV_POSTGRES_DSN at the 'owpengram' role/database"
}
