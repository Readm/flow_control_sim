# ReadOnce Transaction Flow Analysis

## 基于ARM CHI协议文档的ReadOnce流程分析

### 基本信息

**ReadOnce** 是ARM CHI协议中的一种**非分配读取事务（Non-allocating Read transaction）**。

### 事务结构

根据文档第60页（B2.3 Transaction structure），ReadOnce事务的基本结构：

1. **事务开始**：Requester向Home发送ReadOnce请求
2. **请求字段**：包含以下影响事务流程的字段：
   - `Order`: 排序字段
   - `ExpCompAck`: 期望完成确认字段

### ReadOnce的5种主要流程（Alternatives 1-5）

根据文档第59-61页（B2.3 Transaction structure, Figure B2.2: Non-allocating Read），Home处理ReadOnce事务有**5种主要的替代流程**：

#### Alternative 1: Combined response from Home
- **流程**：
  1. 如果原始请求有排序要求（Order != 00），Home可选返回ReadReceipt给Requester
  2. Home返回组合响应和读取数据（CompData）给Requester
- **特点**：最简单直接的流程，Home本地有数据可直接返回
- **适用场景**：Home节点缓存中有所需数据

#### Alternative 2: Separate Data and Response from Home
- **流程**：
  1. Home先发送分离的响应（RespSepData）给Requester
  2. 然后发送数据（DataSepResp）给Requester
- **特点**：响应和数据分离发送
- **限制**：如果请求有排序要求且不需要完成确认，不能使用此流程

#### Alternative 3: Combined response from Subordinate
- **流程**：
  1. 如果原始请求有排序要求，Home可选返回ReadReceipt给Requester
  2. Home向下游发送读取请求（ReadNoSnp）给Subordinate
  3. 如果Home请求ReadReceipt，Subordinate可选返回ReadReceipt给Home
  4. Subordinate返回组合响应和数据（CompData）给Requester
- **特点**：数据来自Subordinate，但响应和数据组合在一起直接发送给Requester
- **限制**：如果请求有排序要求且不需要完成确认，不能使用此流程

#### Alternative 4: Response from Home, Data from Subordinate
- **流程**：
  1. Home返回分离的响应（RespSepData）给Requester
  2. Home向下游发送仅数据请求（ReadNoSnpSep）给Subordinate
  3. 如果Home请求ReadReceipt，Subordinate可选返回ReadReceipt给Home
  4. Subordinate返回读取数据（DataSepResp）给Requester
- **特点**：响应来自Home，数据来自Subordinate，数据直接从Subordinate到Requester
- **优势**：减少数据在Home节点的中转

#### Alternative 5: Forwarding snoop（转发窥探）
- **流程**：
  1. 如果原始请求有排序要求，Home可选返回ReadReceipt给Requester
  2. Home请求Snoopee转发读取数据（Snp*Fwd）给Requester
  3. Snoopee有4种处理方式（子流程5a-5d）
- **特点**：涉及缓存一致性，需要从其他缓存节点获取数据

**Alternative 5的子流程：**

- **5a. With response to Home（响应给Home）**
  - Snoopee提供组合响应和数据（CompData）给Requester
  - Snoopee提供窥探响应（SnpRespFwded）给Home

- **5b. With data to Home（数据给Home）**
  - Snoopee提供组合响应和数据（CompData）给Requester
  - Snoopee提供带数据的窥探响应（SnpRespDataFwded）给Home

- **5c. Failed, must use alternative（失败，必须使用替代流程）**
  - Snoopee提供窥探响应（SnpResp）给Home
  - Home必须使用本节的另一个替代流程来完成事务

- **5d. Failed, must use alternative（失败，必须使用替代流程）**
  - Snoopee提供带数据的窥探响应（SnpRespData或SnpRespDataPtl）给Home
  - Home必须使用本节的另一个替代流程来完成事务

### 流程图说明

文档中的流程图显示了以下节点：
- **Requester (RN)**: 请求节点，发起ReadOnce请求
- **Home (HN)**: 主节点，处理请求
- **Subordinate (SN)**: 从属节点，可能存储数据
- **Snoopee**: 被窥探的节点

### 可选步骤

- **ReadReceipt**: 当Order字段不为00时，可选发送的接收确认

### 使用场景

根据文档B2.3.1.1节（AllocatingRead）的描述，不同替代流程的典型用途：
- **Alternative 1-2**: Home本地有数据时使用
- **Alternative 3-4**: 需要从Subordinate获取数据时使用
- **Alternative 5**: 需要维护缓存一致性时使用

### 相关事务类型

ReadOnce属于非分配读取事务，同类型的事务还包括：
- ReadNoSnp
- ReadOnceCleanInvalid
- ReadOnceMakeInvalid

### 响应类型

根据文档第371页的表格，ReadOnce可能的响应包括：
- **OK**: 正常响应
- **EXOK**: 独占响应（不允许）
- **DERR**: 数据错误
- **NDERR**: 节点错误

### 注意事项

1. ReadOnce是**非分配**读取，不会在缓存中分配新的缓存行
2. 流程选择取决于：
   - Home是否有数据
   - 是否需要从Subordinate获取
   - 是否需要维护缓存一致性
   - Order字段的值
3. 某些流程可能涉及多个节点，需要协调响应和数据传输

### CompAck要求

如果原始请求设置了`ExpCompAck = 1`，Requester必须在以下情况之一后提供CompAck响应：
- 至少收到一个CompData数据包
- 收到RespSepData（如果请求没有排序要求），可以但不必须等待DataSepResp
- 收到RespSepData和至少一个DataSepResp数据包（如果请求有排序要求）

如果原始请求有排序要求，Requester可以但不必须等待ReadReceipt后再发送CompAck。

### 总结

ReadOnce事务有**5种主要的可能流程**，其中第5种还有4个子流程，具体选择取决于：
- 数据的当前位置（Home、Subordinate或其他缓存）
- 是否需要缓存一致性操作
- Order和ExpCompAck字段的设置
- 系统的具体实现策略

这些流程提供了灵活性，允许系统根据实际情况选择最合适的路径来完成读取操作。

