"""
文档处理模块：PDF解析、文本分块、元数据提取、图片提取和OCR
"""

import os
import io
import tempfile
from typing import List, Dict, Any, Optional
from pathlib import Path
import pdfplumber
import PyPDF2
from langchain_text_splitters import RecursiveCharacterTextSplitter
from langchain_core.documents import Document

# 尝试导入图片处理相关库
try:
    import fitz  # PyMuPDF
    PYMUPDF_AVAILABLE = True
except ImportError:
    PYMUPDF_AVAILABLE = False

try:
    import pytesseract
    from PIL import Image
    OCR_AVAILABLE = True
except ImportError:
    OCR_AVAILABLE = False


class DocumentProcessor:
    """文档处理器，负责PDF解析和文本分块，支持图片提取和OCR"""
    
    def __init__(
        self, 
        chunk_size: int = 512, 
        chunk_overlap: int = 50, 
        pdf_parser: str = "pdfplumber",
        extract_images: bool = True,
        ocr_enabled: bool = False,
        image_output_dir: Optional[str] = None,
        render_pages_as_images: bool = False
    ):
        """
        初始化文档处理器
        
        Args:
            chunk_size: 文本分块大小
            chunk_overlap: 分块重叠大小
            pdf_parser: PDF解析器类型（pdfplumber 或 pypdf2）
            extract_images: 是否提取图片
            ocr_enabled: 是否启用OCR提取图片中的文字
            image_output_dir: 图片保存目录（可选，如果为None则不保存）
        """
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap
        self.pdf_parser = pdf_parser
        self.extract_images = extract_images
        self.ocr_enabled = ocr_enabled and OCR_AVAILABLE
        self.image_output_dir = image_output_dir
        self.render_pages_as_images = render_pages_as_images
        
        if self.extract_images and not PYMUPDF_AVAILABLE:
            print("警告: PyMuPDF未安装，图片提取功能将不可用")
            print("安装命令: pip install pymupdf")
            self.extract_images = False
        
        if self.ocr_enabled and not OCR_AVAILABLE:
            print("警告: OCR功能需要安装 pytesseract 和 Pillow")
            print("安装命令: pip install pytesseract pillow")
            self.ocr_enabled = False
        
        self.text_splitter = RecursiveCharacterTextSplitter(
            chunk_size=chunk_size,
            chunk_overlap=chunk_overlap,
            length_function=len,
            separators=["\n\n", "\n", "。", " ", ""]
        )
    
    def load_pdf(self, file_path: str) -> List[Document]:
        """
        加载PDF文件并转换为Document对象列表
        
        Args:
            file_path: PDF文件路径
            
        Returns:
            Document对象列表
        """
        if not os.path.exists(file_path):
            raise FileNotFoundError(f"文件不存在: {file_path}")
        
        if self.pdf_parser == "pdfplumber":
            return self._load_with_pdfplumber(file_path)
        else:
            return self._load_with_pypdf2(file_path)
    
    def _extract_images_with_pymupdf(self, file_path: str, page_num: int) -> List[Dict[str, Any]]:
        """使用PyMuPDF提取页面中的图片"""
        images_info = []
        if not PYMUPDF_AVAILABLE or not self.extract_images:
            return images_info
        
        try:
            doc = fitz.open(file_path)
            if page_num - 1 < len(doc):
                page = doc[page_num - 1]
                image_list = page.get_images()
                
                for img_index, img in enumerate(image_list):
                    xref = img[0]
                    base_image = doc.extract_image(xref)
                    image_bytes = base_image["image"]
                    image_ext = base_image["ext"]
                    
                    # 如果启用OCR，提取图片中的文字
                    ocr_text = ""
                    if self.ocr_enabled:
                        try:
                            with tempfile.NamedTemporaryFile(suffix=f".{image_ext}", delete=False) as tmp_file:
                                tmp_file.write(image_bytes)
                                tmp_path = tmp_file.name
                            
                            image = Image.open(tmp_path)
                            ocr_text = pytesseract.image_to_string(image, lang='eng+chi_sim')
                            os.unlink(tmp_path)
                        except Exception as e:
                            print(f"OCR处理图片失败 (第{page_num}页, 图片{img_index+1}): {str(e)}")
                    
                    # 保存图片（如果指定了输出目录）
                    image_path = None
                    if self.image_output_dir:
                        os.makedirs(self.image_output_dir, exist_ok=True)
                        image_filename = f"{Path(file_path).stem}_p{page_num}_img{img_index+1}.{image_ext}"
                        image_path = os.path.join(self.image_output_dir, image_filename)
                        with open(image_path, "wb") as img_file:
                            img_file.write(image_bytes)
                    
                    images_info.append({
                        "index": img_index + 1,
                        "ext": image_ext,
                        "size": len(image_bytes),
                        "ocr_text": ocr_text.strip() if ocr_text else "",
                        "path": image_path
                    })
            
            doc.close()
        except Exception as e:
            print(f"提取图片失败 (第{page_num}页): {str(e)}")
        
        return images_info
    
    def _ocr_page_as_image(self, file_path: str, page_num: int) -> str:
        """将整个页面渲染为图片并进行OCR"""
        if not PYMUPDF_AVAILABLE or not self.ocr_enabled:
            return ""
        
        try:
            doc = fitz.open(file_path)
            if page_num - 1 < len(doc):
                page = doc[page_num - 1]
                # 渲染页面为图片（放大2倍以提高OCR质量）
                mat = fitz.Matrix(2, 2)
                pix = page.get_pixmap(matrix=mat)
                
                # 转换为PIL Image
                img_data = pix.tobytes("png")
                image = Image.open(io.BytesIO(img_data))
                
                # OCR识别
                ocr_text = pytesseract.image_to_string(image, lang='eng+chi_sim')
                doc.close()
                return ocr_text.strip()
        except Exception as e:
            print(f"页面OCR处理失败 (第{page_num}页): {str(e)}")
        
        return ""
    
    def _load_with_pdfplumber(self, file_path: str) -> List[Document]:
        """使用pdfplumber加载PDF，支持图片提取"""
        documents = []
        file_name = Path(file_path).stem
        
        try:
            with pdfplumber.open(file_path) as pdf:
                for page_num, page in enumerate(pdf.pages, start=1):
                    # 提取文本
                    text = page.extract_text() or ""
                    
                    # 提取表格
                    tables_text = ""
                    try:
                        tables = page.extract_tables()
                        if tables:
                            tables_text = "\n\n[表格内容]\n"
                            for i, table in enumerate(tables, 1):
                                tables_text += f"表格 {i}:\n"
                                for row in table:
                                    if row:
                                        row_text = " | ".join(str(cell) if cell else "" for cell in row)
                                        if row_text.strip():
                                            tables_text += row_text + "\n"
                    except:
                        pass
                    
                    # 提取图片信息
                    images_text = ""
                    images_info = []
                    if self.extract_images:
                        images_info = self._extract_images_with_pymupdf(file_path, page_num)
                        if images_info:
                            images_text = "\n\n[图片信息]\n"
                            for img_info in images_info:
                                images_text += f"图片 {img_info['index']} (格式: {img_info['ext']}, 大小: {img_info['size']} 字节)"
                                if img_info['ocr_text']:
                                    images_text += f"\nOCR文字: {img_info['ocr_text']}\n"
                                else:
                                    images_text += "\n"
                    
                    # 合并所有内容
                    full_content = text
                    if tables_text:
                        full_content += tables_text
                    if images_text:
                        full_content += images_text
                    
                    # 如果启用页面渲染OCR，将整个页面渲染为图片并OCR
                    if self.render_pages_as_images and self.ocr_enabled and PYMUPDF_AVAILABLE:
                        try:
                            page_ocr_text = self._ocr_page_as_image(file_path, page_num)
                            if page_ocr_text:
                                full_content += f"\n\n[页面OCR文字]\n{page_ocr_text}"
                        except Exception as e:
                            print(f"页面OCR失败 (第{page_num}页): {str(e)}")
                    
                    if full_content.strip():
                        metadata = {
                            "source": file_path,
                            "file_name": file_name,
                            "page": page_num,
                            "total_pages": len(pdf.pages),
                            "has_images": len(images_info) > 0,
                            "image_count": len(images_info),
                            "has_tables": bool(tables_text)
                        }
                        
                        doc = Document(
                            page_content=full_content,
                            metadata=metadata
                        )
                        documents.append(doc)
        except Exception as e:
            raise Exception(f"使用pdfplumber解析PDF失败: {str(e)}")
        
        return documents
    
    def _load_with_pypdf2(self, file_path: str) -> List[Document]:
        """使用PyPDF2加载PDF"""
        documents = []
        file_name = Path(file_path).stem
        
        try:
            with open(file_path, 'rb') as file:
                pdf_reader = PyPDF2.PdfReader(file)
                total_pages = len(pdf_reader.pages)
                
                for page_num in range(total_pages):
                    page = pdf_reader.pages[page_num]
                    text = page.extract_text()
                    if text and text.strip():
                        doc = Document(
                            page_content=text,
                            metadata={
                                "source": file_path,
                                "file_name": file_name,
                                "page": page_num + 1,
                                "total_pages": total_pages
                            }
                        )
                        documents.append(doc)
        except Exception as e:
            raise Exception(f"使用PyPDF2解析PDF失败: {str(e)}")
        
        return documents
    
    def chunk_documents(self, documents: List[Document]) -> List[Document]:
        """
        将文档分块
        
        Args:
            documents: Document对象列表
            
        Returns:
            分块后的Document对象列表
        """
        chunks = []
        for doc in documents:
            # 使用LangChain的文本分割器
            doc_chunks = self.text_splitter.split_documents([doc])
            
            # 为每个块添加块索引
            for idx, chunk in enumerate(doc_chunks):
                chunk.metadata["chunk_index"] = idx
                chunk.metadata["total_chunks"] = len(doc_chunks)
            
            chunks.extend(doc_chunks)
        
        return chunks
    
    def process_directory(self, directory_path: str) -> List[Document]:
        """
        处理目录中的所有PDF文件
        
        Args:
            directory_path: 目录路径
            
        Returns:
            所有文档的Document对象列表
        """
        all_documents = []
        directory = Path(directory_path)
        
        if not directory.exists():
            raise FileNotFoundError(f"目录不存在: {directory_path}")
        
        pdf_files = list(directory.glob("*.pdf"))
        if not pdf_files:
            print(f"警告: 在 {directory_path} 中未找到PDF文件")
            return all_documents
        
        print(f"找到 {len(pdf_files)} 个PDF文件")
        
        for pdf_file in pdf_files:
            print(f"处理文件: {pdf_file.name}")
            try:
                docs = self.load_pdf(str(pdf_file))
                all_documents.extend(docs)
            except Exception as e:
                print(f"处理文件 {pdf_file.name} 时出错: {str(e)}")
                continue
        
        return all_documents

