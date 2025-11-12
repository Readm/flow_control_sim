# Config 模块

```
ConfigLoader → Validator → Defaults
                    ↓
              GeneratorFactory
                    ↓
             initializeNodes
```

- **Validator**：负责对 `Config` 进行显式校验与默认值填充，提前阻断非法配置。
- **GeneratorFactory**：根据校验后的参数创建概率或调度型请求生成器，供模拟器组件复用。***

## 插件配置

`Config` 结构新增 `Plugins` 字段，用于声明可选插件：

```go
cfg.Plugins.Incentives = []string{"random"}
```

- 数组元素对应 `hooks.Registry` 中注册的激励插件名称（统一以 `incentive/` 前缀约定）。
- 模拟器启动时会按声明顺序加载插件，实现无需修改核心骨架代码。


