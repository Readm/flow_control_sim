<!-- cc28aac8-a183-46bd-8c14-0e5484f08c4b 3722e942-2fd8-4f96-8aee-46c117e990d6 -->
# Channel 状态可视化实现计划

## 目标

在贝塞尔曲线上显示每个 channel 的 pipeline 状态：

- N 组点（对应 latency cycles），沿曲线均匀分布
- 每组 M 个点（对应 bandwidth），垂直于曲线方向排列
- 第一组表示 Slot[0]（上一 cycle push 进来的），最后一组表示 Slot[latency-1]（这个 cycle 可以被下游使用）
- 有 packet 的点为红色，无 packet 的点为灰色

## 实现步骤

### 1. 后端：扩展 EdgeSnapshot 结构

**文件**: `visualization.go`

- 在 `EdgeSnapshot` 中添加 `BandwidthLimit int` 字段，用于传递带宽信息给前端

### 2. 后端：在 buildFrame 中填充 BandwidthLimit

**文件**: `simulator.go`

- 在 `buildFrame` 方法中，为每个 `EdgeSnapshot` 设置 `BandwidthLimit` 字段
- 从 `s.cfg.BandwidthLimit` 获取带宽值

### 3. 前端：计算贝塞尔曲线路径点

**文件**: `web/static/index.html`

- 使用 Cytoscape.js 的 edge 渲染 API 获取贝塞尔曲线的控制点和路径
- 计算曲线上的 N 个等分点（对应 N 个 latency cycles），均匀分布在曲线上
- 在每个等分点计算垂直于曲线的方向，用于排列 M 个点

### 4. 前端：绘制状态点

**文件**: `web/static/index.html`

- 在 edge 更新时，根据 `pipelineStages` 和 `bandwidthLimit` 绘制点
- 每组对应一个 slot（StageIndex），每个 slot 中：
- 前 `PacketCount` 个点为红色（有 packet）
- 剩余 `(M - PacketCount)` 个点为灰色（无 packet）
- 点在贝塞尔曲线附近，垂直方向排列（垂直于曲线）

### 5. 前端：更新渲染逻辑

**文件**: `web/static/index.html`

- 在 `updateFrame` 函数中，每次更新 edge 时同时更新状态点
- 清理旧的 overlay 元素，重新绘制新的状态点
- 使用 SVG overlay 或 canvas overlay 在 edge 上绘制点

## 技术细节

### 贝塞尔曲线点位置计算

- 使用 Cytoscape.js 的 `cy.edges().renderedPosition()` 或 `cy.edges().renderedBoundingBox()` 获取 edge 的渲染位置
- 使用 Cytoscape.js 的 `cy.edges().midpoint()` 或自定义计算获取贝塞尔曲线上的等分点
- 计算曲线上的 N 个等分点（对应 N 个 latency cycles），均匀分布在曲线上
- 在每个等分点计算切线方向，然后计算垂直方向（旋转90度）
- 在垂直方向上排列 M 个点（对应 bandwidth），间距可设置为 3-5px

### 颜色映射规则

- 对于每个 slot（StageIndex），根据 `PacketCount` 决定点的颜色：
- 前 `PacketCount` 个点：红色 (`#ff4d4f`) - 表示有 packet
- 剩余 `(M - PacketCount)` 个点：灰色 (`#999` 或 `#d9d9d9`) - 表示无 packet
- 例如：如果 M=3，PacketCount=2，则显示 2 个红点 + 1 个灰点
- 点的大小建议设置为 4-6px，便于观察

### 实现方式

- 方案1：使用 SVG overlay，在 Cytoscape container 上叠加 SVG 元素
- 方案2：使用 Cytoscape 的 `cy.on('render')` 事件，在每次渲染时更新点位置
- 方案3：使用自定义 edge 样式，通过 Cytoscape 的扩展机制绘制点（较复杂）

### To-dos

- [ ] 在 EdgeSnapshot 中添加 BandwidthLimit 字段
- [ ] 在 simulator.go 的 buildFrame 中填充 BandwidthLimit 值
- [ ] 在前端实现贝塞尔曲线路径点计算函数
- [ ] 在前端实现状态点绘制函数（根据 pipelineStages 和 bandwidthLimit）
- [ ] 在 updateFrame 中集成状态点绘制和更新逻辑