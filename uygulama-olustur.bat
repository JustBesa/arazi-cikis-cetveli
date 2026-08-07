@echo off
title Arazi Cikis Cetveli Derleme

echo Kod kontrol ediliyor...
go test ./...

if errorlevel 1 (
    echo.
    echo HATA: Testler basarisiz. EXE olusturulmadi.
    pause
    exit /b 1
)

echo.
echo Uygulama olusturuluyor...
go build -trimpath -ldflags="-H=windowsgui -s -w" -o "Arazi-Cikis-Cetveli2.exe" .

if errorlevel 1 (
    echo.
    echo HATA: Derleme basarisiz.
    pause
    exit /b 1
)

echo.
echo Uygulama basariyla olusturuldu:
echo %CD%\Arazi-Cikis-Cetveli2.exe
pause