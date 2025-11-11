# 在Cursor中使用@符号引用RAG项目指南

## 概述

在Cursor中，`@`符号用于引用文件、文件夹、代码片段等，让Agent能够读取和理解这些内容。本指南说明如何在其他Agent会话中使用`@`符号来引用ARM CHI RAG项目。

## 方法1：直接使用@符号引用项目文件夹（推荐）

### 基本用法

在Cursor Chat中，你可以直接使用`@`符号引用整个RAG项目：

```
@~/arm_chi_rag 请帮我查询ReadOnce事务的流程
```

或者引用项目中的特定文件：

```
@~/arm_chi_rag/cursor_integration.py 这个文件如何使用？
@~/arm_chi_rag/README.md 这个项目的功能是什么？
```

### 引用关键文件

#### 引用集成模块
```
@~/arm_chi_rag/cursor_integration.py
```

#### 引用配置文件
```
@~/arm_chi_rag/config.yaml
```

#### 引用文档
```
@~/arm_chi_rag/README.md
@~/arm_chi_rag/CURSOR_INTEGRATION.md
```

### 实际使用示例

**示例1：查询RAG系统**
```
@~/arm_chi_rag/cursor_integration.py 

请使用这个RAG系统查询"ReadOnce事务有哪些流程？"
```

**示例2：理解项目结构**
```
@~/arm_chi_rag/README.md @~/arm_chi_rag/cursor_integration.py

请解释这个RAG项目如何工作，以及如何在其他项目中使用它。
```

**示例3：获取特定信息**
```
@~/arm_chi_rag/READONCE_FLOWS_ANALYSIS.md

基于这个文档，ReadOnce有哪些可能的流程？
```

## 方法2：在其他项目中创建符号链接

如果你在其他项目中工作，可以创建符号链接来方便引用：

### 在Linux/WSL中

```bash
# 在你的项目目录中创建符号链接
cd /path/to/your/project
ln -s ~/arm_chi_rag ./arm_chi_rag_ref
```

然后在Cursor中引用：
```
@./arm_chi_rag_ref/cursor_integration.py
```

### 在Windows中

```cmd
# 使用mklink创建符号链接
cd C:\path\to\your\project
mklink /D arm_chi_rag_ref C:\Users\readm\arm_chi_rag
```

## 方法3：使用相对路径引用

如果你的项目在同一个父目录下：

```
项目结构：
~/projects/
  ├── my_project/
  └── arm_chi_rag/
```

在`my_project`中引用：
```
@../arm_chi_rag/cursor_integration.py
```

## 方法4：创建便捷引用脚本

在你的工作项目中创建一个Python脚本，方便调用RAG系统：

```python
# query_arm_chi.py
import sys
from pathlib import Path

# 添加RAG项目到路径
rag_project = Path.home() / "arm_chi_rag"
sys.path.insert(0, str(rag_project))

from cursor_integration import ARMCHIRAG, get_arm_chi_context

def query(query_text: str, top_k: int = 5):
    """查询ARM CHI文档"""
    rag = ARMCHIRAG(config_path=str(rag_project / "config.yaml"))
    return rag.query(query_text, top_k=top_k)

def get_context(query_text: str, max_length: int = 2000):
    """获取上下文"""
    rag = ARMCHIRAG(config_path=str(rag_project / "config.yaml"))
    return rag.get_context(query_text, max_length)

if __name__ == "__main__":
    import sys
    if len(sys.argv) > 1:
        query_text = " ".join(sys.argv[1:])
        results = query(query_text)
        for r in results:
            print(f"Page {r['page']}: {r['content'][:200]}...")
    else:
        print("Usage: python query_arm_chi.py <query>")
```

然后在Cursor中引用这个脚本：
```
@query_arm_chi.py 请使用这个脚本查询"ReadOnce流程"
```

## 方法5：在Cursor Chat中的完整工作流

### 步骤1：引用RAG项目
```
@~/arm_chi_rag/cursor_integration.py
```

### 步骤2：提出问题
```
请使用这个RAG系统查询"ReadOnce事务有哪些可能的流程？"
```

### 步骤3：Agent会自动
1. 读取`cursor_integration.py`文件
2. 理解如何使用RAG系统
3. 执行查询并返回结果

## 实际使用场景

### 场景1：在flow_sim项目中查询ARM CHI协议

在`flow_sim`项目中，你可以这样使用：

```
@~/arm_chi_rag/cursor_integration.py

请查询ARM CHI协议中ReadOnce事务的流程，并帮我检查flow_sim项目中的实现是否正确。
```

### 场景2：理解文档并生成代码

```
@~/arm_chi_rag/READONCE_FLOWS_ANALYSIS.md @transaction.go

基于ReadOnce的流程分析，请检查transaction.go中的ReadOnce实现是否符合协议规范。
```

### 场景3：多文件引用

```
@~/arm_chi_rag/cursor_integration.py @~/arm_chi_rag/config.yaml @transaction.go

请使用RAG系统查询ReadOnce的完整流程，然后检查transaction.go的实现。
```

## 最佳实践

### 1. 引用顺序
先引用RAG项目文件，再提出问题：
```
@~/arm_chi_rag/cursor_integration.py
请查询...
```

### 2. 组合引用
可以同时引用多个相关文件：
```
@~/arm_chi_rag/cursor_integration.py @~/arm_chi_rag/README.md
```

### 3. 明确路径
使用绝对路径更可靠：
```
@~/arm_chi_rag/cursor_integration.py  # 推荐
@arm_chi_rag/cursor_integration.py    # 可能找不到
```

### 4. 引用文档而非代码
如果只需要信息，引用文档更高效：
```
@~/arm_chi_rag/READONCE_FLOWS_ANALYSIS.md
```

## 常见问题

### Q: @符号找不到文件？
A: 确保使用正确的路径：
- 使用`~`表示home目录：`@~/arm_chi_rag/...`
- 使用绝对路径：`@/home/readm/arm_chi_rag/...`
- 使用相对路径：`@../arm_chi_rag/...`

### Q: Agent无法执行RAG查询？
A: Agent可以读取代码，但执行需要：
1. 确保RAG系统已索引文档（运行过`python main.py index`）
2. 在代码中明确调用RAG函数
3. 或者让Agent生成调用代码

### Q: 如何让Agent自动使用RAG？
A: 在引用时明确说明：
```
@~/arm_chi_rag/cursor_integration.py

请使用ARMCHIRAG类查询"ReadOnce流程"，并返回结果。
```

## 完整示例

### 示例：在flow_sim项目中查询协议

```
@~/arm_chi_rag/cursor_integration.py @transaction.go

请执行以下操作：
1. 使用ARMCHIRAG查询"ReadOnce事务的所有流程"
2. 检查transaction.go中的ReadOnce实现
3. 对比实现是否符合协议规范
4. 如果有差异，请指出并建议修改
```

Agent会：
1. 读取`cursor_integration.py`了解如何使用RAG
2. 读取`transaction.go`了解当前实现
3. 执行RAG查询获取协议规范
4. 对比并给出建议

## 总结

使用`@`符号引用RAG项目的关键点：

1. ✅ **直接引用**：`@~/arm_chi_rag/cursor_integration.py`
2. ✅ **组合引用**：同时引用多个相关文件
3. ✅ **明确指令**：告诉Agent如何使用RAG
4. ✅ **路径正确**：使用绝对路径或`~`符号

这样，你就可以在任何Cursor项目中轻松使用ARM CHI RAG系统了！

