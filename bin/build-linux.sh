#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$ROOT_DIR/web"
FRONTEND_DIR="$WEB_DIR/default"

GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
CGO_ENABLED="${CGO_ENABLED:-0}"
OUTPUT_DIR="${OUTPUT_DIR:-$ROOT_DIR/build/linux}"
APP_NAME="${APP_NAME:-max-api}"
SKIP_FRONTEND="${SKIP_FRONTEND:-0}"
TRIMPATH="${TRIMPATH:-1}"

if [[ "$GOOS" != "linux" ]]; then
  echo "GOOS must be linux for this build script; got: $GOOS" >&2
  exit 1
fi

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

need_cmd go

VERSION="${VERSION:-}"
if [[ -z "$VERSION" ]]; then
  if git -C "$ROOT_DIR" describe --tags --always --dirty >/dev/null 2>&1; then
    VERSION="$(git -C "$ROOT_DIR" describe --tags --always --dirty)"
  elif [[ -s "$ROOT_DIR/VERSION" ]]; then
    VERSION="$(tr -d '[:space:]' < "$ROOT_DIR/VERSION")"
  else
    VERSION="dev"
  fi
fi

if [[ "$SKIP_FRONTEND" != "1" ]]; then
  need_cmd bun
  echo "Building embedded frontend..."
  (
    cd "$WEB_DIR"
    bun install --frozen-lockfile
    cd "$FRONTEND_DIR"
    DISABLE_ESLINT_PLUGIN=true VITE_REACT_APP_VERSION="$VERSION" bun run build
  )
elif [[ ! -f "$FRONTEND_DIR/dist/index.html" ]]; then
  echo "SKIP_FRONTEND=1 was set, but $FRONTEND_DIR/dist/index.html does not exist." >&2
  echo "Run without SKIP_FRONTEND=1 once to generate embedded frontend assets." >&2
  exit 1
fi

mkdir -p "$OUTPUT_DIR"

OUTPUT="$OUTPUT_DIR/${APP_NAME}-${GOOS}-${GOARCH}"
LDFLAGS="-s -w -X github.com/MAX-API-Next/MAX-API/common.Version=$VERSION"
BUILD_FLAGS=()
if [[ "$TRIMPATH" == "1" ]]; then
  BUILD_FLAGS+=("-trimpath")
fi

echo "Building backend: $OUTPUT"
(
  cd "$ROOT_DIR"
  GOOS="$GOOS" GOARCH="$GOARCH" CGO_ENABLED="$CGO_ENABLED" \
    go build "${BUILD_FLAGS[@]}" -ldflags "$LDFLAGS" -o "$OUTPUT" .
)

if command -v shasum >/dev/null 2>&1; then
  shasum -a 256 "$OUTPUT" > "$OUTPUT.sha256"
elif command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$OUTPUT" > "$OUTPUT.sha256"
fi

echo "Done: $OUTPUT"
file "$OUTPUT" 2>/dev/null || true
