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

