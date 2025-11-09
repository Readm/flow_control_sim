# Queue 模块

```
TrackedQueue
  ├─ Hooks (enqueue/dequeue callbacks)
  ├─ MutateFunc (长度/容量同步)
  └─ Items (用于节点自定义逻辑)
```

- **TrackedQueue**：维护队列元素、容量约束以及事件钩子，供节点统一处理入队/出队逻辑。
- **QueueHooks**：在入队和出队时触发，负责记录事务事件或其他副作用。
- **MutateFunc**：每次长度或容量变化时调用，用于回写节点的 `QueueInfo` 信息。
