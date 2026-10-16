#!/bin/sh
# Génère app.css (Tailwind v4 standalone + daisyUI 5). Aucun Node requis.
set -eu
cd "$(dirname "$0")"
TW=4.3.3; DAISY=5.7.47
mkdir -p .bin
[ -x .bin/tailwindcss ] || { curl -fsSL -o .bin/tailwindcss "https://github.com/tailwindlabs/tailwindcss/releases/download/v$TW/tailwindcss-macos-arm64"; chmod +x .bin/tailwindcss; }
[ -f .bin/daisyui.mjs ] || curl -fsSL -o .bin/daisyui.mjs "https://github.com/saadeghi/daisyui/releases/download/v$DAISY/daisyui.mjs"
[ -f .bin/daisyui-theme.mjs ] || curl -fsSL -o .bin/daisyui-theme.mjs "https://github.com/saadeghi/daisyui/releases/download/v$DAISY/daisyui-theme.mjs"
./.bin/tailwindcss -i input.css -o app.css --minify
ls -l app.css
