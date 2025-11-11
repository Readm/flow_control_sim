"""
生成模块：基于检索到的文档生成回答
"""

from typing import List, Dict, Any, Optional
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.messages import HumanMessage, SystemMessage


class AnswerGenerator:
    """答案生成器，基于检索到的文档生成回答"""
    
    def __init__(
        self,
        llm=None,
        system_prompt: Optional[str] = None
    ):
        """
        初始化答案生成器
        
        Args:
            llm: 语言模型实例（可以是OpenAI、本地模型等）
            system_prompt: 系统提示词
        """
        self.llm = llm
        self.system_prompt = system_prompt or self._default_system_prompt()
    
    def _default_system_prompt(self) -> str:
        """默认系统提示词"""
        return """你是一个专业的ARM CHI协议文档助手。基于提供的文档内容，回答用户的问题。

要求：
1. 只基于提供的文档内容回答，不要编造信息
2. 如果文档中没有相关信息，明确说明
3. 回答要准确、专业、清晰
4. 可以引用文档中的具体内容
5. 使用中文回答"""
    
    def generate_answer(
        self,
        query: str,
        context: str,
        max_tokens: int = 1000
    ) -> str:
        """
        基于检索到的上下文生成回答
        
        Args:
            query: 用户问题
            context: 检索到的上下文
            max_tokens: 最大生成token数
            
        Returns:
            生成的回答
        """
        if not self.llm:
            # 如果没有LLM，返回格式化的上下文
            return self._format_context_only(query, context)
        
        # 构建提示词
        prompt = self._build_prompt(query, context)
        
        try:
            # 调用LLM生成回答
            if hasattr(self.llm, 'invoke'):
                # LangChain格式
                response = self.llm.invoke(prompt)
                if hasattr(response, 'content'):
                    return response.content
                return str(response)
            elif hasattr(self.llm, 'generate'):
                # 其他格式
                response = self.llm.generate(prompt)
                return response
            else:
                # 直接调用
                return str(self.llm(prompt))
        except Exception as e:
            print(f"生成回答时出错: {str(e)}")
            return self._format_context_only(query, context)
    
    def _build_prompt(self, query: str, context: str) -> str:
        """构建提示词"""
        prompt = f"""{self.system_prompt}

文档内容：
{context}

用户问题：{query}

请基于上述文档内容回答用户的问题。"""
        return prompt
    
    def _format_context_only(self, query: str, context: str) -> str:
        """当没有LLM时，仅格式化上下文"""
        return f"""问题：{query}

相关文档内容：
{context}

注意：当前系统未配置语言模型，仅显示检索到的相关内容。要获得生成的回答，请配置LLM。"""
    
    def generate_answer_with_sources(
        self,
        query: str,
        retrieved_docs: List[Dict[str, Any]],
        max_context_length: int = 2000
    ) -> Dict[str, Any]:
        """
        基于检索到的文档生成回答（包含来源信息）
        
        Args:
            query: 用户问题
            retrieved_docs: 检索到的文档列表
            max_context_length: 最大上下文长度
            
        Returns:
            包含回答和来源信息的字典
        """
        # 合并上下文
        context_parts = []
        sources = []
        current_length = 0
        
        for doc in retrieved_docs:
            content = doc.get('content', '')
            if current_length + len(content) <= max_context_length:
                context_parts.append(content)
                sources.append({
                    'file_name': doc.get('file_name', 'unknown'),
                    'page': doc.get('page', 'unknown'),
                    'score': doc.get('score', 0)
                })
                current_length += len(content)
            else:
                remaining = max_context_length - current_length
                if remaining > 100:
                    context_parts.append(content[:remaining])
                    sources.append({
                        'file_name': doc.get('file_name', 'unknown'),
                        'page': doc.get('page', 'unknown'),
                        'score': doc.get('score', 0)
                    })
                break
        
        context = "\n\n".join(context_parts)
        
        # 生成回答
        answer = self.generate_answer(query, context)
        
        return {
            'answer': answer,
            'sources': sources,
            'context_length': len(context)
        }

