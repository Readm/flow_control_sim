# ARM CHI RAG 快速参考

## 快速使用

### 基本导入

```python
import sys
from pathlib import Path

# 添加RAG项目路径
rag_path = Path.home() / "arm_chi_rag"
sys.path.insert(0, str(rag_path))

from cursor_integration import ARMCHIRAG, get_arm_chi_context, query_arm_chi
```

### 最简单的使用方式

```python
from cursor_integration import get_arm_chi_context

# 获取上下文（推荐）
context = get_arm_chi_context("你的问题", max_length=2000)
print(context)
```

### 查询并获取结果

```python
from cursor_integration import query_arm_chi

# 查询并获取详细结果
results = query_arm_chi("ReadOnce transaction flow", top_k=5)

for result in results:
    print(f"来源: {result['file_name']} 第 {result['page']} 页")
    print(f"相似度: {result['score']:.4f}")
    print(f"内容: {result['content'][:300]}...\n")
```

### 使用类接口

```python
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()
results = rag.query("ReadOnce", top_k=5)
context = rag.get_context("ReadOnce flow", max_length=2000)
```

## 常用查询示例

### 查询事务类型
```python
context = get_arm_chi_context("ARM CHI transaction types", max_length=2000)
```

### 查询特定事务流程
```python
context = get_arm_chi_context("ReadOnce transaction flow alternatives", max_length=2000)
```

### 查询缓存一致性
```python
context = get_arm_chi_context("cache coherence protocol", max_length=2000)
```

### 查询协议细节
```python
context = get_arm_chi_context("snoop protocol flow", max_length=2000)
```

## 在Cursor Agent中使用

### 方法1: 直接引用文件
```
@cursor_integration.py 请帮我查询ReadOnce流程
```

### 方法2: 引用这个快速参考
```
@QUICK_REFERENCE.md 请参考这个文档使用RAG系统
```

### 方法3: 引用整个项目
```
@~/arm_chi_rag 请使用RAG系统查询ARM CHI文档
```

## API说明

### get_arm_chi_context(query, max_length=2000)
- **功能**: 获取查询的上下文（合并多个相关文档块）
- **参数**: 
  - `query`: 查询文本
  - `max_length`: 最大上下文长度
- **返回**: 合并后的上下文字符串

### query_arm_chi(query, top_k=5)
- **功能**: 查询相关文档
- **参数**:
  - `query`: 查询文本
  - `top_k`: 返回结果数量
- **返回**: 查询结果列表，每个结果包含：
  - `content`: 文档内容
  - `score`: 相似度分数
  - `file_name`: 文件名
  - `page`: 页码
  - `metadata`: 元数据

### ARMCHIRAG类
- `query(query, top_k)`: 查询文档
- `get_context(query, max_length)`: 获取上下文
- `format_results(query)`: 格式化查询结果
- `get_info()`: 获取向量数据库信息

## 注意事项

1. **路径设置**: 确保RAG项目路径正确
2. **配置文件**: 默认使用 `config.yaml`
3. **向量数据库**: 确保已索引文档（运行 `python main.py index`）
4. **性能**: 首次加载可能需要几秒钟初始化

## 故障排除

### 导入错误
```python
# 确保路径正确
import sys
sys.path.insert(0, '/home/readm/arm_chi_rag')
```

### 配置文件不存在
```python
# 指定配置文件路径
rag = ARMCHIRAG(config_path="/home/readm/arm_chi_rag/config.yaml")
```

### 向量数据库为空
```bash
# 重新索引文档
cd ~/arm_chi_rag
python main.py index data/arm_chi_docs --reset
```

