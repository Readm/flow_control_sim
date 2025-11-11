# 图片和图表提取使用指南

## 功能概述

RAG系统现在支持提取PDF中的图片和图表，并将相关信息整合到文档索引中。

## 已实现的功能

### 1. 图片提取
- ✅ 使用PyMuPDF提取PDF中的标准图片对象
- ✅ 提取图片元数据（格式、大小等）
- ✅ 可选：保存图片到指定目录

### 2. OCR文字识别
- ✅ 对提取的图片进行OCR，提取图片中的文字
- ✅ 支持中英文识别
- ✅ 可选：将整个页面渲染为图片并进行OCR（适用于矢量图形）

### 3. 表格提取
- ✅ 自动提取PDF中的表格内容
- ✅ 将表格转换为文本格式

## 配置说明

编辑 `config.yaml` 文件：

```yaml
document:
  extract_images: true      # 是否提取PDF中的图片
  ocr_enabled: false        # 是否启用OCR（需要安装tesseract-ocr）
  image_output_dir: null    # 图片保存目录（null表示不保存）
```

### OCR功能启用步骤

1. **安装系统依赖（Ubuntu/Debian）**：
```bash
sudo apt-get update
sudo apt-get install tesseract-ocr tesseract-ocr-chi-sim
```

2. **安装Python包**（已包含在requirements.txt中）：
```bash
pip install pytesseract pillow
```

3. **启用OCR**：
```yaml
document:
  ocr_enabled: true
```

## 使用方法

### 方法1：基本图片提取（默认）

当前配置已启用图片提取：

```bash
python main.py index data/arm_chi_docs --reset
```

这将：
- 提取PDF中的图片对象
- 提取表格内容
- 在文档中标记包含图片的页面

### 方法2：启用OCR提取图片文字

1. 安装tesseract-ocr（见上方）
2. 修改 `config.yaml`：
```yaml
document:
  ocr_enabled: true
```

3. 重新索引：
```bash
python main.py index data/arm_chi_docs --reset
```

### 方法3：保存提取的图片

修改 `config.yaml`：
```yaml
document:
  image_output_dir: "./extracted_images"
```

图片将保存为：`文件名_p页码_img序号.格式`

## 图片处理方式

### 标准图片对象
- 系统会自动检测PDF中的标准图片对象
- 提取图片元数据和内容
- 如果启用OCR，会提取图片中的文字

### 矢量图形/嵌入图片
对于嵌入在PDF内容流中的矢量图形：
- 当前系统会提取文本描述（如果存在）
- 如需处理矢量图形，可以启用页面渲染OCR（见下方）

### 页面渲染OCR（高级功能）

如果PDF中的图表是矢量图形，可以将整个页面渲染为图片并进行OCR：

```python
from rag_system.document_processor import DocumentProcessor

processor = DocumentProcessor(
    extract_images=True,
    ocr_enabled=True,
    render_pages_as_images=True  # 启用页面渲染OCR
)
```

**注意**：这会显著增加处理时间，建议仅对包含重要图表的页面使用。

## 查询包含图片的文档

查询结果会包含图片信息：

```python
from cursor_integration import ARMCHIRAG

rag = ARMCHIRAG()
results = rag.query("架构图", top_k=5)

for result in results:
    if result['metadata'].get('has_images'):
        print(f"第 {result['page']} 页包含 {result['metadata']['image_count']} 张图片")
        print(f"内容: {result['content'][:500]}...")
```

## 注意事项

1. **处理时间**：启用OCR会显著增加索引时间
2. **存储空间**：保存图片会占用额外存储空间
3. **OCR准确性**：OCR识别准确度取决于图片质量
4. **矢量图形**：对于矢量图形，可能需要使用页面渲染OCR

## 当前状态

对于 `IHI0050H_amba_chi_architecture_spec.pdf`：
- ✅ 文本内容：已完整提取
- ✅ 表格内容：已提取
- ⚠️ 图片对象：未检测到标准图片对象（可能是矢量图形）
- ✅ 图表引用：文本中的"Figure X"等信息已包含

**建议**：
- 如果图表的关键信息在文本中已有描述，当前系统已足够
- 如需提取图表的具体视觉内容，启用OCR功能
- 对于复杂的架构图，考虑使用多模态模型生成描述

## 故障排除

### OCR不工作
1. 检查tesseract是否安装：`tesseract --version`
2. 检查Python包：`python -c "import pytesseract; print('OK')"`
3. 查看错误日志

### 图片未提取
1. 检查PyMuPDF是否安装：`python -c "import fitz; print('OK')"`
2. PDF中的图片可能是矢量图形，不是标准图片对象
3. 尝试启用页面渲染OCR

### 处理速度慢
1. OCR处理较慢，可以只对特定页面启用
2. 考虑使用GPU加速（如果可用）
3. 减少处理的PDF文件数量

