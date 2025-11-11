"""
Cursor使用示例：展示如何在Cursor中使用RAG检索功能
"""

from cursor_integration import ARMCHIRAG, get_arm_chi_context, query_arm_chi


def example_for_cursor():
    """示例：在Cursor中使用RAG检索"""
    print("=" * 60)
    print("Cursor使用示例：只需要检索功能")
    print("=" * 60)
    print()
    
    # 初始化（只需要检索功能，不需要LLM）
    rag = ARMCHIRAG()
    
    # 示例问题
    query = "ARM CHI协议的事务类型有哪些？"
    print(f"问题: {query}\n")
    
    print("-" * 60)
    print("方式1：获取上下文（推荐）")
    print("-" * 60)
    
    # 获取合并的上下文
    context = rag.get_context(query, max_length=2000)
    
    print("检索到的文档上下文：")
    print(context[:500] + "..." if len(context) > 500 else context)
    print(f"\n上下文长度: {len(context)} 字符")
    print("\n💡 在Cursor中：")
    print("  1. 复制上面的上下文内容")
    print("  2. 在Cursor Chat中提问")
    print("  3. Cursor的LLM会基于这些文档内容回答")
    
    print("\n" + "-" * 60)
    print("方式2：获取详细检索结果")
    print("-" * 60)
    
    # 获取详细的检索结果
    results = rag.query(query, top_k=3)
    
    print(f"找到 {len(results)} 个相关文档片段：\n")
    for i, result in enumerate(results, 1):
        print(f"文档片段 {i}:")
        print(f"  来源: {result['file_name']} 第 {result['page']} 页")
        print(f"  相似度: {result['score']:.4f}")
        print(f"  内容预览: {result['content'][:200]}...")
        print()
    
    print("💡 在Cursor中：")
    print("  可以将这些文档片段作为上下文提供给Cursor")
    print("  Cursor的LLM会基于这些文档生成准确回答")


def example_code_generation():
    """示例：用于代码生成"""
    print("\n" + "=" * 60)
    print("示例：用于代码生成")
    print("=" * 60)
    print()
    
    # 获取关于某个主题的文档上下文
    context = get_arm_chi_context("缓存一致性协议", max_length=1500)
    
    print("检索到的文档上下文：")
    print(context[:400] + "..." if len(context) > 400 else context)
    
    print("\n💡 在Cursor中提问：")
    print('  "基于以下ARM CHI文档内容，帮我实现一个缓存一致性协议的Python模拟器"')
    print("\n然后粘贴上面的文档内容，Cursor会基于这些文档生成代码")


def example_quick_query():
    """示例：快速查询"""
    print("\n" + "=" * 60)
    print("快速查询示例")
    print("=" * 60)
    print()
    
    # 使用便捷函数
    results = query_arm_chi("ARM CHI协议", top_k=2)
    
    print(f"快速查询结果：{len(results)} 个文档片段\n")
    for result in results:
        print(f"- {result['file_name']} 第 {result['page']} 页")
        print(f"  {result['content'][:100]}...\n")


if __name__ == "__main__":
    try:
        example_for_cursor()
        example_code_generation()
        example_quick_query()
        
        print("\n" + "=" * 60)
        print("总结")
        print("=" * 60)
        print("✅ 对于Cursor，只需要检索功能即可")
        print("✅ 使用 get_context() 或 query() 获取文档")
        print("✅ 将检索结果提供给Cursor的LLM")
        print("❌ 不需要配置额外的LLM生成功能")
        print("\n详细说明请查看 CURSOR_INTEGRATION.md")
        
    except Exception as e:
        print(f"错误: {e}")
        import traceback
        traceback.print_exc()

