# ARM CHI文档RAG系统

一个本地RAG（检索增强生成）系统，用于索引和查询ARM CHI文档，辅助CursorAgent编程。

## 功能特性

- 📄 **PDF文档处理**：支持批量处理PDF文档，自动提取文本和元数据
- 🔍 **智能检索**：基于向量相似度的语义搜索
- 💾 **本地部署**：完全本地运行，无需外部API密钥
- 🚀 **API服务**：提供RESTful API接口，方便集成
- ⚙️ **可配置**：通过YAML配置文件灵活调整参数

## 系统架构

```
文档处理层 → 向量化层 → 向量存储层 → 检索层 → API服务层
```

### 核心组件

1. **文档处理层**：PDF解析、文本分块、元数据提取
2. **向量化层**：使用sentence-transformers进行文本嵌入
3. **向量存储层**：ChromaDB向量数据库
4. **检索层**：相似度搜索、上下文检索
5. **服务层**：FastAPI RESTful API

## 安装步骤

### 1. 环境要求

- Python 3.8+
- pip 或 conda

### 2. 安装依赖

```bash
cd ~/arm_chi_rag
pip install -r requirements.txt
```

### 3. 准备文档

将ARM CHI文档（PDF格式）放入 `data/arm_chi_docs/` 目录：

```bash
mkdir -p data/arm_chi_docs
# 将PDF文件复制到此目录
cp /path/to/arm_chi_docs/*.pdf data/arm_chi_docs/
```

## 使用方法

### 命令行接口

#### 1. 索引文档

将PDF文档索引到向量数据库：

```bash
python main.py index data/arm_chi_docs
```

如果需要重置向量数据库：

```bash
python main.py index data/arm_chi_docs --reset
```

#### 2. 查询文档

查询相关文档：

```bash
python main.py query "ARM CHI协议的事务类型有哪些？"
```

指定返回结果数量：

```bash
python main.py query "缓存一致性协议" --top-k 10
```

#### 3. 启动API服务器

启动RESTful API服务：

```bash
python main.py api
```

服务器将在 `http://127.0.0.1:8000` 启动，API文档可在 `http://127.0.0.1:8000/docs` 查看。

### API接口

#### 健康检查

```bash
curl http://127.0.0.1:8000/health
```

#### 索引文档

```bash
curl -X POST "http://127.0.0.1:8000/index" \
  -H "Content-Type: application/json" \
  -d '{
    "directory_path": "data/arm_chi_docs",
    "reset": false
  }'
```

#### 查询文档

```bash
curl -X POST "http://127.0.0.1:8000/query" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "ARM CHI协议的事务类型",
    "top_k": 5
  }'
```

#### 获取集合信息

```bash
curl http://127.0.0.1:8000/info
```

## 配置说明

编辑 `config.yaml` 文件以调整系统参数：

```yaml
# 文档处理配置
document:
  chunk_size: 512          # 文本分块大小（tokens）
  chunk_overlap: 50         # 分块重叠大小
  pdf_parser: "pdfplumber"  # PDF解析器

# 向量化配置
embedding:
  model_name: "all-MiniLM-L6-v2"  # 嵌入模型
  device: "cpu"                    # cpu 或 cuda
  normalize_embeddings: true

# 向量数据库配置
vector_db:
  persist_directory: "./vector_db"
  collection_name: "arm_chi_docs"

# 检索配置
retrieval:
  top_k: 5              # 默认返回结果数
  score_threshold: 0.5  # 相似度阈值

# API服务配置
api:
  host: "127.0.0.1"
  port: 8000
  reload: false
```

### 嵌入模型选择

- **all-MiniLM-L6-v2**（默认）：轻量级，速度快，适合CPU
- **all-mpnet-base-v2**：高质量，速度较慢，适合GPU

修改 `config.yaml` 中的 `embedding.model_name` 来切换模型。

## Cursor集成

### 方法1：通过API调用（推荐）

在Cursor中，可以通过HTTP请求调用本地RAG服务：

```python
import requests

def query_arm_chi(query: str, top_k: int = 5):
    """查询ARM CHI文档"""
    response = requests.post(
        "http://127.0.0.1:8000/query",
        json={"query": query, "top_k": top_k}
    )
    return response.json()

# 使用示例
results = query_arm_chi("ARM CHI协议的事务类型")
for result in results["results"]:
    print(f"来源: {result['file_name']}")
    print(f"内容: {result['content']}\n")
```

### 方法2：直接导入模块

如果Cursor支持Python环境，可以直接导入：

```python
from rag_system.retriever import Retriever
from rag_system.vector_store import VectorStore
import yaml

# 加载配置
with open("config.yaml", "r") as f:
    config = yaml.safe_load(f)

# 初始化
vector_store = VectorStore(
    persist_directory=config["vector_db"]["persist_directory"],
    collection_name=config["vector_db"]["collection_name"],
    embedding_model_name=config["embedding"]["model_name"]
)

retriever = Retriever(
    vector_store=vector_store,
    top_k=config["retrieval"]["top_k"]
)

# 查询
results = retriever.retrieve_with_scores("你的查询")
```

### 方法3：使用封装类（最简单）

使用提供的 `cursor_integration.py`：

```python
from cursor_integration import ARMCHIRAG, query_arm_chi, get_arm_chi_context

# 方式1：使用封装类
rag = ARMCHIRAG()
results = rag.query("ARM CHI协议的事务类型")
context = rag.get_context("缓存一致性协议", max_length=1500)

# 方式2：使用便捷函数
results = query_arm_chi("ARM CHI协议", top_k=5)
context = get_arm_chi_context("缓存一致性协议")
```

## 项目结构

```
~/arm_chi_rag/
├── rag_system/              # 核心模块
│   ├── __init__.py
│   ├── document_processor.py    # PDF处理和分块
│   ├── vector_store.py          # 向量数据库操作
│   ├── retriever.py             # 检索逻辑
│   └── api_server.py            # FastAPI服务
├── data/
│   └── arm_chi_docs/            # ARM CHI文档目录
├── vector_db/                   # ChromaDB数据存储
├── requirements.txt             # 依赖包
├── config.yaml                  # 配置文件
├── main.py                      # 主程序入口
├── cursor_integration.py         # Cursor集成工具
└── README.md                    # 本文档
```

## 常见问题

### Q: 如何更新文档索引？

A: 删除 `vector_db/` 目录，然后重新运行索引命令：

```bash
rm -rf vector_db/
python main.py index data/arm_chi_docs
```

或者使用 `--reset` 参数：

```bash
python main.py index data/arm_chi_docs --reset
```

### Q: 如何提高检索质量？

A: 
1. 调整 `chunk_size` 和 `chunk_overlap` 参数
2. 使用更高质量的嵌入模型（如 `all-mpnet-base-v2`）
3. 降低 `score_threshold` 以返回更多结果
4. 增加 `top_k` 值

### Q: 支持哪些文档格式？

A: 目前仅支持PDF格式。如需支持其他格式，可以扩展 `document_processor.py`。

### Q: 如何加速向量化？

A: 如果有GPU，在 `config.yaml` 中设置：

```yaml
embedding:
  device: "cuda"
```

## 许可证

MIT License

## 贡献

欢迎提交Issue和Pull Request！

