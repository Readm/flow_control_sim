"""
Cursor集成示例：提供便捷的Python接口供Cursor使用
"""

import os
import yaml
from pathlib import Path
from typing import List, Dict, Any, Optional
from rag_system.vector_store import VectorStore
from rag_system.retriever import Retriever
from rag_system.generator import AnswerGenerator
from rag_system.rag_chain import RAGChain
from rag_system.llm_factory import create_llm


class ARMCHIRAG:
    """ARM CHI RAG系统封装类，方便Cursor使用"""
    
    def __init__(self, config_path: str = "config.yaml"):
        """
        初始化RAG系统
        
        Args:
            config_path: 配置文件路径
        """
        self.config_path = Path(config_path)
        if not self.config_path.exists():
            raise FileNotFoundError(f"配置文件不存在: {config_path}")
        
        with open(self.config_path, 'r', encoding='utf-8') as f:
            self.config = yaml.safe_load(f) or {}
        
        # 初始化组件
        self._init_components()
    
    def _init_components(self):
        """初始化向量存储和检索器"""
        vector_db_config = self.config.get("vector_db", {})
        embedding_config = self.config.get("embedding", {})
        
        self.vector_store = VectorStore(
            persist_directory=vector_db_config.get("persist_directory", "./vector_db"),
            collection_name=vector_db_config.get("collection_name", "arm_chi_docs"),
            embedding_model_name=embedding_config.get("model_name", "all-MiniLM-L6-v2"),
            device=embedding_config.get("device", "cpu"),
            normalize_embeddings=embedding_config.get("normalize_embeddings", True)
        )
        
        retrieval_config = self.config.get("retrieval", {})
        self.retriever = Retriever(
            vector_store=self.vector_store,
            top_k=retrieval_config.get("top_k", 5),
            score_threshold=retrieval_config.get("score_threshold", 0.5)
        )
        
        # 初始化生成器（如果启用）
        generation_config = self.config.get("generation", {})
        self.generator = None
        if generation_config.get("enabled", False):
            llm = create_llm(generation_config)
            if llm:
                self.generator = AnswerGenerator(
                    llm=llm,
                    system_prompt=generation_config.get("system_prompt")
                )
                print("LLM生成功能已启用")
            else:
                print("警告: LLM生成功能配置失败，将只使用检索功能")
        
        # 创建RAG链
        self.rag_chain = RAGChain(
            retriever=self.retriever,
            generator=self.generator
        )
    
    def query(self, query: str, top_k: Optional[int] = None) -> List[Dict[str, Any]]:
        """
        查询ARM CHI文档
        
        Args:
            query: 查询文本
            top_k: 返回结果数量（可选，使用配置中的默认值）
            
        Returns:
            查询结果列表，每个结果包含content、score、metadata等字段
        """
        if top_k:
            self.retriever.top_k = top_k
        
        return self.retriever.retrieve_with_scores(query)
    
    def get_context(self, query: str, max_length: int = 2000) -> str:
        """
        获取查询的上下文（合并多个相关文档块）
        
        Args:
            query: 查询文本
            max_length: 最大上下文长度
            
        Returns:
            合并后的上下文字符串
        """
        return self.retriever.get_context(query, max_length)
    
    def format_results(self, query: str) -> str:
        """
        格式化查询结果，返回可读的字符串
        
        Args:
            query: 查询文本
            
        Returns:
            格式化后的结果字符串
        """
        results = self.query(query)
        return self.retriever.format_results(results)
    
    def get_info(self) -> Dict[str, Any]:
        """
        获取向量数据库信息
        
        Returns:
            集合信息字典
        """
        return self.vector_store.get_collection_info()
    
    def ask(
        self, 
        query: str, 
        top_k: Optional[int] = None,
        generate_answer: bool = True
    ) -> Dict[str, Any]:
        """
        完整的RAG查询：检索 + 生成回答
        
        Args:
            query: 用户问题
            top_k: 检索的文档数量
            generate_answer: 是否生成回答（如果有generator）
            
        Returns:
            包含检索结果和生成回答的字典
        """
        return self.rag_chain.query(
            query=query,
            top_k=top_k,
            generate_answer=generate_answer
        )


# 全局实例（可选）
_rag_instance: Optional[ARMCHIRAG] = None


def get_rag_instance(config_path: str = "config.yaml") -> ARMCHIRAG:
    """
    获取RAG系统单例实例
    
    Args:
        config_path: 配置文件路径
        
    Returns:
        ARMCHIRAG实例
    """
    global _rag_instance
    if _rag_instance is None:
        _rag_instance = ARMCHIRAG(config_path)
    return _rag_instance


# 便捷函数
def query_arm_chi(query: str, top_k: int = 5) -> List[Dict[str, Any]]:
    """
    便捷查询函数
    
    Args:
        query: 查询文本
        top_k: 返回结果数量
        
    Returns:
        查询结果列表
    """
    rag = get_rag_instance()
    return rag.query(query, top_k)


def get_arm_chi_context(query: str, max_length: int = 2000) -> str:
    """
    获取ARM CHI文档上下文
    
    Args:
        query: 查询文本
        max_length: 最大上下文长度
        
    Returns:
        上下文字符串
    """
    rag = get_rag_instance()
    return rag.get_context(query, max_length)


# 使用示例
if __name__ == "__main__":
    # 示例1：基本查询
    rag = ARMCHIRAG()
    results = rag.query("ARM CHI协议的事务类型")
    for result in results:
        print(f"来源: {result['file_name']}")
        print(f"相似度: {result['score']:.4f}")
        print(f"内容: {result['content'][:200]}...\n")
    
    # 示例2：获取上下文
    context = rag.get_context("缓存一致性协议", max_length=1500)
    print("上下文:\n", context)
    
    # 示例3：格式化输出
    formatted = rag.format_results("ARM CHI协议")
    print(formatted)

