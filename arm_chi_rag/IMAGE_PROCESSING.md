# PDF图片处理说明

## 当前实现状态

### 当前处理方式
**当前RAG系统只提取PDF中的文本内容，图片信息被忽略。**

在 `document_processor.py` 中，代码只使用了：
```python
text = page.extract_text()  # 只提取文本
```

这意味着：
- ✅ 文本内容：完全提取并索引
- ❌ 图片内容：**完全忽略**
- ❌ 图表信息：**完全忽略**
- ⚠️ 表格内容：部分提取（如果表格是文本格式）

### 影响
对于ARM CHI这样的技术文档，通常包含大量图表、架构图、时序图等，这些信息在当前的RAG系统中无法被检索到。

## 图片检测结果

对 `IHI0050H_amba_chi_architecture_spec.pdf` 的检查：
- 总页数：716页
- 包含图表引用的页面：大量（如"Figure X"、"Table X"等）
- pdfplumber检测到的图片对象：0个（图片可能是嵌入在内容流中的矢量图形）

## 改进方案

### 方案1：基础图片信息提取（已实现）

已创建 `document_processor_enhanced.py`，可以：
- 检测页面中的图片对象
- 提取图片的位置和尺寸信息
- 提取表格内容
- 在文档元数据中标记是否有图片

**使用方法：**
```python
from rag_system.document_processor_enhanced import EnhancedDocumentProcessor

processor = EnhancedDocumentProcessor(
    extract_images=True,  # 启用图片提取
    ocr_enabled=False     # 暂不启用OCR
)
```

### 方案2：OCR文字提取（需要额外安装）

对于包含文字的图片，可以使用OCR提取文字：

**安装依赖：**
```bash
# Ubuntu/Debian
sudo apt-get install tesseract-ocr tesseract-ocr-chi-sim

# 或使用pip
pip install pytesseract pillow
```

**使用方法：**
```python
processor = EnhancedDocumentProcessor(
    extract_images=True,
    ocr_enabled=True  # 启用OCR
)
```

### 方案3：多模态模型生成图片描述（推荐用于图表）

对于架构图、流程图等，可以使用多模态模型生成描述：

**需要的库：**
- `transformers` (已安装)
- `torch` (已安装)
- 多模态模型如 `llava` 或使用API如 `GPT-4V`

**实现思路：**
1. 提取PDF中的图片
2. 使用多模态模型生成图片描述
3. 将描述文本添加到文档内容中

### 方案4：使用PyMuPDF (fitz) 提取图片（推荐）

PyMuPDF可以更好地提取PDF中的图片：

**安装：**
```bash
pip install pymupdf
```

**优势：**
- 更好的图片提取能力
- 支持将页面渲染为图片
- 可以保存图片到文件

## 推荐实施步骤

### 短期方案（快速改进）
1. 使用 `EnhancedDocumentProcessor` 提取图片元数据
2. 在文档中标记包含图片的页面
3. 提取表格内容（pdfplumber支持）

### 中期方案（OCR支持）
1. 安装OCR工具
2. 对包含文字的图片进行OCR
3. 将OCR结果合并到文档内容中

### 长期方案（多模态支持）
1. 集成多模态模型
2. 为图表生成描述性文本
3. 将图片描述向量化并索引

## 当前建议

对于ARM CHI文档：
1. **文本内容**：已完整提取 ✅
2. **图表引用**：文本中提到"Figure X"等信息已包含 ✅
3. **实际图表内容**：需要OCR或多模态模型处理 ⚠️

**如果图表中的关键信息在文本中已有描述，当前系统可能已经足够使用。**

如果需要检索图表的具体内容，建议：
- 先尝试方案2（OCR）
- 如果图表主要是架构图/流程图，考虑方案3（多模态模型）

## 测试图片提取

可以使用以下代码测试图片提取：

```python
from rag_system.document_processor_enhanced import EnhancedDocumentProcessor

processor = EnhancedDocumentProcessor(extract_images=True)
docs = processor.load_pdf("data/arm_chi_docs/IHI0050H_amba_chi_architecture_spec.pdf")

# 检查包含图片的页面
for doc in docs:
    if doc.metadata.get('has_images'):
        print(f"第 {doc.metadata['page']} 页包含图片")
```

