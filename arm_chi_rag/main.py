"""
主程序入口：提供命令行接口用于索引和查询
"""

import argparse
import yaml
from pathlib import Path
from rag_system.document_processor import DocumentProcessor
from rag_system.vector_store import VectorStore
from rag_system.retriever import Retriever
from rag_system.api_server import RAGAPIServer


def load_config(config_path: str = "config.yaml") -> dict:
    """加载配置文件"""
    config_file = Path(config_path)
    if not config_file.exists():
        print(f"配置文件不存在: {config_path}，使用默认配置")
        return {}
    
    with open(config_file, 'r', encoding='utf-8') as f:
        return yaml.safe_load(f) or {}


def index_documents(config: dict, directory_path: str, reset: bool = False):
    """索引文档"""
    print("=" * 50)
    print("开始索引文档...")
    print("=" * 50)
    
    # 初始化组件
    doc_config = config.get("document", {})
    image_output_dir = doc_config.get("image_output_dir")
    if image_output_dir == "null" or image_output_dir == "":
        image_output_dir = None
    
    processor = DocumentProcessor(
        chunk_size=doc_config.get("chunk_size", 512),
        chunk_overlap=doc_config.get("chunk_overlap", 50),
        pdf_parser=doc_config.get("pdf_parser", "pdfplumber"),
        extract_images=doc_config.get("extract_images", True),
        ocr_enabled=doc_config.get("ocr_enabled", False),
        image_output_dir=image_output_dir
    )
    
    vector_db_config = config.get("vector_db", {})
    embedding_config = config.get("embedding", {})
    vector_store = VectorStore(
        persist_directory=vector_db_config.get("persist_directory", "./vector_db"),
        collection_name=vector_db_config.get("collection_name", "arm_chi_docs"),
        embedding_model_name=embedding_config.get("model_name", "all-MiniLM-L6-v2"),
        device=embedding_config.get("device", "cpu"),
        normalize_embeddings=embedding_config.get("normalize_embeddings", True)
    )
    
    if reset:
        print("重置向量数据库...")
        vector_store.reset()
    
    # 处理文档
    documents = processor.process_directory(directory_path)
    if not documents:
        print("错误: 未找到可处理的文档")
        return
    
    # 分块
    print(f"\n正在分块 {len(documents)} 个文档...")
    chunks = processor.chunk_documents(documents)
    print(f"生成 {len(chunks)} 个文档块")
    
    # 添加到向量数据库
    vector_store.add_documents(chunks)
    
    # 显示信息
    info = vector_store.get_collection_info()
    print("\n" + "=" * 50)
    print("索引完成！")
    print(f"集合名称: {info['collection_name']}")
    print(f"文档块数量: {info['document_count']}")
    print("=" * 50)


def query_documents(config: dict, query: str, top_k: int = 5):
    """查询文档"""
    print("=" * 50)
    print(f"查询: {query}")
    print("=" * 50)
    
    # 初始化组件
    vector_db_config = config.get("vector_db", {})
    embedding_config = config.get("embedding", {})
    vector_store = VectorStore(
        persist_directory=vector_db_config.get("persist_directory", "./vector_db"),
        collection_name=vector_db_config.get("collection_name", "arm_chi_docs"),
        embedding_model_name=embedding_config.get("model_name", "all-MiniLM-L6-v2"),
        device=embedding_config.get("device", "cpu"),
        normalize_embeddings=embedding_config.get("normalize_embeddings", True)
    )
    
    retrieval_config = config.get("retrieval", {})
    retriever = Retriever(
        vector_store=vector_store,
        top_k=top_k,
        score_threshold=retrieval_config.get("score_threshold", 0.5)
    )
    
    # 查询
    results = retriever.retrieve_with_scores(query)
    
    if not results:
        print("未找到相关文档")
        return
    
    # 显示结果
    print(f"\n找到 {len(results)} 个相关文档块:\n")
    formatted = retriever.format_results(results)
    print(formatted)


def start_api_server(config: dict):
    """启动API服务器"""
    print("=" * 50)
    print("启动RAG API服务器...")
    print("=" * 50)
    
    api_config = config.get("api", {})
    server = RAGAPIServer(config)
    
    host = api_config.get("host", "127.0.0.1")
    port = api_config.get("port", 8000)
    reload = api_config.get("reload", False)
    
    print(f"服务器地址: http://{host}:{port}")
    print(f"API文档: http://{host}:{port}/docs")
    print("\n按 Ctrl+C 停止服务器\n")
    
    server.run(host=host, port=port, reload=reload)


def main():
    """主函数"""
    parser = argparse.ArgumentParser(description="ARM CHI文档RAG系统")
    parser.add_argument(
        "--config",
        type=str,
        default="config.yaml",
        help="配置文件路径（默认: config.yaml）"
    )
    
    subparsers = parser.add_subparsers(dest="command", help="可用命令")
    
    # 索引命令
    index_parser = subparsers.add_parser("index", help="索引文档")
    index_parser.add_argument(
        "directory",
        type=str,
        help="包含PDF文档的目录路径"
    )
    index_parser.add_argument(
        "--reset",
        action="store_true",
        help="重置向量数据库"
    )
    
    # 查询命令
    query_parser = subparsers.add_parser("query", help="查询文档")
    query_parser.add_argument(
        "query",
        type=str,
        help="查询文本"
    )
    query_parser.add_argument(
        "--top-k",
        type=int,
        default=5,
        help="返回top-k个结果（默认: 5）"
    )
    
    # API服务器命令
    api_parser = subparsers.add_parser("api", help="启动API服务器")
    
    args = parser.parse_args()
    
    # 加载配置
    config = load_config(args.config)
    
    # 执行命令
    if args.command == "index":
        index_documents(config, args.directory, args.reset)
    elif args.command == "query":
        query_documents(config, args.query, args.top_k)
    elif args.command == "api":
        start_api_server(config)
    else:
        parser.print_help()


if __name__ == "__main__":
    main()

