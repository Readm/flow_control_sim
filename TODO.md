# Network Core TODO List

## P0 - 立即需要

- [ ] **统计和监控API**
  - Network/Node/Link级别的统计数据（packet发送/接收/丢弃、延迟、利用率）
  - GetStats() / ResetStats() 接口

- [ ] **拓扑查询API**
  - GetNode(id), GetAllNodes(), GetAllLinks()
  - GetNeighbors(nodeID), GetTopology()

## P1 - 近期需要

- [ ] **Packet注入和提取**
  - InjectPacket(nodeID, pkt)
  - ExtractPackets(nodeID)
  - PendingPackets() - 网络中传输的packet数量

- [ ] **拓扑生成器**
  - NewRingTopology(), NewMeshTopology(), NewTreeTopology()
  - 减少测试代码重复

- [ ] **拓扑验证**
  - Validate() - 验证拓扑合法性
  - ValidateConnectivity() - 检查孤立节点
  - CheckBandwidthMatch() - 验证带宽匹配

## P2 - 可以延后

- [ ] **配置文件支持** - LoadFromFile/SaveToFile (JSON/YAML)
- [ ] **Checkpoint和恢复** - 保存/恢复网络状态
- [ ] **事件回调系统** - PacketSent/Received/Dropped事件

## 备注

- 统计监控是性能分析的基础，当前只能靠profiling
- 拓扑查询API对调试和可视化很重要
- 其他功能可根据实际需求优先级调整
