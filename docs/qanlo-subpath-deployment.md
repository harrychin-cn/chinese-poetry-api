# Qanlo `/poetry-api/` 子路径部署手册

本手册对应“诗词画曲赋”运行在 `https://qanlo.com/poetry-api/` 的部署方式。应用会只接受由反向代理写入、且仅含安全路径字符的 `X-Forwarded-Prefix`；在本地直接访问时保持根路径行为。

## 上线顺序

1. 先构建并替换 S2 的 `poetry-api` 容器镜像，容器只监听本机 `127.0.0.1:12780`。
2. 在 S1 的 Nginx 中一次性应用下面的入口修复，先执行 `nginx -t`，再 reload。
3. 对公网前缀入口、页面链接、PWA 资源和 API 逐项验收；未通过则先回滚 Nginx，再回滚 S2 镜像。

S1 与 S2 即使位于同一台物理主机，也必须按以上两个逻辑步骤执行和记录。

## Nginx 片段

将原有诗词 API 入口替换为以下等价片段。保留站点中其他 Qanlo 路由与既有安全响应头。

```nginx
location = /poetry-api {
    return 301 https://qanlo.com/poetry-api/console;
}

location = /poetry-api/ {
    return 301 https://qanlo.com/poetry-api/console;
}

location /poetry-api/ {
    proxy_pass http://127.0.0.1:12780/;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-Prefix /poetry-api;
}
```

其中 `proxy_pass` 末尾的 `/` 必须保留：它会移除公网前缀后把应用路由转发为 `/console`、`/api/v1/...` 等内部根路径。`X-Forwarded-Prefix` 让应用在响应中重新生成公网可用的 `/poetry-api/...` 链接。

## 上线后验收

```bash
curl -fsSI https://qanlo.com/poetry-api/
curl -fsS https://qanlo.com/poetry-api/api/v1/health
curl -fsS 'https://qanlo.com/poetry-api/api/v1/poems/search?q=月'
curl -fsS https://qanlo.com/poetry-api/console | grep -F '/poetry-api/api/v1/poems/search'
curl -fsS https://qanlo.com/poetry-api/manifest.json | grep -F '"start_url": "/poetry-api/console"'
curl -fsS https://qanlo.com/poetry-api/service-worker.js | grep -F '"/poetry-api/console"'
```

入口请求只能在 HTTPS 内完成一次跳转到 `/poetry-api/console`；不得先降级到 `http://`。浏览器验收还需确认首页、控制台、文档、价格、作品库、个人页和证书页均不离开 `/poetry-api/`。

## 回滚

1. 恢复修改前、已备份的 Nginx 站点配置，执行 `nginx -t` 后 reload。
2. 若问题来自新容器镜像，切回发布前已保留的诗词 API 镜像并重新创建容器；不删除数据卷或备份卷。
3. 再次执行健康检查和搜索请求，记录回滚时间、镜像摘要和结果。
