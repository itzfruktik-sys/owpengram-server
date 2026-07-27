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

rem --- Postgres role/database naming (telesrv -> owpengram), one-time, opt-in
rem Only runs for installs that already accepted the Docker naming migration
rem above; a "telesrv"-naming install already said no to this rename theme
rem once and isn't asked again. See deploy\migrate-db-naming.ps1.
if "%DOCKER_PREFIX%"=="owpengram" (
  powershell -NoProfile -ExecutionPolicy Bypass -File deploy\migrate-db-naming.ps1 -ContainerName "%DOCKER_PREFIX%-postgres"
  if !ERRORLEVEL! neq 0 (
    echo [ERROR] postgres naming migration failed
    pause
    exit /b 1
  )
)

rem --- Build ------------------------------------------------------------------
echo.
echo == [3/4] Building server binaries ==
if /i "%NO_BUILD%"=="true" (
  echo [cfg] skipping build, --no-build set
  if not exist "bin\owpengram-server.exe" (
    if not exist "bin\owpengram-admin-panel.exe" (
      echo [ERROR] no binaries found in bin\ - run without --no-build first
      pause
      exit /b 1
    )
  )
) else (
  if not exist bin mkdir bin
  echo [cfg] building owpengram-server...
  go build -o bin\owpengram-server.exe .\cmd\telesrv
  if !ERRORLEVEL! neq 0 (
    echo [ERROR] failed to build owpengram-server
    pause
    exit /b 1
  )
  echo [cfg] building owpengram-admin-panel...
  go build -o bin\owpengram-admin-panel.exe .\cmd\telesrv-admin
  if !ERRORLEVEL! neq 0 (
    echo [ERROR] failed to build owpengram-admin-panel
    pause
    exit /b 1
  )
  echo [ok] binaries built
)

rem --- Start servers ----------------------------------------------------------
echo.
echo == [4/4] Starting owpengram-server + owpengram-admin-panel ==

if not exist "%LOG_DIR%" mkdir "%LOG_DIR%"
set "SERVER_LOG=%LOG_DIR%\owpengram-server.log"
set "ADMIN_LOG=%LOG_DIR%\owpengram-admin-panel.log"

rem "start /B cmd /c ..." creates no window, so the earlier WINDOWTITLE-based
rem taskkill in stop_server below never matched anything and silently killed
rem nothing. Launch via Start-Process -PassThru instead and track the real
rem PID. Still routed through cmd.exe /c so ">>log 2>&1" can merge stdout and
rem stderr into one file the way the bash script does (the server's own zap
rem logger writes to stderr, so splitting the streams would leave the "main"
rem log file nearly empty); "taskkill /T" below kills the whole cmd+exe tree,
rem not just the cmd.exe wrapper, so this still actually stops the process.
set "SERVER_PID="
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Start-Process -FilePath 'cmd.exe' -ArgumentList '/c bin\owpengram-server.exe >> \"%SERVER_LOG%\" 2>&1' -WindowStyle Hidden -PassThru).Id"`) do set "SERVER_PID=%%i"
if not defined SERVER_PID (
  echo [ERROR] failed to start owpengram-server
  pause
  exit /b 1
)
echo [ok] owpengram-server started, PID %SERVER_PID%, logs -^> %SERVER_LOG%

set "ADMIN_PID="
for /f "usebackq delims=" %%i in (`powershell -NoProfile -Command "(Start-Process -FilePath 'cmd.exe' -ArgumentList '/c bin\owpengram-admin-panel.exe >> \"%ADMIN_LOG%\" 2>&1' -WindowStyle Hidden -PassThru).Id"`) do set "ADMIN_PID=%%i"
if not defined ADMIN_PID (
  echo [ERROR] failed to start owpengram-admin-panel
  taskkill /PID %SERVER_PID% /T /F >nul 2>&1
  pause
  exit /b 1
)
echo [ok] owpengram-admin-panel started, PID %ADMIN_PID%, logs -^> %ADMIN_LOG%

echo.
echo ============================================
echo  OwpenGram server is running
echo ============================================
echo.
echo  Logs:
echo    owpengram-server:       type %SERVER_LOG%
echo    owpengram-admin-panel:  type %ADMIN_LOG%
echo.
echo ============================================

rem --- Interactive menu ------------------------------------------------------
:menu
tasklist /FI "PID eq %SERVER_PID%" 2>nul | find "%SERVER_PID%" >nul
if errorlevel 1 (
  echo [WARN] owpengram-server PID %SERVER_PID% exited unexpectedly
  echo        Check %SERVER_LOG% for details
  taskkill /PID %ADMIN_PID% /T /F >nul 2>&1
  pause
  exit /b 1
)
tasklist /FI "PID eq %ADMIN_PID%" 2>nul | find "%ADMIN_PID%" >nul
if errorlevel 1 (
  echo [WARN] owpengram-admin-panel PID %ADMIN_PID% exited unexpectedly
  echo        Check %ADMIN_LOG% for details
  taskkill /PID %SERVER_PID% /T /F >nul 2>&1
  pause
  exit /b 1
)

echo.
echo   [1] View owpengram-server logs (last 50 lines)
echo   [2] View owpengram-admin-panel logs (last 50 lines)
echo   [3] View both logs (last 50 lines)
echo   [4] Follow owpengram-server logs (live)
echo   [5] Follow owpengram-admin-panel logs (live)
echo   [q] Stop server and exit
echo.
set /p "choice=  Choice: "

if "!choice!"=="1" (
  powershell -NoProfile -Command "Get-Content '%SERVER_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  goto menu
)
if "!choice!"=="2" (
  powershell -NoProfile -Command "Get-Content '%ADMIN_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  goto menu
)
if "!choice!"=="3" (
  echo   --- owpengram-server ---
  powershell -NoProfile -Command "Get-Content '%SERVER_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  echo   --- owpengram-admin-panel ---
  powershell -NoProfile -Command "Get-Content '%ADMIN_LOG%' -Tail 50 -ErrorAction SilentlyContinue"
  goto menu
)
if "!choice!"=="4" (
  echo   Press Ctrl+C to stop following
  powershell -NoProfile -Command "Get-Content '%SERVER_LOG%' -Wait -Tail 10"
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
echo [stop] stopping owpengram-server and owpengram-admin-panel ...
if defined SERVER_PID taskkill /PID %SERVER_PID% /T /F >nul 2>&1
if defined ADMIN_PID taskkill /PID %ADMIN_PID% /T /F >nul 2>&1
echo [ok] stopped.
exit /b 0
