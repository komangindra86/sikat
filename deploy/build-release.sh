#!/usr/bin/env sh
# Membangun paket rilis Linux (binary tunggal + file deploy).
# Pemakaian: ./deploy/build-release.sh 1.0.0
set -eu
VERSION="${1:-dev}"
OUT="dist/sikat-${VERSION}-linux-amd64"
rm -rf "$OUT" && mkdir -p "$OUT"
# CSS sudah ter-build di static/css/app.css (Node hanya diperlukan saat mengubah tampilan).
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION}" -o "$OUT/sikat" ./cmd/server
cp .env.example "$OUT/.env.example"
cp -r deploy/systemd deploy/nginx "$OUT/"
cp docs/DEPLOYMENT.md "$OUT/"
tar -C dist -czf "dist/sikat-${VERSION}-linux-amd64.tar.gz" "sikat-${VERSION}-linux-amd64"
sha256sum "dist/sikat-${VERSION}-linux-amd64.tar.gz" > "dist/sikat-${VERSION}-linux-amd64.tar.gz.sha256"
echo "Paket: dist/sikat-${VERSION}-linux-amd64.tar.gz"
