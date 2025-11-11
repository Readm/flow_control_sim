"""
RAG使用示例：演示检索模式和生成模式的区别
"""

from cursor_integration import ARMCHIRAG


def example_retrieval_only():
    """示例1：纯检索模式（当前默认）"""
    print("=" * 60)
    print("示例1：纯检索模式")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    
    query = "ARM CHI协议的事务类型有哪些？"
    print(f"问题: {query}\n")
    
    # 只检索，不生成回答
    results = rag.query(query, top_k=3)
    
    print(f"找到 {len(results)} 个相关文档片段:\n")
    for i, result in enumerate(results, 1):
        print(f"片段 {i} (相似度: {result['score']:.4f}):")
        print(f"  来源: {result['file_name']} 第 {result['page']} 页")
        print(f"  内容: {result['content'][:200]}...\n")
    
    print("注意: 这是纯检索模式，只返回原始文档片段")
    print("需要用户自己阅读和理解这些片段\n")


def example_rag_with_generation():
    """示例2：RAG模式（检索+生成，需要配置LLM）"""
    print("=" * 60)
    print("示例2：RAG模式（检索+生成）")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    
    query = "ARM CHI协议的事务类型有哪些？"
    print(f"问题: {query}\n")
    
    # 使用ask方法：检索 + 生成回答
    result = rag.ask(query, top_k=3, generate_answer=True)
    
    if result.get('answer'):
        print("生成的回答:")
        print(result['answer'])
        print("\n来源:")
        for source in result['sources']:
            print(f"  - {source['file_name']} 第 {source['page']} 页 (相似度: {source['score']:.4f})")
    else:
        print("注意: LLM生成功能未启用")
        print("要启用生成功能，请:")
        print("  1. 在 config.yaml 中配置 generation.enabled: true")
        print("  2. 配置LLM提供商和API密钥")
        print("  3. 安装相应的依赖包")
        print("\n当前只返回检索结果:")
        for doc in result.get('retrieved_docs', [])[:3]:
            print(f"  - {doc['file_name']} 第 {doc['page']} 页")
    
    print()


def example_context_extraction():
    """示例3：提取上下文用于其他用途"""
    print("=" * 60)
    print("示例3：提取上下文")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    
    query = "缓存一致性协议"
    print(f"问题: {query}\n")
    
    # 获取合并的上下文
    context = rag.get_context(query, max_length=1500)
    
    print("提取的上下文:")
    print(context[:500] + "..." if len(context) > 500 else context)
    print(f"\n上下文长度: {len(context)} 字符")
    print("\n这个上下文可以:")
    print("  - 传递给其他LLM进行进一步处理")
    print("  - 用于代码生成")
    print("  - 用于文档摘要")
    print()


def example_comparison():
    """示例4：对比检索模式和RAG模式"""
    print("=" * 60)
    print("示例4：检索 vs RAG 对比")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    query = "ARM CHI协议的事务类型"
    
    print("问题:", query)
    print("\n" + "-" * 60)
    print("模式1: 纯检索 (query方法)")
    print("-" * 60)
    results = rag.query(query, top_k=2)
    print(f"返回: {len(results)} 个文档片段")
    print("特点: 快速、原始、需要用户理解")
    
    print("\n" + "-" * 60)
    print("模式2: RAG (ask方法)")
    print("-" * 60)
    result = rag.ask(query, top_k=2)
    if result.get('answer'):
        print("返回: 生成的综合回答 + 来源")
        print("特点: 更易理解、综合性强")
    else:
        print("返回: 检索结果（LLM未启用）")
        print("特点: 需要配置LLM才能生成回答")
    
    print()


if __name__ == "__main__":
    try:
        # 运行所有示例
        example_retrieval_only()
        example_rag_with_generation()
        example_context_extraction()
        example_comparison()
        
        print("=" * 60)
        print("所有示例运行完成！")
        print("=" * 60)
        print("\n提示:")
        print("- 当前系统默认使用纯检索模式")
        print("- 要启用LLM生成，请配置 config.yaml 中的 generation 部分")
        print("- 详细说明请查看 RAG_VS_RETRIEVAL.md")
        
    except FileNotFoundError as e:
        print(f"错误: {e}")
        print("\n提示: 请先运行 'python main.py index data/arm_chi_docs' 来索引文档")
    except Exception as e:
        print(f"错误: {e}")
        import traceback
        traceback.print_exc()

