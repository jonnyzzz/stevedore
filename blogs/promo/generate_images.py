from PIL import Image, ImageDraw, ImageFont
import os

def get_font(name, size):
    # Map common names to the JetBrains Mono files we installed
    font_map = {
        "bold": "JetBrainsMono-Bold.ttf",
        "regular": "JetBrainsMono-Regular.ttf",
        "italic": "JetBrainsMono-Italic.ttf",
        "code": "JetBrainsMono-Regular.ttf" 
    }
    
    filename = font_map.get(name, "JetBrainsMono-Regular.ttf")
    path = f"/usr/share/fonts/truetype/jetbrains/{filename}"
    
    try:
        return ImageFont.truetype(path, size)
    except IOError:
        print(f"Warning: Could not load {path}, falling back to default.")
        return ImageFont.load_default()

def create_image(filename, width, height, bg_color, title_text, subtitle_text=None, footer_text=None):
    img = Image.new('RGB', (width, height), color=bg_color)
    d = ImageDraw.Draw(img)
    
    title_font = get_font("bold", 60)
    subtitle_font = get_font("regular", 40)
    footer_font = get_font("italic", 30)
    
    # Colors
    text_color = (255, 255, 255)
    accent_color = (0, 255, 127) # Spring Green
    
    # Draw Title
    d.text((50, 50), title_text, font=title_font, fill=text_color)
    
    # Draw Subtitle
    if subtitle_text:
        d.text((50, 140), subtitle_text, font=subtitle_font, fill=(200, 200, 200))

    return img, d, text_color, accent_color

def generate_hero_image():
    width, height = 1200, 675 # Twitter card size
    bg_color = (30, 30, 30) # Dark gray
    
    img, d, text_color, accent_color = create_image(
        "hero.png", width, height, bg_color, 
        "Stevedore", "GitOps for your Homelab"
    )
    
    code_font = get_font("code", 28)

    # Draw pseudo-terminal
    term_x, term_y = 50, 250
    term_w, term_h = 1100, 350
    d.rectangle([term_x, term_y, term_x + term_w, term_y + term_h], fill=(10, 10, 10), outline=(100, 100, 100))
    
    # Header bar
    d.rectangle([term_x, term_y, term_x + term_w, term_y + 40], fill=(50, 50, 50))
    d.ellipse([term_x + 15, term_y + 12, term_x + 30, term_y + 27], fill=(255, 95, 86)) # Red
    d.ellipse([term_x + 40, term_y + 12, term_x + 55, term_y + 27], fill=(255, 189, 46)) # Yellow
    d.ellipse([term_x + 65, term_y + 12, term_x + 80, term_y + 27], fill=(39, 201, 63)) # Green

    # Terminal Content
    lines = [
        "$ stevedore status",
        "",
        "DEPLOYMENT       STATUS    HEALTHY",
        "----------------------------------",
        "discord-bot      Running   [OK] ✓",
        "home-assistant   Running   [OK] ✓",
        "pi-hole          Running   [OK] ✓",
        "",
        "$ _"
    ]
    
    y_offset = term_y + 60
    for line in lines:
        fill_color = text_color
        if "✓" in line:
            fill_color = accent_color
        if "$" in line:
            fill_color = (100, 200, 255) # Light blue prompt
            
        d.text((term_x + 30, y_offset), line, font=code_font, fill=fill_color)
        y_offset += 40

    img.save("blogs/promo/stevedore_hero.png")
    print("Generated stevedore_hero.png")

def generate_architecture_image():
    width, height = 1200, 675
    bg_color = (255, 255, 255) # White background for diagram
    
    img = Image.new('RGB', (width, height), color=bg_color)
    d = ImageDraw.Draw(img)
    
    # Reduced font size for boxes to ensure fit
    font_large = get_font("bold", 32)
    font_small = get_font("regular", 25)

    # Boxes with centered text
    def draw_box(x, y, w, h, color, text):
        d.rectangle([x, y, x+w, y+h], fill=color, outline=(0,0,0), width=3)
        
        # Calculate text size for centering
        lines = text.split('\n')
        
        # Calculate total height of the text block
        total_text_height = 0
        line_heights = []
        for line in lines:
            bbox = d.textbbox((0, 0), line, font=font_large)
            # height = bottom - top
            lh = bbox[3] - bbox[1]
            # add a bit of leading
            lh = lh * 1.2
            line_heights.append(lh)
            total_text_height += lh
        
        # Start Y position
        current_y = y + (h - total_text_height) / 2
        
        for i, line in enumerate(lines):
            bbox = d.textbbox((0, 0), line, font=font_large)
            text_width = bbox[2] - bbox[0]
            
            # Center X
            current_x = x + (w - text_width) / 2
            
            d.text((current_x, current_y), line, font=font_large, fill=(0,0,0))
            current_y += line_heights[i]

    # GitHub
    draw_box(50, 250, 250, 150, (240, 240, 240), "GitHub\n(Git Push)")
    
    # Arrow 1 (Gap: 300 -> 450, 150px wide)
    d.line([300, 325, 450, 325], fill=(0,0,0), width=5)
    d.polygon([(450, 325), (430, 315), (430, 335)], fill=(0,0,0))
    
    # Centered "Polls" text
    label1 = "Polls"
    bbox1 = d.textbbox((0, 0), label1, font=font_small)
    w1 = bbox1[2] - bbox1[0]
    d.text((300 + (150 - w1)/2, 290), label1, font=font_small, fill=(100,100,100))

    # Stevedore (Host)
    # Host starts at 450. Daemon is at 500.
    d.rectangle([450, 150, 1150, 500], fill=(230, 240, 255), outline=(0,0,200), width=3)
    d.text((470, 170), "Your Raspberry Pi", font=font_small, fill=(0,0,100))

    # Stevedore Container
    draw_box(500, 250, 250, 150, (100, 150, 255), "Stevedore\nDaemon")

    # Arrow 2 (Gap: 750 -> 900, 150px wide)
    d.line([750, 325, 900, 325], fill=(0,0,0), width=5)
    d.polygon([(900, 325), (880, 315), (880, 335)], fill=(0,0,0))
    
    # Centered "Deploys" text
    label2 = "Deploys"
    bbox2 = d.textbbox((0, 0), label2, font=font_small)
    w2 = bbox2[2] - bbox2[0]
    d.text((750 + (150 - w2)/2, 290), label2, font=font_small, fill=(100,100,100))

    # App
    draw_box(900, 250, 200, 150, (100, 255, 100), "Your\nApp")

    img.save("blogs/promo/stevedore_architecture.png")
    print("Generated stevedore_architecture.png")

if __name__ == "__main__":
    generate_hero_image()
    generate_architecture_image()