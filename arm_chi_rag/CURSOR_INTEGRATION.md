# Cursor集成指南

## 为什么只需要检索部分？

**是的，对于Cursor，只需要检索部分即可！**

原因：
1. ✅ **Cursor本身就有LLM**：Cursor内置了Claude/GPT等大模型
2. ✅ **检索提供上下文**：RAG系统检索相关文档片段
3. ✅ **Cursor的LLM处理**：Cursor使用自己的LLM基于检索到的文档理解和回答

工作流程：
```
你的问题 → RAG检索 → 返回相关文档片段 → Cursor的LLM → 生成回答
```

## 在Cursor中使用RAG系统

### 方法1：直接导入（推荐）

在Cursor的代码中直接使用：

```python
from cursor_integration import ARMCHIRAG, get_arm_chi_context, query_arm_chi

# 方式1：获取上下文，让Cursor的LLM处理
rag = ARMCHIRAG()
context = rag.get_context("ARM CHI协议的事务类型", max_length=2000)

# 将上下文提供给Cursor
# Cursor会自动使用这些上下文来回答你的问题
print(context)
```

**使用场景**：
- 在Cursor中提问："ARM CHI协议的事务类型有哪些？"
- 同时提供检索到的文档上下文
- Cursor的LLM会基于这些上下文生成准确回答

### 方法2：在代码中查询

```python
from cursor_integration import query_arm_chi

# 查询相关文档
results = query_arm_chi("缓存一致性协议", top_k=5)

# 将结果提供给Cursor
for result in results:
    print(f"来源: {result['file_name']} 第 {result['page']} 页")
    print(f"内容: {result['content']}\n")
```

### 方法3：通过API服务（适合多项目共享）

```bash
# 启动API服务
python main.py api
```

在Cursor中使用：

```python
import requests

def get_arm_chi_context(query: str, top_k: int = 5):
    """获取ARM CHI文档上下文"""
    response = requests.post(
        "http://127.0.0.1:8000/query",
        json={"query": query, "top_k": top_k}
    )
    data = response.json()
    
    # 合并所有检索到的文档内容
    context = "\n\n".join([
        f"来源: {r['file_name']} 第 {r['page']} 页\n内容: {r['content']}"
        for r in data["results"]
    ])
    return context

# 使用
context = get_arm_chi_context("ARM CHI协议的事务类型")
print(context)
# 然后让Cursor基于这个上下文回答你的问题
```

## 实际使用示例

### 示例1：在Cursor中查询文档

```python
# 在Cursor的代码编辑器中
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()

# 查询你需要的文档内容
query = "ARM CHI协议的事务类型有哪些？"
results = rag.query(query, top_k=3)

# 显示检索结果
print("检索到的相关文档：")
for i, result in enumerate(results, 1):
    print(f"\n文档 {i}:")
    print(f"  来源: {result['file_name']} 第 {result['page']} 页")
    print(f"  相似度: {result['score']:.4f}")
    print(f"  内容: {result['content'][:300]}...")

# 现在你可以在Cursor中提问，Cursor会基于这些文档内容回答
```

### 示例2：获取上下文用于代码生成

```python
from cursor_integration import get_arm_chi_context

# 获取关于某个主题的上下文
context = get_arm_chi_context("缓存一致性协议", max_length=2000)

# 在Cursor中，你可以这样提问：
# "基于以下ARM CHI文档内容，帮我实现一个缓存一致性协议的模拟器"
# 然后粘贴context内容
print(context)
```

### 示例3：在Cursor Chat中使用

在Cursor的Chat界面中：

1. **提问**："ARM CHI协议的事务类型有哪些？"

2. **同时提供文档上下文**：
```python
# 先运行这个获取上下文
from cursor_integration import get_arm_chi_context
context = get_arm_chi_context("ARM CHI协议的事务类型", max_length=2000)
print(context)
```

3. **在Cursor Chat中**：
   - 粘贴检索到的文档内容
   - 然后提问："基于这些文档内容，ARM CHI协议的事务类型有哪些？"
   - Cursor的LLM会基于这些文档生成准确回答

## 最佳实践

### 1. 只使用检索功能

```python
# ✅ 推荐：只使用检索
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()
results = rag.query("你的问题", top_k=5)
context = rag.get_context("你的问题", max_length=2000)

# ❌ 不需要：LLM生成功能
# result = rag.ask("你的问题")  # 不需要，Cursor有自己的LLM
```

### 2. 配置建议

在 `config.yaml` 中：
```yaml
# ✅ 保持这样即可（generation.enabled: false）
generation:
  enabled: false  # 不需要启用，Cursor有自己的LLM
```

### 3. 使用流程

1. **检索文档**：使用 `query()` 或 `get_context()` 获取相关文档
2. **提供给Cursor**：将检索结果作为上下文
3. **Cursor处理**：Cursor的LLM基于这些文档回答

## 为什么不需要生成功能？

| 功能 | 需要吗？ | 原因 |
|------|---------|------|
| 检索（Retrieval） | ✅ **需要** | 找到相关文档片段 |
| 生成（Generation） | ❌ **不需要** | Cursor本身就有LLM |

**总结**：
- RAG系统：负责检索相关文档 ✅
- Cursor的LLM：负责理解和生成回答 ✅
- 不需要额外的LLM生成功能 ❌

## 快速开始

```python
# 最简单的使用方式
from cursor_integration import get_arm_chi_context

# 获取上下文
context = get_arm_chi_context("你的问题", max_length=2000)

# 在Cursor中使用这个上下文
# Cursor会自动基于这些文档内容回答你的问题
print(context)
```

## 总结

**对于Cursor，只需要检索部分即可！**

- ✅ 使用 `query()` 或 `get_context()` 检索文档
- ✅ 将检索结果提供给Cursor
- ✅ Cursor的LLM会基于这些文档生成回答
- ❌ 不需要配置额外的LLM生成功能

