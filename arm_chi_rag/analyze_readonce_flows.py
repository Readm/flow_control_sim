"""
Analyze ReadOnce transaction flows with OCR to extract information from diagrams
"""
import sys
from pathlib import Path

project_root = Path(__file__).parent
sys.path.insert(0, str(project_root))

from cursor_integration import ARMCHIRAG
import fitz  # PyMuPDF

try:
    import pytesseract
    from PIL import Image
    OCR_AVAILABLE = True
except ImportError:
    OCR_AVAILABLE = False
    print("Warning: OCR not available. Install pytesseract and Pillow for OCR support.")

def extract_text_with_ocr(page_image_path):
    """Extract text from page image using OCR"""
    if not OCR_AVAILABLE:
        return ""
    
    try:
        image = Image.open(page_image_path)
        # Use both English and Chinese OCR
        text = pytesseract.image_to_string(image, lang='eng+chi_sim')
        return text.strip()
    except Exception as e:
        print(f"OCR failed: {str(e)}")
        return ""

def analyze_readonce_flows():
    """Analyze ReadOnce transaction flows from documents and images"""
    print("=" * 60)
    print("Analyzing ReadOnce Transaction Flows")
    print("=" * 60)
    
    rag = ARMCHIRAG()
    
    # Query for ReadOnce flows
    print("\n1. Querying RAG system for ReadOnce information...")
    queries = [
        "ReadOnce transaction flow sequence",
        "ReadOnce possible flows",
        "ReadOnce protocol flow diagram"
    ]
    
    all_content = []
    for query in queries:
        results = rag.query(query, top_k=8)
        for result in results:
            all_content.append({
                'page': result['page'],
                'content': result['content'],
                'score': result['score']
            })
    
    # Focus on pages with flow information
    print("\n2. Key pages with ReadOnce flow information:")
    key_pages = [56, 57, 59, 60, 196, 284, 285, 286]
    
    pdf_file = project_root / "data" / "arm_chi_docs" / "IHI0050H_amba_chi_architecture_spec.pdf"
    if not pdf_file.exists():
        print(f"Error: PDF not found")
        return
    
    doc = fitz.open(str(pdf_file))
    
    # Extract and analyze key pages
    flows_info = []
    
    for page_num in key_pages:
        if page_num - 1 >= len(doc):
            continue
        
        print(f"\n--- Analyzing Page {page_num} ---")
        page = doc[page_num - 1]
        
        # Get text content from RAG results
        page_content = [c['content'] for c in all_content if c['page'] == page_num]
        
        if page_content:
            print(f"Text content found: {len(page_content)} chunks")
            # Show first chunk
            print(f"First chunk: {page_content[0][:300]}...")
        
        # Render page as image
        mat = fitz.Matrix(3, 3)  # Higher resolution for OCR
        pix = page.get_pixmap(matrix=mat)
        
        output_dir = project_root / "readonce_analysis"
        output_dir.mkdir(exist_ok=True)
        output_file = output_dir / f"page_{page_num}_highres.png"
        pix.save(str(output_file))
        
        print(f"Rendered page {page_num} to {output_file.name}")
        
        # Try OCR if available
        if OCR_AVAILABLE:
            print("Running OCR...")
            ocr_text = extract_text_with_ocr(output_file)
            if ocr_text:
                # Look for flow-related keywords
                flow_keywords = ['ReadOnce', 'Requester', 'Home', 'Subordinate', 'Snoopee', 
                               'Request', 'Response', 'Data', 'flow', 'sequence']
                if any(kw.lower() in ocr_text.lower() for kw in flow_keywords):
                    print(f"OCR found flow-related content (first 500 chars):")
                    print(ocr_text[:500])
                    flows_info.append({
                        'page': page_num,
                        'ocr_text': ocr_text,
                        'text_content': page_content[0] if page_content else ""
                    })
        
        # Check for figure references in text
        if page_content:
            import re
            figures = re.findall(r'Figure\s+[B\d\.]+[:\s]+[^\n]+', page_content[0], re.IGNORECASE)
            if figures:
                print(f"Figure references: {figures[:3]}")
    
    doc.close()
    
    # Summary
    print("\n" + "=" * 60)
    print("ReadOnce Flow Analysis Summary")
    print("=" * 60)
    
    # Extract flow information from text
    print("\n3. Extracted ReadOnce Flow Information:")
    
    # Look for specific flow patterns in content
    flow_patterns = []
    for content_item in all_content:
        text = content_item['content']
        if 'ReadOnce' in text or 'ReadOnce' in text:
            # Look for sequence descriptions
            if 'sequence' in text.lower() or 'flow' in text.lower():
                flow_patterns.append({
                    'page': content_item['page'],
                    'text': text[:500]
                })
    
    for i, pattern in enumerate(flow_patterns[:5], 1):
        print(f"\nPattern {i} (Page {pattern['page']}):")
        print(pattern['text'])
    
    print("\n" + "=" * 60)
    print("Analysis complete. Check 'readonce_analysis' directory for rendered pages.")
    print("=" * 60)

if __name__ == "__main__":
    analyze_readonce_flows()






