# 如何查看 Cache 机制执行日志

## 快速开始

运行测试并查看日志：

```bash
go test -v -run TestReadOnceCacheMechanism
```

## 日志输出说明

日志会显示以下关键信息：

### 1. ReadOnce 请求接收
```
[Cache] HomeNode 2: Received ReadOnce request (TxnID=1, Addr=0x1000, Cycle=4)
```
- 显示 HomeNode 收到了 ReadOnce 请求
- 包含事务ID、地址和周期信息

### 2. 缓存查找结果

**缓存未命中 (Cache Miss):**
```
[Cache] HomeNode 2: CACHE MISS for address 0x1000 (TxnID=1, Cycle=4) - forwarding to SN
```
- 缓存中没有数据，需要转发到 Slave Node

**缓存命中 (Cache Hit):**
```
[Cache] HomeNode 2: CACHE HIT for address 0x1000 (TxnID=2, Cycle=24)
```
- 缓存中有数据，可以直接返回

### 3. 缓存更新
```
[Cache] HomeNode 2: Received CompData from SN, updating cache for address 0x1000 (TxnID=1, Cycle=6)
[Cache] HomeNode 2: Cache updated for address 0x1000 (TxnID=1)
```
- 从 SN 收到数据后更新缓存

### 4. 响应发送
```
[Cache] HomeNode 2: Sending CompData response directly to RN 0 (TxnID=2, PacketID=4, Cycle=24)
```
- 缓存命中时，直接发送响应给 Request Node

## 完整执行流程示例

### 第一次请求（缓存未命中）
1. Cycle 4: RN 发送 ReadOnce 请求到 HN
2. Cycle 4: HN 检查缓存 → **CACHE MISS**
3. Cycle 4: HN 转发请求到 SN
4. Cycle 6: SN 处理请求并返回 CompData
5. Cycle 6: HN 收到 CompData → **更新缓存**
6. Cycle 6: HN 转发 CompData 到 RN

### 第二次请求（缓存命中）
1. Cycle 24: RN 发送 ReadOnce 请求到 HN（相同地址）
2. Cycle 24: HN 检查缓存 → **CACHE HIT**
3. Cycle 24: HN 直接生成 CompData 响应
4. Cycle 24: HN 直接发送响应到 RN

## 查看 Transaction Metadata

除了日志，还可以通过 Transaction Metadata 查看缓存行为：

```go
txn := txnMgr.GetTransaction(txnID)
if txn.Context.Metadata != nil {
    cacheHit := txn.Context.Metadata["cache_hit"]      // "true" if hit
    cacheMiss := txn.Context.Metadata["cache_miss"]     // "true" if miss
    cacheUpdated := txn.Context.Metadata["cache_updated"] // "true" if updated
}
```

## 日志级别

默认日志级别是 `LogLevelInfo`，会显示所有 `[Cache]` 标签的日志。

如果需要更详细的调试信息，可以修改 `logger.go` 中的日志级别。

