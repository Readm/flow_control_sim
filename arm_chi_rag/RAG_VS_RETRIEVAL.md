# RAG vs 纯检索：系统功能说明

## 当前系统状态

### ✅ 已实现：检索（Retrieval）
系统目前**只做检索**，返回相关的文档片段：
- 基于向量相似度搜索
- 返回top-k个相关文档块
- 包含相似度分数和元数据

### ⚠️ 未实现：生成（Generation）
系统**没有LLM生成回答**的功能：
- 只返回检索到的原始文档内容
- 不生成基于文档的综合回答
- 需要用户自己阅读和理解检索结果

## 系统架构对比

### 当前系统（纯检索）
```
用户问题 → 向量检索 → 返回相关文档片段
```

### 完整RAG系统（检索+生成）
```
用户问题 → 向量检索 → 检索相关文档 → LLM生成回答 → 返回回答+来源
```

## 已添加的生成功能

我已经为您添加了LLM生成功能的代码框架：

### 新增文件
1. `rag_system/generator.py` - 答案生成器
2. `rag_system/rag_chain.py` - RAG链（整合检索和生成）
3. `rag_system/llm_factory.py` - LLM工厂（支持多种LLM）

### 配置更新
`config.yaml` 中已添加LLM配置部分（默认禁用）

## 如何使用生成功能

### 方法1：使用OpenAI API

1. **修改 `config.yaml`**：
```yaml
generation:
  enabled: true
  provider: "openai"
  model_name: "gpt-3.5-turbo"
  api_key: "your-api-key-here"  # 替换为您的API密钥
  temperature: 0.7
  max_tokens: 1000
```

2. **安装依赖**：
```bash
pip install langchain-openai
```

3. **使用**：
```python
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()
result = rag.ask("ARM CHI协议的事务类型有哪些？")

print("回答：", result['answer'])
print("来源：", result['sources'])
```

### 方法2：使用本地模型（Ollama）

1. **安装Ollama**：
```bash
# 从 https://ollama.ai 下载安装
# 然后运行模型
ollama pull llama2
```

2. **修改 `config.yaml`**：
```yaml
generation:
  enabled: true
  provider: "ollama"
  model_name: "llama2"
  base_url: "http://localhost:11434"
  temperature: 0.7
```

3. **使用**：
```python
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()
result = rag.ask("ARM CHI协议的事务类型有哪些？")
print(result['answer'])
```

### 方法3：使用其他本地API

如果您的本地模型提供OpenAI兼容的API：

```yaml
generation:
  enabled: true
  provider: "openai"  # 使用OpenAI兼容接口
  model_name: "your-model-name"
  base_url: "http://localhost:8000/v1"  # 您的本地API地址
  api_key: "not-needed"  # 本地API可能不需要key
```

## 功能对比

### 纯检索模式（当前默认）

```python
rag = ARMCHIRAG()
results = rag.query("ARM CHI协议的事务类型")

# 返回：原始文档片段列表
for result in results:
    print(result['content'])  # 原始文档内容
```

**优点**：
- ✅ 快速
- ✅ 无需API密钥
- ✅ 完全本地运行
- ✅ 返回原始文档，可追溯

**缺点**：
- ❌ 需要用户自己理解文档
- ❌ 可能返回多个不相关的片段
- ❌ 没有综合性的回答

### RAG模式（检索+生成）

```python
rag = ARMCHIRAG()  # 需要配置LLM
result = rag.ask("ARM CHI协议的事务类型有哪些？")

# 返回：生成的回答 + 来源
print(result['answer'])      # LLM生成的综合回答
print(result['sources'])     # 来源文档信息
```

**优点**：
- ✅ 生成综合性的回答
- ✅ 更易理解
- ✅ 基于文档内容，准确可靠
- ✅ 包含来源信息，可追溯

**缺点**：
- ❌ 需要LLM（API密钥或本地模型）
- ❌ 生成速度较慢
- ❌ 可能产生幻觉（需要好的提示词）

## 推荐使用方式

### 场景1：快速查找文档片段
使用纯检索模式：
```python
results = rag.query("缓存一致性协议")
```

### 场景2：需要综合回答
使用RAG模式（需要配置LLM）：
```python
result = rag.ask("ARM CHI协议的事务类型有哪些？")
print(result['answer'])
```

### 场景3：获取上下文用于其他用途
```python
context = rag.get_context("缓存一致性协议", max_length=2000)
# 可以将context传递给其他LLM或系统
```

## 总结

- **当前系统**：只有检索（Retrieval），没有生成（Generation）
- **已添加代码**：生成功能的完整框架
- **需要配置**：LLM才能启用生成功能
- **推荐**：根据需求选择使用模式

如果只需要快速查找文档，当前系统已足够。如果需要生成综合回答，需要配置LLM。

