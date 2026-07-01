import json
import os
import urllib.parse
import urllib.request

base_url = os.environ.get("BASE_URL", "http://localhost:1279")
api_key = os.environ.get("API_KEY")

if not api_key:
    raise SystemExit("Please set API_KEY=cp_live_xxx")

params = urllib.parse.urlencode({
    "author": "李白",
    "q": "月",
    "search_in": "content",
    "page": 1,
    "page_size": 5,
})

req = urllib.request.Request(
    f"{base_url}/api/v1/poems/query?{params}",
    headers={"X-API-Key": api_key},
)

with urllib.request.urlopen(req, timeout=10) as resp:
    payload = json.loads(resp.read().decode("utf-8"))

print(json.dumps(payload, ensure_ascii=False, indent=2))
