const baseUrl = process.env.BASE_URL || "http://localhost:1279";
const apiKey = process.env.API_KEY;

if (!apiKey) {
  throw new Error("Please set API_KEY=cp_live_xxx");
}

const params = new URLSearchParams({
  author: "李白",
  q: "月",
  search_in: "content",
  page: "1",
  page_size: "5",
});

const res = await fetch(`${baseUrl}/api/v1/poems/query?${params}`, {
  headers: { "X-API-Key": apiKey },
});

if (!res.ok) {
  throw new Error(`HTTP ${res.status}: ${await res.text()}`);
}

console.log(JSON.stringify(await res.json(), null, 2));
