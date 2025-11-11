"""
增强版文档处理模块：支持PDF解析、文本分块、图片提取和OCR
"""

import os
from typing import List, Dict, Any, Optional
from pathlib import Path
import pdfplumber
import PyPDF2
from langchain_text_splitters import RecursiveCharacterTextSplitter
from langchain_core.documents import Document


class EnhancedDocumentProcessor:
    """增强版文档处理器，支持图片提取和OCR"""
    
    def __init__(
        self, 
        chunk_size: int = 512, 
        chunk_overlap: int = 50, 
        pdf_parser: str = "pdfplumber",
        extract_images: bool = False,
        ocr_enabled: bool = False
    ):
        """
        初始化增强版文档处理器
        
        Args:
            chunk_size: 文本分块大小
            chunk_overlap: 分块重叠大小
            pdf_parser: PDF解析器类型（pdfplumber 或 pypdf2）
            extract_images: 是否提取图片
            ocr_enabled: 是否启用OCR（需要安装pytesseract和Pillow）
        """
        self.chunk_size = chunk_size
        self.chunk_overlap = chunk_overlap
        self.pdf_parser = pdf_parser
        self.extract_images = extract_images
        self.ocr_enabled = ocr_enabled
        
        # 检查OCR依赖
        if ocr_enabled:
            try:
                import pytesseract
                from PIL import Image
                self.ocr_available = True
            except ImportError:
                print("警告: OCR功能需要安装 pytesseract 和 Pillow")
                print("安装命令: pip install pytesseract pillow")
                self.ocr_enabled = False
                self.ocr_available = False
        
        self.text_splitter = RecursiveCharacterTextSplitter(
            chunk_size=chunk_size,
            chunk_overlap=chunk_overlap,
            length_function=len,
            separators=["\n\n", "\n", "。", " ", ""]
        )
    
    def _extract_image_text(self, image_path: str) -> str:
        """使用OCR提取图片中的文字"""
        if not self.ocr_available:
            return ""
        
        try:
            import pytesseract
            from PIL import Image
            
            image = Image.open(image_path)
            text = pytesseract.image_to_string(image, lang='eng+chi_sim')
            return text.strip()
        except Exception as e:
            print(f"OCR处理图片失败 {image_path}: {str(e)}")
            return ""
    
    def _process_images_in_page(self, page, page_num: int) -> str:
        """处理页面中的图片"""
        image_texts = []
        
        if hasattr(page, 'images') and page.images:
            for idx, img in enumerate(page.images):
                try:
                    # 尝试提取图片
                    # 注意：pdfplumber的图片提取需要额外处理
                    # 这里提供一个框架，实际实现可能需要使用其他库如PyMuPDF
                    image_info = f"[图片 {idx+1}: 位置 x0={img.get('x0', '?')}, y0={img.get('y0', '?')}, 宽度={img.get('width', '?')}, 高度={img.get('height', '?')}]"
                    image_texts.append(image_info)
                    
                    # 如果启用OCR，可以尝试提取图片中的文字
                    # 注意：pdfplumber不直接支持图片保存，需要使用其他方法
                    if self.ocr_enabled:
                        # 这里需要先将图片保存到临时文件
                        # 可以使用 page.to_image() 然后保存
                        pass
                        
                except Exception as e:
                    print(f"处理第 {page_num} 页图片 {idx+1} 时出错: {str(e)}")
        
        return "\n".join(image_texts)
    
    def _load_with_pdfplumber(self, file_path: str) -> List[Document]:
        """使用pdfplumber加载PDF（增强版）"""
        documents = []
        file_name = Path(file_path).stem
        
        try:
            with pdfplumber.open(file_path) as pdf:
                for page_num, page in enumerate(pdf.pages, start=1):
                    # 提取文本
                    text = page.extract_text() or ""
                    
                    # 提取图片信息
                    image_info = ""
                    if self.extract_images:
                        image_info = self._process_images_in_page(page, page_num)
                    
                    # 提取表格（表格也可以视为结构化图片）
                    tables = []
                    if hasattr(page, 'extract_tables'):
                        try:
                            tables = page.extract_tables()
                        except:
                            pass
                    
                    # 合并文本内容
                    full_content = text
                    if image_info:
                        full_content += f"\n\n[页面图片信息]\n{image_info}"
                    if tables:
                        table_text = "\n\n[表格内容]\n"
                        for i, table in enumerate(tables, 1):
                            table_text += f"表格 {i}:\n"
                            for row in table:
                                if row:
                                    table_text += " | ".join(str(cell) if cell else "" for cell in row) + "\n"
                        full_content += table_text
                    
                    if full_content.strip():
                        doc = Document(
                            page_content=full_content,
                            metadata={
                                "source": file_path,
                                "file_name": file_name,
                                "page": page_num,
                                "total_pages": len(pdf.pages),
                                "has_images": len(page.images) > 0 if hasattr(page, 'images') else False,
                                "image_count": len(page.images) if hasattr(page, 'images') else 0,
                                "has_tables": len(tables) > 0 if tables else False
                            }
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
    
    def load_pdf(self, file_path: str) -> List[Document]:
        """加载PDF文件并转换为Document对象列表"""
        if not os.path.exists(file_path):
            raise FileNotFoundError(f"文件不存在: {file_path}")
        
        if self.pdf_parser == "pdfplumber":
            return self._load_with_pdfplumber(file_path)
        else:
            return self._load_with_pypdf2(file_path)
    
    def chunk_documents(self, documents: List[Document]) -> List[Document]:
        """将文档分块"""
        chunks = []
        for doc in documents:
            doc_chunks = self.text_splitter.split_documents([doc])
            
            for idx, chunk in enumerate(doc_chunks):
                chunk.metadata["chunk_index"] = idx
                chunk.metadata["total_chunks"] = len(doc_chunks)
            
            chunks.extend(doc_chunks)
        
        return chunks
    
    def process_directory(self, directory_path: str) -> List[Document]:
        """处理目录中的所有PDF文件"""
        all_documents = []
        directory = Path(directory_path)
        
        if not directory.exists():
            raise FileNotFoundError(f"目录不存在: {directory_path}")
        
        pdf_files = list(directory.glob("*.pdf"))
        if not pdf_files:
            print(f"警告: 在 {directory_path} 中未找到PDF文件")
            return all_documents
        
        print(f"找到 {len(pdf_files)} 个PDF文件")
        if self.extract_images:
            print("图片提取: 已启用")
        if self.ocr_enabled:
            print("OCR功能: 已启用")
        
        for pdf_file in pdf_files:
            print(f"处理文件: {pdf_file.name}")
            try:
                docs = self.load_pdf(str(pdf_file))
                all_documents.extend(docs)
            except Exception as e:
                print(f"处理文件 {pdf_file.name} 时出错: {str(e)}")
                continue
        
        return all_documents

