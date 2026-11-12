@echo off
REM Menjalankan SIKAT untuk development lokal.
REM Akses: http://sikat.test  (Nginx Laragon -> 127.0.0.1:8080)
REM Tutup jendela ini atau tekan Ctrl+C untuk menghentikan server.
cd /d "%~dp0"
echo Membangun SIKAT...
go build -o bin\sikat.exe .\cmd\server || goto :error
echo Menerapkan migrasi database...
bin\sikat.exe migrate up || goto :error
echo.
echo SIKAT berjalan di http://sikat.test
echo.
bin\sikat.exe serve
goto :eof
:error
echo.
echo Gagal menjalankan SIKAT. Periksa pesan di atas.
pause
