# Network Core TODO List
 帮我做如下优化：我们要求Ready信息也必须是单增的，以此，来简化ReadyMap，变为一个ReadyQueue。帮我设计一个ReadyUntil和ReadyQueue的协作模式。
ReadyQueue的意思是，它存储每个Cycle的Ready的信息，如果ReadyUntil已经超过里面的Cycle，或者，cycle的Ready已经被读取过，那么删除它。这样避免它
持续的增长。在Debug模式下，检查Ready（cycle）访问必须是按照cycle单增的，不过可以跳过，例如，1,3,4,5,10。用最简洁的话解释你的设计。
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
