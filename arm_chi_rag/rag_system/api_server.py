"""
API服务模块：FastAPI应用，提供文档索引和查询接口
"""

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
from pydantic import BaseModel
from typing import List, Optional, Dict, Any
import uvicorn

from .document_processor import DocumentProcessor
from .vector_store import VectorStore
from .retriever import Retriever


# 请求/响应模型
class IndexRequest(BaseModel):
    """索引请求模型"""
    directory_path: str
    reset: bool = False


class QueryRequest(BaseModel):
    """查询请求模型"""
    query: str
    top_k: Optional[int] = 5
    include_scores: bool = False


class QueryResponse(BaseModel):
    """查询响应模型"""
    results: List[Dict[str, Any]]
    query: str
    total_results: int


class HealthResponse(BaseModel):
    """健康检查响应模型"""
    status: str
    collection_info: Dict[str, Any]


class RAGAPIServer:
    """RAG API服务器"""
    
    def __init__(
        self,
        config: Dict[str, Any],
        document_processor: Optional[DocumentProcessor] = None,
        vector_store: Optional[VectorStore] = None,
        retriever: Optional[Retriever] = None
    ):
        """
        初始化API服务器
        
        Args:
            config: 配置字典
            document_processor: 文档处理器实例（可选）
            vector_store: 向量存储实例（可选）
            retriever: 检索器实例（可选）
        """
        self.config = config
        self.app = FastAPI(title="ARM CHI RAG API", version="0.1.0")
        
        # 添加CORS中间件
        self.app.add_middleware(
            CORSMiddleware,
            allow_origins=["*"],
            allow_credentials=True,
            allow_methods=["*"],
            allow_headers=["*"],
        )
        
        # 初始化组件
        doc_config = config.get("document", {})
        self.document_processor = document_processor or DocumentProcessor(
            chunk_size=doc_config.get("chunk_size", 512),
            chunk_overlap=doc_config.get("chunk_overlap", 50),
            pdf_parser=doc_config.get("pdf_parser", "pdfplumber")
        )
        
        vector_db_config = config.get("vector_db", {})
        embedding_config = config.get("embedding", {})
        self.vector_store = vector_store or VectorStore(
            persist_directory=vector_db_config.get("persist_directory", "./vector_db"),
            collection_name=vector_db_config.get("collection_name", "arm_chi_docs"),
            embedding_model_name=embedding_config.get("model_name", "all-MiniLM-L6-v2"),
            device=embedding_config.get("device", "cpu"),
            normalize_embeddings=embedding_config.get("normalize_embeddings", True)
        )
        
        retrieval_config = config.get("retrieval", {})
        self.retriever = retriever or Retriever(
            vector_store=self.vector_store,
            top_k=retrieval_config.get("top_k", 5),
            score_threshold=retrieval_config.get("score_threshold", 0.5)
        )
        
        # 注册路由
        self._register_routes()
    
    def _register_routes(self):
        """注册API路由"""
        
        @self.app.get("/", tags=["Root"])
        async def root():
            return {
                "message": "ARM CHI RAG API",
                "version": "0.1.0",
                "endpoints": {
                    "health": "/health",
                    "index": "/index",
                    "query": "/query",
                    "info": "/info"
                }
            }
        
        @self.app.get("/health", response_model=HealthResponse, tags=["Health"])
        async def health():
            """健康检查端点"""
            try:
                collection_info = self.vector_store.get_collection_info()
                return HealthResponse(
                    status="healthy",
                    collection_info=collection_info
                )
            except Exception as e:
                raise HTTPException(status_code=500, detail=str(e))
        
        @self.app.post("/index", tags=["Index"])
        async def index_documents(request: IndexRequest):
            """索引文档端点"""
            try:
                if request.reset:
                    self.vector_store.reset()
                    print("向量数据库已重置")
                
                # 处理文档
                documents = self.document_processor.process_directory(
                    request.directory_path
                )
                
                if not documents:
                    raise HTTPException(
                        status_code=400,
                        detail="未找到可处理的文档"
                    )
                
                # 分块
                chunks = self.document_processor.chunk_documents(documents)
                
                # 添加到向量数据库
                ids = self.vector_store.add_documents(chunks)
                
                return {
                    "status": "success",
                    "message": f"成功索引 {len(chunks)} 个文档块",
                    "document_count": len(documents),
                    "chunk_count": len(chunks),
                    "ids_count": len(ids)
                }
            except Exception as e:
                raise HTTPException(status_code=500, detail=str(e))
        
        @self.app.post("/query", response_model=QueryResponse, tags=["Query"])
        async def query_documents(request: QueryRequest):
            """查询文档端点"""
            try:
                top_k = request.top_k or self.retriever.top_k
                results = self.retriever.retrieve_with_scores(request.query)
                
                # 限制返回数量
                if len(results) > top_k:
                    results = results[:top_k]
                
                return QueryResponse(
                    query=request.query,
                    results=results,
                    total_results=len(results)
                )
            except Exception as e:
                raise HTTPException(status_code=500, detail=str(e))
        
        @self.app.get("/info", tags=["Info"])
        async def get_info():
            """获取集合信息端点"""
            try:
                return self.vector_store.get_collection_info()
            except Exception as e:
                raise HTTPException(status_code=500, detail=str(e))
    
    def run(self, host: str = "127.0.0.1", port: int = 8000, reload: bool = False):
        """
        运行API服务器
        
        Args:
            host: 主机地址
            port: 端口号
            reload: 是否启用热重载
        """
        uvicorn.run(self.app, host=host, port=port, reload=reload)

