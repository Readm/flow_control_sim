"""
RAG链：完整的检索增强生成流程
"""

from typing import List, Dict, Any, Optional
from .retriever import Retriever
from .generator import AnswerGenerator


class RAGChain:
    """RAG链，整合检索和生成"""
    
    def __init__(
        self,
        retriever: Retriever,
        generator: Optional[AnswerGenerator] = None
    ):
        """
        初始化RAG链
        
        Args:
            retriever: 检索器实例
            generator: 生成器实例（可选，如果没有则只返回检索结果）
        """
        self.retriever = retriever
        self.generator = generator
    
    def query(
        self,
        query: str,
        top_k: Optional[int] = None,
        generate_answer: bool = True,
        max_context_length: int = 2000
    ) -> Dict[str, Any]:
        """
        完整的RAG查询流程
        
        Args:
            query: 用户问题
            top_k: 检索的文档数量
            generate_answer: 是否生成回答（如果有generator）
            max_context_length: 最大上下文长度
            
        Returns:
            包含检索结果和生成回答的字典
        """
        # 1. 检索相关文档
        if top_k:
            self.retriever.top_k = top_k
        
        retrieved_docs = self.retriever.retrieve_with_scores(query)
        
        result = {
            'query': query,
            'retrieved_docs': retrieved_docs,
            'total_results': len(retrieved_docs)
        }
        
        # 2. 生成回答（如果有generator且启用）
        if self.generator and generate_answer and retrieved_docs:
            generated = self.generator.generate_answer_with_sources(
                query=query,
                retrieved_docs=retrieved_docs,
                max_context_length=max_context_length
            )
            result['answer'] = generated['answer']
            result['sources'] = generated['sources']
        else:
            # 只返回检索结果
            result['answer'] = None
            result['sources'] = [
                {
                    'file_name': doc.get('file_name', 'unknown'),
                    'page': doc.get('page', 'unknown'),
                    'score': doc.get('score', 0)
                }
                for doc in retrieved_docs
            ]
        
        return result
    
    def get_context(self, query: str, max_length: int = 2000) -> str:
        """获取上下文（向后兼容）"""
        return self.retriever.get_context(query, max_length)

