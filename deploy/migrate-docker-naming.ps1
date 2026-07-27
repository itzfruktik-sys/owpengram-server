<#
Resolves (and, once, offers to migrate) the Docker project/container/volume
naming used by deploy/docker-compose.yml.

The project used to be named "telesrv" in Docker (containers telesrv-postgres /
telesrv-redis, volumes telesrv_pgdata / telesrv_redisdata). It's now
"owpengram" by default, so a gramsrv instance running on the same machine
can't be confused with this one at a glance in `docker ps` / Docker Desktop.

Existing installs may still have telesrv_* volumes with real data in them.
This script asks, at most once, whether to copy that data over to owpengram_*
naming. The answer is cached in a state file next to .secrets/.public_ip:
  - accepted -> volumes are copied (old telesrv_* volumes are left in place,
    untouched, as a backup), state becomes "owpengram", never asked again.
  - declined -> state becomes "telesrv", the install keeps the old naming
    forever, never asked again.
  - fresh install (no telesrv_* volumes found) -> silently uses "owpengram",
    nothing to migrate.

Prints exactly two lines to stdout: the resolved project name, then the
resolved container/volume prefix. Everything else (prompts, progress) goes to
stderr, so a caller can safely capture just stdout.
#>
[CmdletBinding()]
param(
    [string]$ComposeFile = "deploy\docker-compose.yml",
    [string]$StateFile = ".docker_naming"
)

function Write-Info([string]$Message) {
    [Console]::Error.WriteLine($Message)
}

function Write-Result([string]$Project, [string]$Prefix) {
    [Console]::Out.WriteLine($Project)
    [Console]::Out.WriteLine($Prefix)
}

function Test-DockerVolume([string]$Name) {
    docker volume inspect $Name *> $null
    return $LASTEXITCODE -eq 0
}

$state = $null
if (Test-Path $StateFile) {
    $state = (Get-Content $StateFile -Raw -ErrorAction SilentlyContinue)
    if ($state) { $state = $state.Trim() }
}

if ($state -eq "owpengram") {
    Write-Result "owpengram" "owpengram"
    exit 0
}
if ($state -eq "telesrv") {
    Write-Result "telesrv" "telesrv"
    exit 0
}

$oldPg = Test-DockerVolume "telesrv_pgdata"
$oldRedis = Test-DockerVolume "telesrv_redisdata"

if (-not $oldPg -and -not $oldRedis) {
    # Fresh install: nothing to migrate, just adopt the new naming.
    Set-Content -Path $StateFile -Value "owpengram" -NoNewline
    Write-Result "owpengram" "owpengram"
    exit 0
}

if ([Console]::IsInputRedirected) {
    # Can't prompt right now (piped/non-interactive run). Keep old naming for
    # this run only — don't cache a decision nobody actually made.
    Write-Info "[cfg] old 'telesrv_*' Docker volumes found but running non-interactively; keeping old naming for now"
    Write-Result "telesrv" "telesrv"
    exit 0
}

Write-Info ""
Write-Info "== Docker container/volume naming =="
Write-Info "Found existing Docker volumes named 'telesrv_*' (from before the project was"
Write-Info "renamed to OwpenGram). Renaming them to 'owpengram_*' avoids confusing this"
Write-Info "server with a gramsrv instance running on the same machine. Recommended."
$reply = Read-Host "Migrate Docker containers/volumes from 'telesrv' to 'owpengram' naming now? [Y/n]"

if ($reply -match '^[Nn]') {
    Set-Content -Path $StateFile -Value "telesrv" -NoNewline
    Write-Info "[cfg] keeping 'telesrv' Docker naming (won't ask again)"
    Write-Result "telesrv" "telesrv"
    exit 0
}

Write-Info "[cfg] migrating Docker volumes: telesrv_* -> owpengram_*"

$env:TELESRV_DOCKER_PROJECT = "telesrv"
$env:TELESRV_DOCKER_PREFIX = "telesrv"
docker compose -f $ComposeFile -p telesrv stop postgres redis *> $null
Remove-Item Env:\TELESRV_DOCKER_PROJECT, Env:\TELESRV_DOCKER_PREFIX -ErrorAction SilentlyContinue

if ($oldPg) {
    docker volume create owpengram_pgdata *> $null
    docker run --rm -v telesrv_pgdata:/from -v owpengram_pgdata:/to alpine sh -c "cp -a /from/. /to/"
    if ($LASTEXITCODE -ne 0) {
        Write-Info "[ERROR] failed to copy pgdata volume"
        exit 1
    }
}
if ($oldRedis) {
    docker volume create owpengram_redisdata *> $null
    docker run --rm -v telesrv_redisdata:/from -v owpengram_redisdata:/to alpine sh -c "cp -a /from/. /to/"
    if ($LASTEXITCODE -ne 0) {
        Write-Info "[ERROR] failed to copy redisdata volume"
        exit 1
    }
}

$env:TELESRV_DOCKER_PROJECT = "telesrv"
$env:TELESRV_DOCKER_PREFIX = "telesrv"
docker compose -f $ComposeFile -p telesrv down *> $null
Remove-Item Env:\TELESRV_DOCKER_PROJECT, Env:\TELESRV_DOCKER_PREFIX -ErrorAction SilentlyContinue

Set-Content -Path $StateFile -Value "owpengram" -NoNewline
Write-Info "[ok] migration complete - old 'telesrv_*' volumes were left in place, untouched, as a backup"
Write-Result "owpengram" "owpengram"
