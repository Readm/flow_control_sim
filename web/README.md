# Web 模块

```
WebServer
  ├─ Router (multiplexer)
  ├─ ApiLayer (REST handlers)
  └─ WsHub (广播/订阅)
```

- **Router**：负责注册 HTTP/WS 路由。
- **ApiLayer**：按功能拆分控制、统计、配置等 REST 处理逻辑。
- **WsHub**：集中管理 WebSocket 会话、广播最新帧数据。
