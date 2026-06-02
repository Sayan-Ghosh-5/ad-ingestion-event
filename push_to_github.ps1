# Pushing local workspace to GitHub Repository
# Repository: https://github.com/Sayan-Ghosh-5/ad-ingestion-event

Write-Host "===================================================" -ForegroundColor Cyan
Write-Host "Pushing local workspace to GitHub Repository" -ForegroundColor Cyan
Write-Host "Repository: https://github.com/Sayan-Ghosh-5/ad-ingestion-event" -ForegroundColor Cyan
Write-Host "===================================================" -ForegroundColor Cyan
Write-Host ""

# 1. Add/Set Remote URL
Write-Host "Setting remote origin..." -ForegroundColor Yellow
git remote remove origin 2>$null
git remote add origin https://github.com/Sayan-Ghosh-5/ad-ingestion-event.git
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to add remote origin."
    Read-Host "Press Enter to exit"
    exit $LASTEXITCODE
}

# 2. Rename branch to main
Write-Host "Setting default branch to main..." -ForegroundColor Yellow
git branch -M main
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to rename branch to main."
    Read-Host "Press Enter to exit"
    exit $LASTEXITCODE
}

# 3. Stage all files
Write-Host "Staging all files..." -ForegroundColor Yellow
git add -A
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to stage files."
    Read-Host "Press Enter to exit"
    exit $LASTEXITCODE
}

# 4. Commit changes
Write-Host "Committing changes..." -ForegroundColor Yellow
git commit -m "feat: ad event ingestion service with hardening fixes

Includes:
- Worker pool with pgx.CopyFrom batching
- Idle rate limiter eviction to prevent OOM
- Singleflight cache stampede prevention
- Hourly rollup aggregation
- Input validation and sanitization
- Docker compose configuration and load tests"
if ($LASTEXITCODE -ne 0) {
    Write-Error "Failed to commit changes."
    Read-Host "Press Enter to exit"
    exit $LASTEXITCODE
}

# 5. Force push to replace remote files
Write-Host "Force pushing to main (replacing remote files)..." -ForegroundColor Yellow
git push -u origin main --force
if ($LASTEXITCODE -ne 0) {
    Write-Host ""
    Write-Host "Failed to push to remote repository." -ForegroundColor Red
    Write-Host "Please make sure you are logged in to GitHub via the git CLI or credential manager." -ForegroundColor Red
    Write-Host ""
    Read-Host "Press Enter to exit"
    exit $LASTEXITCODE
}

Write-Host ""
Write-Host "===================================================" -ForegroundColor Green
Write-Host "Success! Repository updated successfully." -ForegroundColor Green
Write-Host "===================================================" -ForegroundColor Green
Read-Host "Press Enter to exit"
