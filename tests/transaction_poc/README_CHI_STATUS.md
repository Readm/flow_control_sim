# CHI ReadShared 测试进展

## 已完成工作
- 在 `chi_readshared_test.go` 中构建了 Requester / Home / Subordinate 三个节点，每个节点拥有各自的 `TxnManager`、Cache/Directory 能力以及 `MockIncentiveHook` 驱动的事务逻辑。
- 为三个节点编写了基础的 CHI ReadShared 事务流程：
  - Requester 发送 `ReadShared` 请求并等待 `CompData`;
  - Home 收到 `ReadShared` 后向 Subordinate 转发 `ReadNoSnp` 并等待 `ReadReceipt`，随后更新目录状态；
  - Subordinate 收到 `ReadNoSnp` 后返回 `CompData` 给 Requester，同时向 Home 发送 `ReadReceipt`。
- 增加了多处调试输出，用于观察消息在简化模拟环境中的流转情况。

## 当前问题
- 虽然 Requester/Home/Subordinate 的事务逻辑均已启动，但在跑测试 (`go test ./tests/transaction_poc -run TestCHIReadSharedTransaction -timeout 30s -v`) 时，这三个事务始终未能完成：
  - Requester 发出 `ReadShared` 后一直等待 `CompData`；
  - Home 可以收到 `ReadShared` 并转发 `ReadNoSnp`，但自身也没有达到完成状态；
  - Subordinate 收到 `ReadNoSnp` 后会发送 `CompData` 和 `ReadReceipt`，但 Requester 端没有收到该 `CompData`。
- 最终测试报错 `transactions incomplete: requester=false home=false sub=false (active=1,1,1)`，说明消息路由或 Channel 匹配仍存在缺陷，`CompData`/`ReadReceipt` 未能正确投递到对应节点的事务。
- 在当前模拟框架（简化的 `runChiSimulation`）中需要进一步梳理按 Channel 的消息投递规则，确保各节点收发链路闭合，否则事务无法进入完成态。

