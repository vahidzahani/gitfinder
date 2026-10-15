@echo off
rem Builds a small, self-contained gitfinder.exe (no runtime needed).
setlocal
where go >nul 2>nul || (
  echo Go is not installed. Install it with:  winget install GoLang.Go
  exit /b 1
)
go vet ./... || exit /b 1
go build -trimpath -ldflags "-s -w" -o gitfinder.exe . || exit /b 1
echo Built gitfinder.exe
