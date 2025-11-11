"""
Extract pages related to ReadOnce transaction flows, including images
"""
import sys
from pathlib import Path

project_root = Path(__file__).parent
sys.path.insert(0, str(project_root))

from cursor_integration import ARMCHIRAG
import fitz  # PyMuPDF

def extract_readonce_info():
    """Extract ReadOnce transaction flow information"""
    print("=" * 60)
    print("Extracting ReadOnce Transaction Flow Information")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    
    # Query for ReadOnce
    queries = [
        "ReadOnce transaction flow",
        "ReadOnce protocol flow",
        "ReadOnce sequence",
        "ReadOnce possible flows"
    ]
    
    all_results = []
    for query in queries:
        results = rag.query(query, top_k=10)
        all_results.extend(results)
    
    # Get unique pages
    pages_info = {}
    for result in all_results:
        page_num = result['page']
        if page_num not in pages_info:
            pages_info[page_num] = {
                'content': [],
                'scores': [],
                'file_name': result.get('file_name', '')
            }
        pages_info[page_num]['content'].append(result['content'])
        pages_info[page_num]['scores'].append(result['score'])
    
    print(f"\nFound {len(pages_info)} unique pages related to ReadOnce")
    
    # Sort by average score
    sorted_pages = sorted(
        pages_info.items(),
        key=lambda x: sum(x[1]['scores']) / len(x[1]['scores']),
        reverse=True
    )
    
    # Extract top pages
    pdf_file = project_root / "data" / "arm_chi_docs" / "IHI0050H_amba_chi_architecture_spec.pdf"
    if not pdf_file.exists():
        print(f"Error: PDF not found: {pdf_file}")
        return
    
    output_dir = project_root / "extracted_readonce_pages"
    output_dir.mkdir(exist_ok=True)
    
    doc = fitz.open(str(pdf_file))
    
    print("\nTop pages with ReadOnce information:")
    for page_num, info in sorted_pages[:10]:
        print(f"\nPage {page_num} (avg score: {sum(info['scores'])/len(info['scores']):.4f}):")
        
        # Show content snippets
        for i, content in enumerate(info['content'][:2]):
            print(f"  Snippet {i+1}: {content[:200]}...")
        
        # Render page as image
        try:
            if page_num - 1 < len(doc):
                page = doc[page_num - 1]
                mat = fitz.Matrix(2, 2)  # 2x zoom for better quality
                pix = page.get_pixmap(matrix=mat)
                
                output_file = output_dir / f"page_{page_num}_readonce.png"
                pix.save(str(output_file))
                print(f"  ✓ Rendered to: {output_file.name} ({pix.width}x{pix.height})")
        except Exception as e:
            print(f"  ✗ Failed to render page {page_num}: {str(e)}")
    
    doc.close()
    
    # Also search for "Figure" references
    print("\n" + "=" * 60)
    print("Searching for Figure references related to ReadOnce")
    print("=" * 60)
    
    figure_results = rag.query("ReadOnce Figure diagram", top_k=10)
    for result in figure_results[:5]:
        content = result['content']
        if 'Figure' in content or 'figure' in content:
            print(f"\nPage {result['page']}:")
            # Extract figure references
            import re
            figures = re.findall(r'Figure\s+\d+[\.\d]*', content, re.IGNORECASE)
            if figures:
                print(f"  Figures mentioned: {', '.join(set(figures))}")
            print(f"  Content: {content[:300]}...")

if __name__ == "__main__":
    extract_readonce_info()






