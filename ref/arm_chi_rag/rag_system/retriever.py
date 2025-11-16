"""
检索模块：相似度搜索、上下文检索、结果格式化
"""

from typing import List, Dict, Any
from langchain_core.documents import Document
from .vector_store import VectorStore


class Retriever:
    """检索器，封装向量存储的检索功能"""
    
    def __init__(
        self,
        vector_store: VectorStore,
        top_k: int = 5,
        score_threshold: float = 0.5
    ):
        """
        初始化检索器
        
        Args:
            vector_store: 向量存储实例
            top_k: 返回top-k个结果
            score_threshold: 相似度阈值
        """
        self.vector_store = vector_store
        self.top_k = top_k
        self.score_threshold = score_threshold
    
    def retrieve(self, query: str, include_scores: bool = False) -> List[Document]:
        """
        检索相关文档
        
        Args:
            query: 查询文本
            include_scores: 是否包含相似度分数
            
        Returns:
            相关文档列表
        """
        if include_scores:
            results = self.vector_store.similarity_search_with_scores(
                query, k=self.top_k
            )
            # 过滤低分结果
            filtered = [
                doc for doc, score in results
                if score >= self.score_threshold
            ]
            return filtered
        else:
            return self.vector_store.similarity_search(
                query,
                k=self.top_k,
                score_threshold=self.score_threshold
            )
    
    def retrieve_with_scores(self, query: str) -> List[Dict[str, Any]]:
        """
        检索相关文档（带分数和元数据）
        
        Args:
            query: 查询文本
            
        Returns:
            包含文档内容、分数和元数据的字典列表
        """
        results = self.vector_store.similarity_search_with_scores(
            query, k=self.top_k
        )
        
        formatted_results = []
        for doc, score in results:
            if score >= self.score_threshold:
                formatted_results.append({
                    "content": doc.page_content,
                    "score": float(score),
                    "metadata": doc.metadata,
                    "source": doc.metadata.get("source", "unknown"),
                    "page": doc.metadata.get("page", "unknown"),
                    "file_name": doc.metadata.get("file_name", "unknown")
                })
        
        return formatted_results
    
    def format_results(self, results: List[Dict[str, Any]]) -> str:
        """
        格式化检索结果
        
        Args:
            results: 检索结果列表
            
        Returns:
            格式化后的字符串
        """
        if not results:
            return "未找到相关文档"
        
        formatted = []
        for idx, result in enumerate(results, 1):
            formatted.append(f"\n--- 结果 {idx} (相似度: {result['score']:.4f}) ---")
            formatted.append(f"来源: {result['file_name']} (第 {result['page']} 页)")
            formatted.append(f"内容:\n{result['content']}\n")
        
        return "\n".join(formatted)
    
    def get_context(self, query: str, max_length: int = 2000) -> str:
        """
        获取查询的上下文（合并多个相关文档块）
        
        Args:
            query: 查询文本
            max_length: 最大上下文长度
            
        Returns:
            合并后的上下文字符串
        """
        results = self.retrieve_with_scores(query)
        
        if not results:
            return ""
        
        context_parts = []
        current_length = 0
        
        for result in results:
            content = result['content']
            if current_length + len(content) <= max_length:
                context_parts.append(content)
                current_length += len(content)
            else:
                # 截断最后一个文档块
                remaining = max_length - current_length
                if remaining > 100:  # 至少保留100个字符
                    context_parts.append(content[:remaining])
                break
        
        return "\n\n".join(context_parts)

