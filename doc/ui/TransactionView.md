# Transaction View 调用链梳理

本文档记录 Web 前端「Transaction View」的工作流程、相关后端接口以及常见错误排查方法，便于后续调试与扩展。

## 前端交互流程

1. 用户在 `Transaction View` 页面输入 Transaction ID 并点击 `Load Timeline`（或按 Enter）。
2. `web/static/js/views/txnView.js` 调用 `loadTimeline(txnId)`：
   - 校验 ID 是否为正整数；否则提示 `Please enter a valid transaction ID`。
   - 通过 `fetch('/api/transaction/${id}/timeline')` 访问 REST 接口。
3. 根据返回结果：
   - HTTP 404：前端提示 `Transaction ${id} not found`。
   - HTTP 200：解析 JSON 并调用 `renderMermaidSequenceDiagram(...)`。
   - 如果 `timeline.events` 为空或缺失，显示 `No events found for this transaction`。
4. Mermaid 渲染完成后，会在页面生成时序图及原始 Mermaid 源码，方便调试。

## 后端接口链路

```
GET /api/transaction/{id}/timeline
  └─ web_api_timeline.go: handleTransactionTimeline
       └─ TransactionManager.GetTransactionTimeline(id)
            ├─ 返回 TransactionTimeline（含 events, nodes, packets）
            └─ 事件来源：TransactionManager.RecordPacketEvent
```

- `TransactionManager` 通过 `RecordPacketEvent` 持续接收各节点上报的 `PacketEvent`，并将其按 `TransactionID` 存入 `packetHistory`。
- 事件的产生点分布在 `rn.go`、`hn.go`、`sn.go`、`channel.go` 等文件内部，均以 `p.TransactionID` 为键写入。
- 配置项控制：`Config.EnablePacketHistory`、`MaxPacketHistorySize`、`HistoryOverflowMode`、`MaxTransactionHistory` 共同影响历史是否保留。
  - 默认情况下（未显式配置），`EnablePacketHistory` 会被强制设为 `true`，历史记录无限制。
  - 若同时设置了 `EnablePacketHistory=false` 且任一历史相关字段为非零值，则事件记录会被完全关闭，导致时间线为空。

## 常见错误与排查

### 1. “No events found for this transaction”

**触发条件**：接口返回 200，但 `timeline.events` 为空。常见原因：

- **历史记录未开启**：检查配置文件是否显式置 `EnablePacketHistory=false`，且同时设置了 `MaxPacketHistorySize/MaxTransactionHistory/HistoryOverflowMode` 其中之一；若是，请改为 `EnablePacketHistory=true` 或移除相关限制。
- **事件尚未到达**：请求在事务刚创建后立即发起，队列/链路尚未产生任何 `PacketEvent`。可稍等若干 cycle 再次查询。
- **历史被清理**：若配置了有限的 `MaxTransactionHistory` 或 `MaxPacketHistorySize`，旧事务可能被回收。可通过 `/api/transactions` 查看最新事务 ID，确认目标是否仍存在。

**调试步骤**：

1. `curl -s http://127.0.0.1:8080/api/transactions | jq '.[].id'` 获取可用 Transaction ID。
2. `curl -s http://127.0.0.1:8080/api/transaction/1/timeline | jq '.events | length'` 查看事件数量。
3. 若结果为 0，检查模拟器启动日志中是否出现 `transaction manager event channel` 等相关警告。
4. 若配置问题导致历史禁用，修改配置后重新 `Reset`（或重启模拟器），再试。

### 2. HTTP 404（Transaction not found）

- 说明 `TransactionManager` 内不存在该 ID 的上下文，多发生在事务已经完成且模拟器执行了 `Reset`，或 ID 输入错误。
- 可通过 `/api/transactions` 列表确认 ID 是否仍存在。

## 建议

- 在自动化 UI 测试中，可先调用 `/api/transactions` 获取最新 ID，减少手动输入错误。
- 如果需要长时间调试同一事务，可在配置中增大 `MaxTransactionHistory` 或设置 `HistoryOverflowMode="initial"`，避免历史被过早清理。
