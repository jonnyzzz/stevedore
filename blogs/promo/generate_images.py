from PIL import Image, ImageDraw, ImageFont
import os

def create_image(filename, width, height, bg_color, title_text, subtitle_text=None, footer_text=None):
    img = Image.new('RGB', (width, height), color=bg_color)
    d = ImageDraw.Draw(img)
    
    # Fonts - simplistic approach since we might not have custom fonts
    # We'll use default, but try to scale it by drawing larger
    # Actually, let's look for a ttf if possible, or use default
    try:
        # Try to use a standard font likely to be in a linux container
        title_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 60)
        subtitle_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 40)
        footer_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Oblique.ttf", 30)
        code_font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf", 30)
    except IOError:
        # Fallback to default if specific fonts aren't found
        print("Warning: Custom fonts not found, using default.")
        title_font = ImageFont.load_default()
        subtitle_font = ImageFont.load_default()
        footer_font = ImageFont.load_default()
        code_font = ImageFont.load_default()

    # Colors
    text_color = (255, 255, 255)
    accent_color = (0, 255, 127) # Spring Green
    
    # Draw Title
    d.text((50, 50), title_text, font=title_font, fill=text_color)
    
    # Draw Subtitle
    if subtitle_text:
        d.text((50, 140), subtitle_text, font=subtitle_font, fill=(200, 200, 200))

    return img, d, code_font, text_color, accent_color

def generate_hero_image():
    width, height = 1200, 675 # Twitter card size
    bg_color = (30, 30, 30) # Dark gray
    
    img, d, code_font, text_color, accent_color = create_image(
        "hero.png", width, height, bg_color, 
        "Stevedore", "GitOps for your Homelab"
    )
    
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
    
    try:
        font_large = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 40)
        font_small = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf", 25)
    except:
        font_large = ImageFont.load_default()
        font_small = ImageFont.load_default()

    # Boxes
    def draw_box(x, y, w, h, color, text):
        d.rectangle([x, y, x+w, y+h], fill=color, outline=(0,0,0), width=3)
        d.text((x + 20, y + h/2 - 20), text, font=font_large, fill=(0,0,0))

    # GitHub
    draw_box(100, 250, 250, 150, (240, 240, 240), "GitHub\n(Git Push)")
    
    # Arrow 1
    d.line([350, 325, 450, 325], fill=(0,0,0), width=5)
    d.polygon([(450, 325), (430, 315), (430, 335)], fill=(0,0,0))
    d.text((360, 290), "Polls", font=font_small, fill=(100,100,100))

    # Stevedore (Host)
    d.rectangle([450, 150, 1100, 500], fill=(230, 240, 255), outline=(0,0,200), width=3)
    d.text((470, 170), "Your Raspberry Pi", font=font_small, fill=(0,0,100))

    # Stevedore Container
    draw_box(500, 250, 250, 150, (100, 150, 255), "Stevedore\nDaemon")

    # Arrow 2
    d.line([750, 325, 850, 325], fill=(0,0,0), width=5)
    d.polygon([(850, 325), (830, 315), (830, 335)], fill=(0,0,0))
    d.text((770, 290), "Deploys", font=font_small, fill=(100,100,100))

    # App
    draw_box(850, 250, 200, 150, (100, 255, 100), "Your\nApp")

    img.save("blogs/promo/stevedore_architecture.png")
    print("Generated stevedore_architecture.png")

if __name__ == "__main__":
    generate_hero_image()
    generate_architecture_image()
