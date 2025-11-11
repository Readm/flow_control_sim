"""
Quick reference script for using ARM CHI RAG in other projects
Usage: Import this file and use the functions directly
"""
import sys
from pathlib import Path
from typing import List, Dict, Any, Optional

# Auto-detect RAG project path
RAG_PROJECT_PATH = Path.home() / "arm_chi_rag"

# Add to Python path
if str(RAG_PROJECT_PATH) not in sys.path:
    sys.path.insert(0, str(RAG_PROJECT_PATH))

try:
    from cursor_integration import ARMCHIRAG, get_arm_chi_context, query_arm_chi
    RAG_AVAILABLE = True
except ImportError:
    RAG_AVAILABLE = False
    print(f"Warning: ARM CHI RAG not found at {RAG_PROJECT_PATH}")
    print("Please ensure the RAG project is at ~/arm_chi_rag")


def query(query_text: str, top_k: int = 5) -> List[Dict[str, Any]]:
    """
    Query ARM CHI documents
    
    Args:
        query_text: Query string
        top_k: Number of results to return
        
    Returns:
        List of query results
    """
    if not RAG_AVAILABLE:
        raise RuntimeError("RAG system not available")
    
    return query_arm_chi(query_text, top_k)


def get_context(query_text: str, max_length: int = 2000) -> str:
    """
    Get context from ARM CHI documents
    
    Args:
        query_text: Query string
        max_length: Maximum context length
        
    Returns:
        Context string
    """
    if not RAG_AVAILABLE:
        raise RuntimeError("RAG system not available")
    
    return get_arm_chi_context(query_text, max_length)


def format_results(query_text: str, top_k: int = 5) -> str:
    """
    Get formatted query results as string
    
    Args:
        query_text: Query string
        top_k: Number of results
        
    Returns:
        Formatted string with results
    """
    if not RAG_AVAILABLE:
        rag = ARMCHIRAG(config_path=str(RAG_PROJECT_PATH / "config.yaml"))
        results = rag.query(query_text, top_k)
        return rag.retriever.format_results(results)
    else:
        return "RAG system not available"


# Example usage
if __name__ == "__main__":
    if RAG_AVAILABLE:
        # Example 1: Query
        print("Querying ReadOnce flows...")
        results = query("ReadOnce transaction flow", top_k=3)
        for r in results:
            print(f"Page {r['page']}: {r['content'][:100]}...")
        
        # Example 2: Get context
        print("\nGetting context...")
        context = get_context("ReadOnce", max_length=500)
        print(context[:300])
    else:
        print("RAG system not available. Please check the installation.")

