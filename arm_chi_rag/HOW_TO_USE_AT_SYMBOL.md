# 如何在Cursor Agent中使用@符号引用ARM CHI RAG系统

## 快速开始

在Cursor的Agent对话中，使用 `@` 符号可以引用文件、文件夹或代码片段。以下是几种使用RAG系统的方法：

## 方法1: 引用集成文件（最简单）

在Agent对话中直接输入：

```
@cursor_integration.py 请帮我查询ReadOnce事务的流程
```

或者：

```
@cursor_integration.py 如何使用这个RAG系统查询ARM CHI文档？
```

Agent会自动读取 `cursor_integration.py` 文件，了解如何使用RAG系统。

## 方法2: 引用整个项目文件夹

如果你想引用整个RAG项目：

```
@~/arm_chi_rag 请帮我查询ReadOnce事务的所有可能流程
```

或者使用相对路径（如果在其他项目中）：

```
@../arm_chi_rag 如何使用这个RAG系统？
```

## 方法3: 引用特定的文档或示例

### 引用使用示例
```
@cursor_usage_example.py 请参考这个示例，帮我查询ReadOnce流程
```

### 引用集成指南
```
@CURSOR_INTEGRATION.md 根据这个指南，如何在代码中使用RAG系统？
```

### 引用分析报告
```
@READONCE_FLOWS_ANALYSIS.md 基于这个分析，ReadOnce有哪些流程？
```

## 方法4: 在代码中使用（推荐）

创建一个便于引用的包装文件：

```python
# 在其他项目中创建 arm_chi_rag_helper.py
import sys
from pathlib import Path

# 添加RAG项目路径
rag_project_path = Path.home() / "arm_chi_rag"
sys.path.insert(0, str(rag_project_path))

from cursor_integration import ARMCHIRAG, get_arm_chi_context, query_arm_chi

def query_arm_chi_docs(query: str, top_k: int = 5):
    """
    查询ARM CHI文档的便捷函数
    
    Args:
        query: 查询文本
        top_k: 返回结果数量
    
    Returns:
        查询结果列表
    """
    return query_arm_chi(query, top_k)

def get_arm_chi_doc_context(query: str, max_length: int = 2000):
    """
    获取ARM CHI文档上下文的便捷函数
    
    Args:
        query: 查询文本
        max_length: 最大上下文长度
    
    Returns:
        上下文字符串
    """
    return get_arm_chi_context(query, max_length)
```

然后在Agent中引用：

```
@arm_chi_rag_helper.py 请使用这个RAG系统查询ReadOnce流程
```

## 实际使用示例

### 示例1: 在Agent对话中查询

```
@cursor_integration.py 

请帮我查询ReadOnce事务的所有可能流程，并解释每种流程的适用场景。
```

### 示例2: 在代码生成中使用

```
@cursor_integration.py 

请基于这个RAG系统，帮我写一个函数来查询ARM CHI协议的事务类型。
```

### 示例3: 结合多个文件

```
@cursor_integration.py @CURSOR_INTEGRATION.md 

请参考这些文件，告诉我如何在当前项目中使用ARM CHI RAG系统。
```

## 最佳实践

### 1. 创建快捷引用文件

在RAG项目根目录创建一个 `QUICK_REFERENCE.md`：

```markdown
# ARM CHI RAG 快速参考

## 基本使用

```python
from cursor_integration import get_arm_chi_context

# 获取上下文
context = get_arm_chi_context("你的问题", max_length=2000)
```

## 常用查询

- ReadOnce流程: `get_arm_chi_context("ReadOnce transaction flow")`
- 事务类型: `get_arm_chi_context("ARM CHI transaction types")`
- 缓存一致性: `get_arm_chi_context("cache coherence protocol")`
```

然后引用：

```
@QUICK_REFERENCE.md 请参考这个快速参考
```

### 2. 使用相对路径引用

如果你的项目在 `~/flow_sim`，RAG在 `~/arm_chi_rag`：

```
@../arm_chi_rag/cursor_integration.py 请使用这个RAG系统
```

### 3. 引用配置文件

如果需要了解配置：

```
@config.yaml 请查看RAG系统的配置
```

## 常见问题

### Q: @符号找不到文件怎么办？

A: 确保使用正确的路径：
- 绝对路径: `@/home/readm/arm_chi_rag/cursor_integration.py`
- 相对路径: `@../arm_chi_rag/cursor_integration.py`
- 家目录: `@~/arm_chi_rag/cursor_integration.py`

### Q: 如何引用整个文件夹？

A: 引用文件夹路径：
```
@~/arm_chi_rag
```

### Q: 可以在代码中直接使用吗？

A: 可以！在代码中导入：

```python
import sys
sys.path.append('/home/readm/arm_chi_rag')
from cursor_integration import get_arm_chi_context

context = get_arm_chi_context("ReadOnce flow")
```

然后在Agent中引用这个代码文件。

## 完整工作流程示例

1. **在Agent对话中**：
   ```
   @cursor_integration.py 请帮我查询ReadOnce的所有流程
   ```

2. **Agent会**：
   - 读取 `cursor_integration.py`
   - 理解如何使用RAG系统
   - 可能自动调用RAG系统查询
   - 或者告诉你如何使用

3. **你可以在代码中使用**：
   ```python
   from cursor_integration import get_arm_chi_context
   context = get_arm_chi_context("ReadOnce flow")
   print(context)
   ```

4. **然后继续问Agent**：
   ```
   基于这个上下文，请解释ReadOnce的5种流程
   ```

## 总结

使用 `@` 符号引用RAG系统的方法：

1. ✅ **引用集成文件**: `@cursor_integration.py`
2. ✅ **引用项目文件夹**: `@~/arm_chi_rag`
3. ✅ **引用文档**: `@CURSOR_INTEGRATION.md`
4. ✅ **引用示例**: `@cursor_usage_example.py`
5. ✅ **组合引用**: `@cursor_integration.py @README.md`

记住：Agent会读取被引用的文件内容，理解如何使用RAG系统，然后帮助你完成查询任务！

