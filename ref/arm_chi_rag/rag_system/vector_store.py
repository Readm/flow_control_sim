"""
向量存储模块：ChromaDB向量数据库操作
"""

import os
from typing import List, Optional
import chromadb
from chromadb.config import Settings
from langchain_core.documents import Document
from langchain_community.vectorstores import Chroma

# 兼容不同版本的LangChain导入
try:
    from langchain_community.embeddings import HuggingFaceEmbeddings
except ImportError:
    try:
        from langchain.embeddings import HuggingFaceEmbeddings
    except ImportError:
        raise ImportError("无法导入HuggingFaceEmbeddings，请确保已安装langchain-community")


class VectorStore:
    """向量存储管理器"""
    
    def __init__(
        self,
        persist_directory: str = "./vector_db",
        collection_name: str = "arm_chi_docs",
        embedding_model_name: str = "all-MiniLM-L6-v2",
        device: str = "cpu",
        normalize_embeddings: bool = True
    ):
        """
        初始化向量存储
        
        Args:
            persist_directory: 持久化目录
            collection_name: 集合名称
            embedding_model_name: 嵌入模型名称
            device: 设备（cpu或cuda）
            normalize_embeddings: 是否归一化向量
        """
        self.persist_directory = persist_directory
        self.collection_name = collection_name
        self.device = device
        
        # 创建持久化目录
        os.makedirs(persist_directory, exist_ok=True)
        
        # 初始化嵌入模型
        self.embeddings = HuggingFaceEmbeddings(
            model_name=embedding_model_name,
            model_kwargs={"device": device},
            encode_kwargs={"normalize_embeddings": normalize_embeddings}
        )
        
        # 初始化ChromaDB客户端
        self.client = chromadb.PersistentClient(
            path=persist_directory,
            settings=Settings(anonymized_telemetry=False)
        )
        
        # 初始化向量存储
        self.vectorstore = Chroma(
            client=self.client,
            collection_name=collection_name,
            embedding_function=self.embeddings,
            persist_directory=persist_directory
        )
    
    def add_documents(self, documents: List[Document]) -> List[str]:
        """
        添加文档到向量数据库
        
        Args:
            documents: Document对象列表
            
        Returns:
            文档ID列表
        """
        if not documents:
            print("警告: 没有文档需要添加")
            return []
        
        print(f"正在添加 {len(documents)} 个文档块到向量数据库...")
        ids = self.vectorstore.add_documents(documents)
        print(f"成功添加 {len(ids)} 个文档块")
        return ids
    
    def similarity_search(
        self,
        query: str,
        k: int = 5,
        score_threshold: Optional[float] = None
    ) -> List[Document]:
        """
        相似度搜索
        
        Args:
            query: 查询文本
            k: 返回top-k个结果
            score_threshold: 相似度阈值（可选）
            
        Returns:
            相关文档列表
        """
        results = self.vectorstore.similarity_search_with_score(query, k=k)
        
        # 过滤低分结果
        if score_threshold is not None:
            filtered_results = [
                doc for doc, score in results if score >= score_threshold
            ]
        else:
            filtered_results = [doc for doc, score in results]
        
        return filtered_results
    
    def similarity_search_with_scores(
        self,
        query: str,
        k: int = 5
    ) -> List[tuple]:
        """
        相似度搜索（带分数）
        
        Args:
            query: 查询文本
            k: 返回top-k个结果
            
        Returns:
            (Document, score)元组列表
        """
        return self.vectorstore.similarity_search_with_score(query, k=k)
    
    def get_collection_info(self) -> dict:
        """
        获取集合信息
        
        Returns:
            集合信息字典
        """
        collection = self.client.get_collection(self.collection_name)
        count = collection.count()
        return {
            "collection_name": self.collection_name,
            "document_count": count,
            "persist_directory": self.persist_directory
        }
    
    def delete_collection(self):
        """删除集合"""
        try:
            self.client.delete_collection(self.collection_name)
            print(f"已删除集合: {self.collection_name}")
        except Exception as e:
            print(f"删除集合时出错: {str(e)}")
    
    def reset(self):
        """重置向量数据库（删除并重新创建）"""
        self.delete_collection()
        self.vectorstore = Chroma(
            client=self.client,
            collection_name=self.collection_name,
            embedding_function=self.embeddings,
            persist_directory=self.persist_directory
        )

