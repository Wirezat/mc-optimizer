#!/usr/bin/env bash
# Flattens all MI recipe JSONs into a single directory.
# Files are renamed using their relative path (/ → __) to avoid collisions.
#
# Usage:
#   ./flatten-mi-recipes.sh [source-dir] [output-dir]
#
# Defaults:
#   source: ~/files/software_open/Modern-Industrialization/src/generated/resources/data/modern_industrialization/recipe
#   output: ~/mi_recipes_flat

SRC="${1:-$HOME/files/software_open/Modern-Industrialization/src/generated/resources/data/modern_industrialization/recipe}"
OUT="${2:-$HOME/mi_recipes_flat}"

if [ ! -d "$SRC" ]; then
  echo "Error: source directory not found: $SRC" >&2
  exit 1
fi

mkdir -p "$OUT"

count=0
while IFS= read -r -d '' file; do
  # Path relative to SRC, e.g. "alloy/mixer/battery_alloy/dust.json"
  rel="${file#$SRC/}"
  # Replace / with __ for a flat filename
  flat="${rel//\//__}"
  cp "$file" "$OUT/$flat"
  (( count++ ))
done < <(find "$SRC" -name "*.json" -print0)

echo "Copied $count recipes → $OUT"
