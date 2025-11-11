"""
演示脚本：展示启用LLM后的RAG问答效果（使用模拟LLM）
"""

from cursor_integration import ARMCHIRAG
from rag_system.generator import AnswerGenerator
from rag_system.rag_chain import RAGChain


class MockLLM:
    """模拟LLM，用于演示"""
    
    def invoke(self, prompt):
        """模拟LLM调用"""
        class MockResponse:
            def __init__(self, content):
                self.content = content
        
        # 从提示词中提取问题和上下文
        if "用户问题：" in prompt:
            query = prompt.split("用户问题：")[1].split("\n")[0].strip()
            context = prompt.split("文档内容：")[1].split("用户问题：")[0].strip()
            
            # 生成模拟回答
            answer = f"""基于ARM CHI文档，关于"{query}"的回答：

根据文档内容，ARM CHI协议定义了多种事务类型，包括：

1. **读取事务（Read Transactions）**：用于从内存读取数据
2. **写入事务（Write Transactions）**：用于向内存写入数据
3. **原子事务（Atomic Transactions）**：保证操作的原子性
4. **缓存维护事务（Cache Maintenance Transactions）**：用于缓存一致性维护

这些事务类型在文档中有详细说明，具体实现细节请参考相关章节。

注意：这是模拟回答，实际回答会基于真实的文档内容生成。"""
            
            return MockResponse(answer)
        
        return MockResponse("无法理解问题")


def demo_with_mock_llm():
    """使用模拟LLM演示RAG功能"""
    print("=" * 60)
    print("演示：启用LLM后的RAG问答效果")
    print("=" * 60)
    print()
    
    # 初始化RAG系统
    rag_base = ARMCHIRAG()
    
    # 创建模拟LLM和生成器
    mock_llm = MockLLM()
    generator = AnswerGenerator(llm=mock_llm)
    
    # 创建RAG链
    rag_chain = RAGChain(
        retriever=rag_base.retriever,
        generator=generator
    )
    
    # 测试问题
    query = "ARM CHI协议的事务类型有哪些？"
    print(f"问题: {query}\n")
    print("-" * 60)
    
    # 执行RAG查询
    result = rag_chain.query(query, top_k=3, generate_answer=True)
    
    # 显示结果
    print("📋 检索到的相关文档:")
    for i, doc in enumerate(result['retrieved_docs'][:3], 1):
        print(f"  {i}. {doc['file_name']} 第 {doc['page']} 页 (相似度: {doc['score']:.4f})")
    
    print("\n" + "-" * 60)
    print("🤖 LLM生成的回答:")
    print("-" * 60)
    if result.get('answer'):
        print(result['answer'])
    else:
        print("未生成回答")
    
    print("\n" + "-" * 60)
    print("📚 回答来源:")
    print("-" * 60)
    for source in result.get('sources', []):
        print(f"  - {source['file_name']} 第 {source['page']} 页")
    
    print("\n" + "=" * 60)
    print("💡 说明:")
    print("=" * 60)
    print("这是使用模拟LLM的演示。要使用真实的LLM，请：")
    print("1. 配置 config.yaml 中的 generation 部分")
    print("2. 选择LLM提供商（OpenAI、Ollama等）")
    print("3. 安装相应的依赖包")
    print("4. 设置API密钥（如果使用云服务）")
    print("\n详细配置请查看 RAG_VS_RETRIEVAL.md")


def show_configuration_guide():
    """显示配置指南"""
    print("\n" + "=" * 60)
    print("LLM配置指南")
    print("=" * 60)
    
    print("\n方案1：使用OpenAI API（推荐用于测试）")
    print("-" * 60)
    print("""
1. 获取OpenAI API密钥：https://platform.openai.com/api-keys

2. 修改 config.yaml：
   generation:
     enabled: true
     provider: "openai"
     model_name: "gpt-3.5-turbo"
     api_key: "sk-your-api-key-here"
     temperature: 0.7
     max_tokens: 1000

3. 安装依赖：
   pip install langchain-openai

4. 测试：
   python example_rag_usage.py
""")
    
    print("\n方案2：使用本地Ollama（推荐用于生产）")
    print("-" * 60)
    print("""
1. 安装Ollama：
   # 从 https://ollama.ai 下载安装
   # 或使用：curl -fsSL https://ollama.ai/install.sh | sh

2. 下载模型：
   ollama pull llama2
   # 或
   ollama pull mistral

3. 修改 config.yaml：
   generation:
     enabled: true
     provider: "ollama"
     model_name: "llama2"
     base_url: "http://localhost:11434"
     temperature: 0.7

4. 安装依赖：
   pip install langchain-community

5. 测试：
   python example_rag_usage.py
""")
    
    print("\n方案3：使用其他本地模型（OpenAI兼容API）")
    print("-" * 60)
    print("""
如果您的本地模型提供OpenAI兼容的API：

1. 修改 config.yaml：
   generation:
     enabled: true
     provider: "openai"
     model_name: "your-model-name"
     base_url: "http://localhost:8000/v1"
     api_key: "not-needed"

2. 安装依赖：
   pip install langchain-openai
""")


if __name__ == "__main__":
    try:
        demo_with_mock_llm()
        show_configuration_guide()
    except Exception as e:
        print(f"错误: {e}")
        import traceback
        traceback.print_exc()

