#!/usr/bin/env bash
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:1279}"
API_KEY="${API_KEY:-}"

if [ -z "$API_KEY" ]; then
  echo "请先设置 API_KEY，例如：API_KEY=cp_live_xxx ./examples/curl/quickstart.sh"
  exit 1
fi

curl "$BASE_URL/api/v1/poems/query?author=李白&q=月&search_in=content&page=1&page_size=5" \
  -H "X-API-Key: $API_KEY"

echo
