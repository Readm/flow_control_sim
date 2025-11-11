"""
使用示例脚本：演示如何使用RAG系统
"""

from cursor_integration import ARMCHIRAG, query_arm_chi, get_arm_chi_context


def example_basic_query():
    """示例1：基本查询"""
    print("=" * 60)
    print("示例1：基本查询")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    results = rag.query("ARM CHI协议的事务类型", top_k=3)
    
    print(f"找到 {len(results)} 个相关文档块\n")
    for i, result in enumerate(results, 1):
        print(f"结果 {i}:")
        print(f"  来源: {result['file_name']}")
        print(f"  页码: {result['page']}")
        print(f"  相似度: {result['score']:.4f}")
        print(f"  内容预览: {result['content'][:150]}...")
        print()


def example_get_context():
    """示例2：获取上下文"""
    print("=" * 60)
    print("示例2：获取上下文")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    context = rag.get_context("缓存一致性协议", max_length=1000)
    
    print("上下文内容:\n")
    print(context)
    print()


def example_format_results():
    """示例3：格式化输出"""
    print("=" * 60)
    print("示例3：格式化输出")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    formatted = rag.format_results("ARM CHI协议")
    print(formatted)
    print()


def example_convenience_functions():
    """示例4：使用便捷函数"""
    print("=" * 60)
    print("示例4：使用便捷函数")
    print("=" * 60)
    
    # 使用便捷函数
    results = query_arm_chi("ARM CHI协议", top_k=2)
    print(f"找到 {len(results)} 个结果\n")
    
    context = get_arm_chi_context("缓存一致性", max_length=500)
    print("上下文:\n", context[:300], "...")
    print()


def example_get_info():
    """示例5：获取系统信息"""
    print("=" * 60)
    print("示例5：获取系统信息")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    info = rag.get_info()
    
    print("向量数据库信息:")
    for key, value in info.items():
        print(f"  {key}: {value}")
    print()


if __name__ == "__main__":
    try:
        # 运行所有示例
        example_basic_query()
        example_get_context()
        example_format_results()
        example_convenience_functions()
        example_get_info()
        
        print("=" * 60)
        print("所有示例运行完成！")
        print("=" * 60)
        
    except FileNotFoundError as e:
        print(f"错误: {e}")
        print("\n提示: 请先运行 'python main.py index data/arm_chi_docs' 来索引文档")
    except Exception as e:
        print(f"错误: {e}")
        import traceback
        traceback.print_exc()

