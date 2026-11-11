#!/usr/bin/env sh
# Menjalankan seluruh automated test SIKAT.
# Test integrasi membutuhkan server MySQL 8 uji. Database sikat_test_* dibuat
# dan dihapus otomatis per test. JANGAN arahkan ke server produksi.
set -eu
: "${SIKAT_TEST_DSN:=root@tcp(127.0.0.1:3306)/}"
export SIKAT_TEST_DSN
cd "$(dirname "$0")/.."
go vet ./...
go test ./... -count=1 "$@"
