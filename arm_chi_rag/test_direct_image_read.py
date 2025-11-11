"""
Test direct image file reading capabilities
"""
import os
import base64
from pathlib import Path

def test_read_image_as_bytes():
    """Test reading image file as bytes"""
    print("=" * 60)
    print("Testing Direct Image File Reading")
    print("=" * 60)
    
    # Check various possible image locations
    project_root = Path(__file__).parent
    possible_dirs = [
        project_root / "test_extracted_images",
        project_root / "extracted_images",
        project_root / "data" / "images",
        project_root / "images",
    ]
    
    found_images = []
    for img_dir in possible_dirs:
        if img_dir.exists():
            image_files = list(img_dir.glob("*.*"))
            image_extensions = ['.png', '.jpg', '.jpeg', '.gif', '.svg', '.ico', '.bmp']
            for img_file in image_files:
                if img_file.suffix.lower() in image_extensions:
                    found_images.append(img_file)
    
    if not found_images:
        print("No image files found in common directories")
        print("\nTrying to extract images from PDF...")
        
        # Try to extract one page as image
        try:
            import fitz  # PyMuPDF
            pdf_file = project_root / "data" / "arm_chi_docs" / "IHI0050H_amba_chi_architecture_spec.pdf"
            if pdf_file.exists():
                doc = fitz.open(str(pdf_file))
                if len(doc) > 0:
                    # Render first page as image
                    page = doc[0]
                    mat = fitz.Matrix(2, 2)  # 2x zoom
                    pix = page.get_pixmap(matrix=mat)
                    
                    # Save as PNG
                    output_dir = project_root / "test_extracted_images"
                    output_dir.mkdir(exist_ok=True)
                    output_file = output_dir / "page_0_rendered.png"
                    pix.save(str(output_file))
                    found_images.append(output_file)
                    print(f"✓ Rendered page 0 as image: {output_file}")
                doc.close()
        except Exception as e:
            print(f"Failed to render PDF page: {str(e)}")
    
    if found_images:
        print(f"\nFound {len(found_images)} image file(s):")
        for img_file in found_images[:3]:  # Test first 3
            try:
                # Read as bytes
                with open(img_file, 'rb') as f:
                    image_bytes = f.read()
                
                print(f"\n✓ Successfully read: {img_file.name}")
                print(f"  Path: {img_file}")
                print(f"  Size: {len(image_bytes)} bytes")
                print(f"  First 20 bytes (hex): {image_bytes[:20].hex()}")
                
                # Try to get image info
                try:
                    from PIL import Image
                    img = Image.open(img_file)
                    print(f"  Format: {img.format}")
                    print(f"  Mode: {img.mode}")
                    print(f"  Size: {img.size}")
                except:
                    pass
                
                # Encode as base64 (for potential transmission)
                b64_data = base64.b64encode(image_bytes).decode('utf-8')
                print(f"  Base64 length: {len(b64_data)} chars")
                
            except Exception as e:
                print(f"✗ Failed to read {img_file.name}: {str(e)}")
        
        return True
    else:
        print("\n⚠ No image files available for testing")
        return False

def test_agent_capabilities():
    """Test what Agent can do with images"""
    print("\n" + "=" * 60)
    print("Agent Image Access Capabilities")
    print("=" * 60)
    
    capabilities = {
        "Read image files": False,
        "Extract images from PDF": False,
        "Render PDF pages as images": False,
        "Access image metadata": False,
    }
    
    # Test reading image files
    try:
        project_root = Path(__file__).parent
        test_dir = project_root / "test_extracted_images"
        if test_dir.exists():
            images = list(test_dir.glob("*.png"))
            if images:
                with open(images[0], 'rb') as f:
                    data = f.read()
                if len(data) > 0:
                    capabilities["Read image files"] = True
    except:
        pass
    
    # Test PDF image extraction
    try:
        import fitz
        capabilities["Extract images from PDF"] = True
        capabilities["Render PDF pages as images"] = True
    except:
        pass
    
    # Test image metadata
    try:
        from PIL import Image
        capabilities["Access image metadata"] = True
    except:
        pass
    
    print("\nCapabilities:")
    for cap, status in capabilities.items():
        status_str = "✓ YES" if status else "✗ NO"
        print(f"  {cap}: {status_str}")
    
    return capabilities

if __name__ == "__main__":
    print("\n" + "=" * 60)
    print("Agent Image Access Test")
    print("=" * 60 + "\n")
    
    # Test 1: Direct image reading
    read_success = test_read_image_as_bytes()
    
    # Test 2: Capabilities
    capabilities = test_agent_capabilities()
    
    # Summary
    print("\n" + "=" * 60)
    print("Summary")
    print("=" * 60)
    if read_success or any(capabilities.values()):
        print("✓ Agent CAN access image content")
        print("\nAvailable methods:")
        if capabilities.get("Read image files"):
            print("  - Can read image files directly")
        if capabilities.get("Extract images from PDF"):
            print("  - Can extract images from PDF documents")
        if capabilities.get("Render PDF pages as images"):
            print("  - Can render PDF pages as images")
        if capabilities.get("Access image metadata"):
            print("  - Can access image metadata (size, format, etc.)")
    else:
        print("⚠ Agent has limited image access capabilities")






