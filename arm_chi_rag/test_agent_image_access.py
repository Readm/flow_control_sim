"""
Test script to verify if Agent can read images/icons from the RAG project
"""
import os
import sys
from pathlib import Path

# Add project root to path
project_root = Path(__file__).parent
sys.path.insert(0, str(project_root))

from rag_system.document_processor import DocumentProcessor

def test_image_extraction():
    """Test if we can extract images from PDF"""
    print("=" * 60)
    print("Testing Image Extraction from PDF")
    print("=" * 60)
    
    # Initialize processor with image extraction enabled
    processor = DocumentProcessor(
        extract_images=True,
        ocr_enabled=False,
        image_output_dir="./test_extracted_images"
    )
    
    # Find PDF files
    pdf_dir = project_root / "data" / "arm_chi_docs"
    if not pdf_dir.exists():
        print(f"Error: PDF directory not found: {pdf_dir}")
        return False
    
    pdf_files = list(pdf_dir.glob("*.pdf"))
    if not pdf_files:
        print(f"Error: No PDF files found in {pdf_dir}")
        return False
    
    print(f"Found {len(pdf_files)} PDF file(s)")
    
    # Process first PDF
    pdf_file = pdf_files[0]
    print(f"\nProcessing: {pdf_file.name}")
    
    try:
        # Load PDF and extract images
        documents = processor.load_pdf(str(pdf_file))
        
        print(f"Loaded {len(documents)} pages")
        
        # Check for images in metadata
        pages_with_images = []
        for doc in documents:
            if doc.metadata.get("has_images", False):
                pages_with_images.append(doc.metadata["page"])
                print(f"  Page {doc.metadata['page']}: {doc.metadata.get('image_count', 0)} image(s)")
        
        if pages_with_images:
            print(f"\n✓ Found images on {len(pages_with_images)} page(s)")
            return True
        else:
            print("\n⚠ No images detected in PDF (may be vector graphics)")
            return False
            
    except Exception as e:
        print(f"Error processing PDF: {str(e)}")
        import traceback
        traceback.print_exc()
        return False

def test_image_file_access():
    """Test if Agent can read image files"""
    print("\n" + "=" * 60)
    print("Testing Image File Access")
    print("=" * 60)
    
    # Check if extracted images directory exists
    image_dir = project_root / "test_extracted_images"
    
    if image_dir.exists():
        image_files = list(image_dir.glob("*.*"))
        image_extensions = ['.png', '.jpg', '.jpeg', '.gif', '.svg', '.ico', '.bmp']
        image_files = [f for f in image_files if f.suffix.lower() in image_extensions]
        
        if image_files:
            print(f"Found {len(image_files)} image file(s):")
            for img_file in image_files[:5]:  # Show first 5
                size = img_file.stat().st_size
                print(f"  - {img_file.name} ({size} bytes)")
            
            # Try to read first image
            try:
                img_file = image_files[0]
                with open(img_file, 'rb') as f:
                    image_data = f.read()
                print(f"\n✓ Successfully read image: {img_file.name}")
                print(f"  Size: {len(image_data)} bytes")
                return True
            except Exception as e:
                print(f"\n✗ Failed to read image: {str(e)}")
                return False
        else:
            print("No image files found in extracted_images directory")
            return False
    else:
        print("Extracted images directory does not exist yet")
        print("Run image extraction first")
        return False

def main():
    """Main test function"""
    print("\n" + "=" * 60)
    print("Agent Image Access Test for ARM CHI RAG Project")
    print("=" * 60 + "\n")
    
    # Test 1: Image extraction
    extraction_success = test_image_extraction()
    
    # Test 2: Image file access
    access_success = test_image_file_access()
    
    # Summary
    print("\n" + "=" * 60)
    print("Test Summary")
    print("=" * 60)
    print(f"Image Extraction: {'✓ PASS' if extraction_success else '✗ FAIL'}")
    print(f"Image File Access: {'✓ PASS' if access_success else '✗ FAIL'}")
    print("=" * 60)
    
    if extraction_success or access_success:
        print("\n✓ Agent can access image content from the RAG project")
    else:
        print("\n⚠ Agent may have limited access to image content")

if __name__ == "__main__":
    main()






