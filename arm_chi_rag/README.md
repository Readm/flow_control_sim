# ARM CHI RAG系统 - Agent使用说明

## 快速使用

在Cursor中使用`@`符号引用这个RAG系统：

```
@arm_chi_rag 请查询ReadOnce事务的所有可能流程
```

或者引用特定文件：

```
@arm_chi_rag/cursor_integration.py 如何使用这个RAG系统？
```

## 主要功能

1. **查询ARM CHI协议文档**：通过语义搜索查找相关文档内容
2. **提取图片和图表**：从PDF中提取图片和图表信息
3. **提供上下文检索**：获取相关文档片段作为上下文

## 核心接口

### Python接口

```python
from cursor_integration import ARMCHIRAG, query_arm_chi, get_arm_chi_context

# 方式1: 使用封装类
rag = ARMCHIRAG()
results = rag.query("ReadOnce transaction flow", top_k=5)

# 方式2: 使用便捷函数
results = query_arm_chi("ReadOnce transaction flow", top_k=5)
context = get_arm_chi_context("ReadOnce transaction flow", max_length=2000)
```

## 常用查询

- "ReadOnce有哪些可能的流程？"
- "ARM CHI协议的事务类型有哪些？"
- "缓存一致性协议的工作原理"
- "ReadOnce的5种替代流程"

## 文件说明

- `cursor_integration.py` - Python集成接口（主要使用这个）
- `config.yaml` - 配置文件
- `vector_db/` - 向量数据库（已索引的文档数据）
- `rag_system/` - 核心模块（vector_store.py, retriever.py）
- `data/` - 原始PDF文档（可选，已索引后不需要）

## 在Agent中使用

当Agent看到`@arm_chi_rag`时，应该：

1. 读取`cursor_integration.py`了解如何使用
2. 使用Python代码调用RAG系统查询
3. 返回查询结果或基于结果回答问题

## 示例

```
用户: @arm_chi_rag ReadOnce有哪些可能的流程？

Agent应该:
1. 读取arm_chi_rag/cursor_integration.py
2. 执行: query_arm_chi("ReadOnce transaction flow", top_k=10)
3. 分析结果并回答用户问题
```

