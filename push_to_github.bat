@echo off
echo ===================================================
echo Pushing local workspace to GitHub Repository
echo Repository: https://github.com/Sayan-Ghosh-5/ad-ingestion-event
echo ===================================================
echo.

:: 1. Add/Set Remote URL
echo Setting remote origin...
git remote remove origin 2>nul
git remote add origin https://github.com/Sayan-Ghosh-5/ad-ingestion-event.git
if %ERRORLEVEL% NEQ 0 (
    echo Failed to add remote origin.
    pause
    exit /b %ERRORLEVEL%
)

:: 2. Rename branch to main
echo Setting default branch to main...
git branch -M main
if %ERRORLEVEL% NEQ 0 (
    echo Failed to rename branch to main.
    pause
    exit /b %ERRORLEVEL%
)

:: 3. Stage all files
echo Staging all files...
git add -A
if %ERRORLEVEL% NEQ 0 (
    echo Failed to stage files.
    pause
    exit /b %ERRORLEVEL%
)

:: 4. Commit changes
echo Committing changes...
git commit -m "feat: ad event ingestion service with hardening fixes

Includes:
- Worker pool with pgx.CopyFrom batching
- Idle rate limiter eviction to prevent OOM
- Singleflight cache stampede prevention
- Hourly rollup aggregation
- Input validation and sanitization
- Docker compose configuration and load tests"
if %ERRORLEVEL% NEQ 0 (
    echo Failed to commit changes.
    pause
    exit /b %ERRORLEVEL%
)

:: 5. Force push to replace remote files
echo Force pushing to main (replacing remote files)...
git push -u origin main --force
if %ERRORLEVEL% NEQ 0 (
    echo.
    echo Failed to push to remote repository.
    echo Please make sure you are logged in to GitHub via the git CLI or credential manager.
    echo.
    pause
    exit /b %ERRORLEVEL%
)

echo.
echo ===================================================
echo Success! Repository updated successfully.
echo ===================================================
pause
