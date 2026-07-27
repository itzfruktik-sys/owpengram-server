@echo off
setlocal enabledelayedexpansion
cd /d "%~dp0"

set "ENV_FILE=.env"
set "COMPOSE_FILE=deploy\docker-compose.yml"
set "LOG_DIR=logs"

set "NO_BUILD=false"
:parse_args
if "%~1"=="" goto done_args
if /i "%~1"=="--no-build" set "NO_BUILD=true"
shift
goto parse_args
:done_args

echo [cfg] script started, NO_BUILD=%NO_BUILD%

rem This script only starts the server -- it never writes or edits .env. Set
rem up .env yourself (from .env.example) before running it.
if not exist "%ENV_FILE%" (
  echo [ERROR] %ENV_FILE% not found - copy .env.example to %ENV_FILE% and configure it first
  pause
  exit /b 1
)

rem --- Docker container/volume naming (telesrv -> owpengram), one-time, opt-in
rem Resolves whether this install uses the new "owpengram" Docker naming or
rem (if telesrv_* volumes exist and the user declined migrating them) keeps
rem the old "telesrv" naming permanently. See deploy\migrate-docker-naming.ps1.
set "DOCKER_PROJECT="
set "DOCKER_PREFIX="
for /f "usebackq delims=" %%i in (`powershell -NoProfile -ExecutionPolicy Bypass -File deploy\migrate-docker-naming.ps1`) do (
  if not defined DOCKER_PROJECT (set "DOCKER_PROJECT=%%i") else if not defined DOCKER_PREFIX (set "DOCKER_PREFIX=%%i")
)
if not defined DOCKER_PROJECT (
  echo [ERROR] docker naming resolution failed
  pause
  exit /b 1
)
set "TELESRV_DOCKER_PROJECT=%DOCKER_PROJECT%"
set "TELESRV_DOCKER_PREFIX=%DOCKER_PREFIX%"
echo [cfg] docker naming = %DOCKER_PREFIX% (project %DOCKER_PROJECT%)

rem --- Start infrastructure (PostgreSQL + Redis) ----------------------------
echo.
echo == [1/4] Starting infrastructure (PostgreSQL + Redis) ==
docker compose -f "%COMPOSE_FILE%" up -d
if %ERRORLEVEL% neq 0 (
  echo [ERROR] docker compose failed
  pause
  exit /b 1
)

rem --- Wait for PostgreSQL --------------------------------------------------
echo.
echo == [2/4] Waiting for PostgreSQL ==
set /a "_pgw=0"
:wait_pg
docker exec %DOCKER_PREFIX%-postgres pg_isready -U telesrv -d telesrv >nul 2>&1
if not errorlevel 1 goto pg_ready
set /a "_pgw+=1"
if !_pgw! gtr 30 (
  echo [ERROR] PostgreSQL not ready after 60s
  pause
  exit /b 1
)
echo [cfg] waiting for PostgreSQL... !_pgw!/30
timeout /t 2 >nul
goto wait_pg
:pg_ready
echo [ok] PostgreSQL is ready

rem --- Build ------------------------------------------------------------------
echo.
echo == [3/4] Building server binaries ==
if /i "%NO_BUILD%"=="true" (
  echo [cfg] skipping build, --no-build set
  if not exist "bin\telesrv.exe" (
    if not exist "bin\telesrv-admin.exe" (
      echo [ERROR] no binaries found in bin\ - run without --no-build first
      pause
      exit /b 1
    )
  )
) else (
  if not exist bin mkdir bin
  echo [cfg] building telesrv...
  go build -o bin\telesrv.exe .\cmd\telesrv
  if %ERRORLEVEL% neq 0 (
    echo [ERROR] failed to build telesrv
    pause
    exit /b 1
  )
  echo [cfg] building telesrv-admin...
  go build -o bin\telesrv-admin.exe .\cmd\telesrv-admin
  if %ERRORLEVEL% neq 0 (
    echo [ERROR] failed to build telesrv-admin
    pause
    exit /b 1
  )
  echo [ok] binaries built
)

rem --- Start servers ----------------------------------------------------------
echo.
echo == [4/4] Starting telesrv + telesrv-admin ==

if not exist "%LOG_DIR%" mkdir "%LOG_DIR%"
set "TELESRV_LOG=%LOG_DIR%\telesrv.log"
set "ADMIN_LOG=%LOG_DIR%\telesrv-admin.log"

rem "start /B cmd /c ..." creates no window, so the earlier WINDOWTITLE-based
rem taskkill in stop_server below never matched anything and silently killed
rem nothing. Launch via Start-Process -PassThru instead and track the real
rem PID. Still routed through cmd.exe /c so ">>log 2>&1" can merge stdout and
rem stderr into one file the way the bash script does (telesrv's own zap
rem logger writes to stderr, so splitting the streams would leave the "main"
rem log file nearly empty); "taskkill /T" below kills the whole cmd+exe tree,
rem not just the cmd.exe wrapper, so this still actually stops the process.
set "TELESRV_PID="
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Start-Process -FilePath 'cmd.exe' -ArgumentList '/c bin\telesrv.exe >> \"%TELESRV_LOG%\" 2>&1' -WindowStyle Hidden -PassThru).Id"`) do set "TELESRV_PID=%%i"
if not defined TELESRV_PID (
  echo [ERROR] failed to start telesrv
  pause
  exit /b 1
)
echo [ok] telesrv started (PID %TELESRV_PID%), logs -^> %TELESRV_LOG%

set "ADMIN_PID="
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Start-Process -FilePath 'cmd.exe' -ArgumentList '/c bin\telesrv-admin.exe >> \"%ADMIN_LOG%\" 2>&1' -WindowStyle Hidden -PassThru).Id"`) do set "ADMIN_PID=%%i"
if not defined ADMIN_PID (
  echo [ERROR] failed to start telesrv-admin
  taskkill /PID %TELESRV_PID% /T /F >nul 2>&1
  pause
  exit /b 1
)
echo [ok] telesrv-admin started (PID %ADMIN_PID%), logs -^> %ADMIN_LOG%

echo.
echo ============================================
echo  OwpenGram server is running
echo ============================================
echo.
echo  Logs:
echo    telesrv:        type %TELESRV_LOG%
echo    telesrv-admin:  type %ADMIN_LOG%
echo.
echo ============================================

rem --- Interactive menu ------------------------------------------------------
:menu
tasklist /FI "PID eq %TELESRV_PID%" 2>nul | find "%TELESRV_PID%" >nul
if errorlevel 1 (
  echo [WARN] telesrv PID %TELESRV_PID% exited unexpectedly
  echo        Check %TELESRV_LOG% for details
  taskkill /PID %ADMIN_PID% /T /F >nul 2>&1
  pause
  exit /b 1
)
tasklist /FI "PID eq %ADMIN_PID%" 2>nul | find "%ADMIN_PID%" >nul
if errorlevel 1 (
  echo [WARN] telesrv-admin PID %ADMIN_PID% exited unexpectedly
  echo        Check %ADMIN_LOG% for details
  taskkill /PID %TELESRV_PID% /T /F >nul 2>&1
  pause
  exit /b 1
)

echo.
echo   [1] View telesrv logs (last 50 lines)
echo   [2] View telesrv-admin logs (last 50 lines)
echo   [3] View both logs (last 50 lines)
echo   [4] Follow telesrv logs (live)
echo   [5] Follow telesrv-admin logs (live)
echo   [q] Stop server and exit
echo.
set /p "choice=  Choice: "

if "!choice!"=="1" (
  powershell -NoProfile -Command "Get-Content '%TELESRV_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  goto menu
)
if "!choice!"=="2" (
  powershell -NoProfile -Command "Get-Content '%ADMIN_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  goto menu
)
if "!choice!"=="3" (
  echo   --- telesrv ---
  powershell -NoProfile -Command "Get-Content '%TELESRV_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  echo   --- telesrv-admin ---
  powershell -NoProfile -Command "Get-Content '%ADMIN_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  goto menu
)
if "!choice!"=="4" (
  echo   Press Ctrl+C to stop following
  powershell -NoProfile -Command "Get-Content '%TELESRV_LOG%' -Wait -Tail 10"
  goto menu
)
if "!choice!"=="5" (
  echo   Press Ctrl+C to stop following
  powershell -NoProfile -Command "Get-Content '%ADMIN_LOG%' -Wait -Tail 10"
  goto menu
)
if /i "!choice!"=="q" goto stop_server
echo   Invalid choice
goto menu

:stop_server
echo.
echo [stop] stopping telesrv and telesrv-admin ...
if defined TELESRV_PID taskkill /PID %TELESRV_PID% /T /F >nul 2>&1
if defined ADMIN_PID taskkill /PID %ADMIN_PID% /T /F >nul 2>&1
echo [ok] stopped.
exit /b 0
