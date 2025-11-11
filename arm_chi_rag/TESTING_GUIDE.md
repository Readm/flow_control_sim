# 测试指南

## 快速测试指令

### 1. 基础功能测试（纯检索）

```bash
# 测试基本查询功能
python example_usage.py
```

### 2. RAG功能测试（检索+生成）

```bash
# 测试检索和生成功能对比
python example_rag_usage.py
```

### 3. 命令行测试

```bash
# 查询文档
python main.py query "ARM CHI协议的事务类型" --top-k 3

# 查看系统信息
python main.py query "test" --top-k 1
```

### 4. API服务测试

```bash
# 启动API服务器
python main.py api

# 在另一个终端测试API
curl http://127.0.0.1:8000/health
curl -X POST "http://127.0.0.1:8000/query" \
  -H "Content-Type: application/json" \
  -d '{"query": "ARM CHI协议", "top_k": 3}'
```

## 详细测试步骤

### 步骤1：确保文档已索引

```bash
# 检查向量数据库是否存在
ls -la vector_db/

# 如果不存在或需要重新索引
python main.py index data/arm_chi_docs --reset
```

### 步骤2：运行基础测试

```bash
# 运行所有基础示例
python example_usage.py
```

预期输出：
- ✅ 基本查询示例
- ✅ 获取上下文示例
- ✅ 格式化输出示例
- ✅ 便捷函数示例
- ✅ 系统信息示例

### 步骤3：运行RAG测试

```bash
# 测试检索和生成功能
python example_rag_usage.py
```

预期输出：
- ✅ 纯检索模式示例
- ✅ RAG模式示例（如果配置了LLM）
- ✅ 上下文提取示例
- ✅ 功能对比示例

### 步骤4：交互式测试

```bash
# Python交互式测试
python
```

```python
from cursor_integration import ARMCHIRAG

# 初始化
rag = ARMCHIRAG()

# 测试查询
results = rag.query("ARM CHI协议", top_k=3)
print(f"找到 {len(results)} 个结果")

# 测试获取上下文
context = rag.get_context("缓存一致性协议")
print(f"上下文长度: {len(context)}")

# 测试RAG（如果配置了LLM）
result = rag.ask("ARM CHI协议的事务类型有哪些？")
if result.get('answer'):
    print("回答:", result['answer'])
else:
    print("LLM未启用，只返回检索结果")
```

## 测试场景

### 场景1：基本检索测试

```bash
python -c "
from cursor_integration import ARMCHIRAG
rag = ARMCHIRAG()
results = rag.query('ARM CHI协议', top_k=2)
print(f'找到 {len(results)} 个结果')
for r in results:
    print(f\"- {r['file_name']} 第 {r['page']} 页\")
"
```

### 场景2：上下文提取测试

```bash
python -c "
from cursor_integration import get_arm_chi_context
context = get_arm_chi_context('缓存一致性协议', max_length=500)
print(f'上下文长度: {len(context)} 字符')
print(context[:200])
"
```

### 场景3：系统信息测试

```bash
python -c "
from cursor_integration import ARMCHIRAG
rag = ARMCHIRAG()
info = rag.get_info()
print('向量数据库信息:')
for k, v in info.items():
    print(f'  {k}: {v}')
"
```

## 故障排除

### 问题1：向量数据库不存在

```bash
# 错误: FileNotFoundError 或 未找到相关文档
# 解决: 重新索引文档
python main.py index data/arm_chi_docs --reset
```

### 问题2：依赖包缺失

```bash
# 错误: ModuleNotFoundError
# 解决: 安装依赖
pip install -r requirements.txt
```

### 问题3：配置文件错误

```bash
# 错误: 配置加载失败
# 解决: 检查config.yaml格式
python -c "import yaml; yaml.safe_load(open('config.yaml'))"
```

## 性能测试

### 测试查询速度

```bash
python -c "
import time
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()
queries = ['ARM CHI协议', '缓存一致性', '事务类型', '协议架构', '数据一致性']

start = time.time()
for q in queries:
    results = rag.query(q, top_k=5)
end = time.time()

print(f'查询 {len(queries)} 个问题，耗时: {end-start:.2f} 秒')
print(f'平均每个查询: {(end-start)/len(queries):.3f} 秒')
"
```

## 完整测试流程

```bash
# 1. 检查环境
python --version
pip list | grep -E "langchain|chromadb|sentence-transformers"

# 2. 检查文档
ls -lh data/arm_chi_docs/

# 3. 检查索引
python -c "from cursor_integration import ARMCHIRAG; print(ARMCHIRAG().get_info())"

# 4. 运行基础测试
python example_usage.py

# 5. 运行RAG测试
python example_rag_usage.py

# 6. 测试API（可选）
python main.py api &
sleep 2
curl http://127.0.0.1:8000/health
pkill -f "python main.py api"
```

## 预期结果

### 成功标志

✅ 能够检索到相关文档
✅ 返回结果包含内容、分数、元数据
✅ 上下文提取正常工作
✅ 系统信息正确显示
✅ 无错误或异常

### 失败标志

❌ 返回空结果（可能未索引）
❌ 导入错误（依赖缺失）
❌ 配置文件错误
❌ 向量数据库错误

