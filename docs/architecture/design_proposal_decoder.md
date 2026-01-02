# NoC Decoder 设计方案

## 1. 核心接口

### 1.1 AddressMapper - 地址到目标节点映射

```go
// AddressMapper 将地址映射到目标节点（最终目的地）
// 这是一个全局共享的组件，定义了地址空间的划分
type AddressMapper interface {
    // MapAddress 返回处理该地址的目标节点 ID
    // 例如：地址 0x1000 → Memory Controller 0
    MapAddress(addr uint64) (targetNodeID int, attributes map[string]interface{})
}

// 示例实现：Interleaved Memory Mapping
type InterleavedAddressMapper struct {
    memoryNodeIDs []int   // Memory Controller 节点 IDs
    granularity   uint64  // 交错粒度（字节）
}

func (m *InterleavedAddressMapper) MapAddress(addr uint64) (int, map[string]interface{}) {
    index := (addr / m.granularity) % uint64(len(m.memoryNodeIDs))
    return m.memoryNodeIDs[index], map[string]interface{}{
        "MemoryIndex": index,
    }
}
```

### 1.2 Topology - 网络拓扑结构

```go
// Topology 描述网络拓扑结构
type Topology interface {
    // GetNeighbors 返回某个节点的所有邻居节点
    GetNeighbors(nodeID int) []int

    // GetDistance 返回两个节点之间的距离（跳数）
    GetDistance(fromID, toID int) int

    // GetAllNodes 返回所有节点 ID
    GetAllNodes() []int
}

// 示例：2D Mesh Topology
type MeshTopology struct {
    rows    int
    cols    int
    nodeIDs [][]int  // nodeIDs[row][col] = nodeID
}

func (t *MeshTopology) GetNeighbors(nodeID int) []int {
    row, col := t.nodeIDToCoord(nodeID)
    neighbors := []int{}

    // North, East, South, West
    if row > 0 {
        neighbors = append(neighbors, t.nodeIDs[row-1][col])
    }
    if col < t.cols-1 {
        neighbors = append(neighbors, t.nodeIDs[row][col+1])
    }
    if row < t.rows-1 {
        neighbors = append(neighbors, t.nodeIDs[row+1][col])
    }
    if col > 0 {
        neighbors = append(neighbors, t.nodeIDs[row][col-1])
    }

    return neighbors
}
```

### 1.3 RoutingAlgorithm - 路由算法

```go
// RoutingAlgorithm 决定从当前节点到目标节点的下一跳
// 支持静态路由（routing table）和动态路由（adaptive）
type RoutingAlgorithm interface {
    // ComputeNextHop 计算下一跳节点
    // 参数：
    //   - currentNodeID: 当前节点 ID
    //   - targetNodeID: 目标节点 ID
    //   - networkState: 网络状态（用于动态路由）
    // 返回：
    //   - nextHopNodeID: 下一跳节点 ID
    //   - alternatives: 备选路径（用于自适应路由）
    ComputeNextHop(
        currentNodeID int,
        targetNodeID int,
        networkState *NetworkState,
    ) (nextHopNodeID int, alternatives []int)
}

// NetworkState 网络状态（用于动态路由）
type NetworkState struct {
    // 每条链路的拥塞程度 (0.0 - 1.0)
    LinkUtilization map[LinkID]float64

    // 每个节点的缓冲区占用
    BufferOccupancy map[int]float64
}

// 示例：XY Routing (静态，确定性)
type XYRoutingAlgorithm struct {
    topology *MeshTopology
}

func (r *XYRoutingAlgorithm) ComputeNextHop(
    currentNodeID int,
    targetNodeID int,
    networkState *NetworkState,
) (int, []int) {
    curRow, curCol := r.topology.nodeIDToCoord(currentNodeID)
    tgtRow, tgtCol := r.topology.nodeIDToCoord(targetNodeID)

    // XY Routing: 先走 X 方向，再走 Y 方向
    if curCol < tgtCol {
        return r.topology.nodeIDs[curRow][curCol+1], nil  // East
    } else if curCol > tgtCol {
        return r.topology.nodeIDs[curRow][curCol-1], nil  // West
    } else if curRow < tgtRow {
        return r.topology.nodeIDs[curRow+1][curCol], nil  // South
    } else if curRow > tgtRow {
        return r.topology.nodeIDs[curRow-1][curCol], nil  // North
    }

    return currentNodeID, nil  // 已到达目标
}

// 示例：Adaptive Routing (动态，考虑拥塞)
type AdaptiveRoutingAlgorithm struct {
    topology *MeshTopology
    baseAlgorithm RoutingAlgorithm  // 基础路由算法（如 XY）
}

func (r *AdaptiveRoutingAlgorithm) ComputeNextHop(
    currentNodeID int,
    targetNodeID int,
    networkState *NetworkState,
) (int, []int) {
    // 1. 获取所有最小路径的候选下一跳
    candidates := r.getMinimalCandidates(currentNodeID, targetNodeID)

    // 2. 根据网络状态选择最优的下一跳
    bestNextHop := candidates[0]
    minUtilization := 1.0

    for _, nextHop := range candidates {
        linkID := LinkID{From: currentNodeID, To: nextHop}
        util := networkState.LinkUtilization[linkID]
        if util < minUtilization {
            minUtilization = util
            bestNextHop = nextHop
        }
    }

    return bestNextHop, candidates
}
```

### 1.4 NodeDecoder - 节点级路由决策器

```go
// NodeDecoder 是每个节点的路由决策器
// 每个节点有自己的 Decoder 实例
type NodeDecoder struct {
    nodeID          int
    addressMapper   AddressMapper       // 共享的地址映射器
    routingAlgorithm RoutingAlgorithm   // 路由算法
    linkMapper      *LinkMapper         // 本地的链路映射器
    networkState    *NetworkState       // 网络状态（用于动态路由）
}

// LinkMapper 将下一跳节点 ID 映射到输出队列索引
type LinkMapper struct {
    // nextHopID → outputQueueIndex
    nodeIDToQueueIndex map[int]int
}

// DecodePacket 解析数据包，返回应该发送到哪个输出队列
func (d *NodeDecoder) DecodePacket(pkt packet.Packet, addr uint64) (queueIndex int, err error) {
    // 1. 地址映射：确定最终目标节点
    targetNodeID, _ := d.addressMapper.MapAddress(addr)

    // 2. 如果已经到达目标，返回本地处理
    if targetNodeID == d.nodeID {
        return LOCAL_QUEUE_INDEX, nil
    }

    // 3. 路由算法：计算下一跳
    nextHopNodeID, _ := d.routingAlgorithm.ComputeNextHop(
        d.nodeID,
        targetNodeID,
        d.networkState,
    )

    // 4. 链路映射：下一跳节点 → 输出队列索引
    queueIndex, exists := d.linkMapper.nodeIDToQueueIndex[nextHopNodeID]
    if !exists {
        return -1, fmt.Errorf("no route to node %d from node %d", nextHopNodeID, d.nodeID)
    }

    return queueIndex, nil
}
```

---

## 2. 自动化配置方案

### 2.1 拓扑构建器 (Topology Builder)

```go
// TopologyBuilder 自动构建拓扑和路由
type TopologyBuilder interface {
    // Build 构建拓扑，返回所有节点的 Decoder
    Build() (decoders map[int]*NodeDecoder, err error)
}

// 示例：Mesh 拓扑构建器
type MeshTopologyBuilder struct {
    rows            int
    cols            int
    memoryNodeIDs   []int           // Memory 节点的位置
    routingAlgo     string          // "XY", "Adaptive", etc.
    granularity     uint64          // 地址交错粒度
}

func (b *MeshTopologyBuilder) Build() (map[int]*NodeDecoder, error) {
    // 1. 创建拓扑
    topology := NewMeshTopology(b.rows, b.cols)

    // 2. 创建地址映射器
    addressMapper := &InterleavedAddressMapper{
        memoryNodeIDs: b.memoryNodeIDs,
        granularity:   b.granularity,
    }

    // 3. 创建路由算法
    var routingAlgo RoutingAlgorithm
    switch b.routingAlgo {
    case "XY":
        routingAlgo = &XYRoutingAlgorithm{topology: topology}
    case "Adaptive":
        routingAlgo = &AdaptiveRoutingAlgorithm{topology: topology}
    }

    // 4. 为每个节点创建 Decoder
    decoders := make(map[int]*NodeDecoder)
    networkState := &NetworkState{
        LinkUtilization: make(map[LinkID]float64),
        BufferOccupancy: make(map[int]float64),
    }

    for _, nodeID := range topology.GetAllNodes() {
        // 创建链路映射器
        linkMapper := b.createLinkMapper(topology, nodeID)

        decoders[nodeID] = &NodeDecoder{
            nodeID:           nodeID,
            addressMapper:    addressMapper,
            routingAlgorithm: routingAlgo,
            linkMapper:       linkMapper,
            networkState:     networkState,
        }
    }

    return decoders, nil
}

func (b *MeshTopologyBuilder) createLinkMapper(topology *MeshTopology, nodeID int) *LinkMapper {
    neighbors := topology.GetNeighbors(nodeID)
    nodeIDToQueueIndex := make(map[int]int)

    // 假设输出队列按照 North, East, South, West 顺序排列
    for i, neighborID := range neighbors {
        nodeIDToQueueIndex[neighborID] = i
    }

    return &LinkMapper{nodeIDToQueueIndex: nodeIDToQueueIndex}
}
```

### 2.2 声明式配置（YAML/JSON）

```yaml
# network_config.yaml
topology:
  type: mesh
  rows: 4
  cols: 4

address_mapping:
  type: interleaved
  granularity: 64  # bytes (cache line size)
  memory_nodes: [12, 13, 14, 15]  # 底部一行是 Memory Controllers

routing:
  algorithm: adaptive
  base_algorithm: xy

nodes:
  # CPU nodes (0-11)
  - id_range: [0, 11]
    type: cpu

  # Memory Controller nodes (12-15)
  - id_range: [12, 15]
    type: memory_controller
```

加载配置：

```go
func LoadNetworkConfig(configFile string) (map[int]*NodeDecoder, error) {
    config := parseYAML(configFile)

    var builder TopologyBuilder
    switch config.Topology.Type {
    case "mesh":
        builder = &MeshTopologyBuilder{
            rows:          config.Topology.Rows,
            cols:          config.Topology.Cols,
            memoryNodeIDs: config.AddressMapping.MemoryNodes,
            routingAlgo:   config.Routing.Algorithm,
            granularity:   config.AddressMapping.Granularity,
        }
    case "ring":
        builder = &RingTopologyBuilder{...}
    // 更多拓扑类型...
    }

    return builder.Build()
}
```

---

## 3. 使用示例

### 3.1 在节点中使用 Decoder

```go
type CPUNodeHandler struct {
    nodeID       int
    decoder      *NodeDecoder
    outputQueues []*queue.OutputQueue
}

func (h *CPUNodeHandler) sendRequest(cycle uint64, addr uint64) error {
    // 1. 使用 Decoder 决定路由
    queueIndex, err := h.decoder.DecodePacket(nil, addr)
    if err != nil {
        return err
    }

    // 2. 发送到对应的输出队列
    pkt := NewMemoryRequestPacket(h.nodeID, addr)
    h.outputQueues[queueIndex].InjectPackets(int(cycle), []packet.Packet{pkt})

    return nil
}
```

### 3.2 网络状态更新（动态路由）

```go
// 在 Network Tick 时更新网络状态
func (net *Network) UpdateNetworkState(cycle uint64) {
    for linkID, link := range net.links {
        // 计算链路利用率
        utilization := float64(link.GetOccupancy()) / float64(link.GetCapacity())
        net.networkState.LinkUtilization[linkID] = utilization
    }
}
```

---

## 4. 优势

###  模块化
- Address Mapper, Routing Algorithm, Topology 独立实现
- 每个组件可以单独测试和替换

###  可扩展
- 支持任意拓扑（Mesh, Ring, Tree, Custom）
- 支持任意路由算法（静态、动态、自适应）
- 支持任意地址映射策略

###  自动化
- TopologyBuilder 自动构建整个网络
- 声明式配置文件
- 用户只需指定拓扑类型和参数

###  动态路由
- NetworkState 跟踪网络状态
- 路由算法可以访问实时状态
- 支持自适应路由和负载均衡

###  每节点独立
- 每个节点有自己的 Decoder 实例
- 但共享 AddressMapper 和 NetworkState

---

## 5. 待讨论的问题

1. **路由表 vs 在线计算**
   - 对于静态路由（如 XY），是否预先生成路由表？
   - 还是每次都在线计算（更灵活但可能慢）？

2. **动态路由的全局协调**
   - NetworkState 如何高效更新？
   - 是否需要中心化的状态管理？

3. **多级缓存的地址映射**
   - L2 Slice 的映射是否和 Memory 一致？
   - 还是需要多个 AddressMapper？

4. **虚拟通道 (Virtual Channel)**
   - 是否需要支持虚拟通道以避免死锁？
   - 如何在 Decoder 中表达？

5. **配置的复杂度**
   - 声明式配置能否覆盖所有场景？
   - 还是需要编程式 API？
