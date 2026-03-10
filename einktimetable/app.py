import logging

from flask import Flask, render_template, request, jsonify, send_file, send_from_directory
import io
import base64
from PIL import Image, ImageDraw
from playwright.sync_api import sync_playwright
import tempfile
import os

from trams import generate_page

app = Flask(__name__)

# Global variable to store display mode: 'tram' or 'wifi'
display_mode = 'tram'

def html_to_monochrome_image(html_content, width=400, height=300):
    """Convert HTML to monochrome image with exact dimensions"""
    
    # Embed all TTF fonts as base64 data URLs
    static_dir = os.path.join(os.path.dirname(__file__), 'static')
    for filename in os.listdir(static_dir):
        if filename.endswith('.ttf'):
            font_path = os.path.join(static_dir, filename)
            try:
                with open(font_path, 'rb') as f:
                    font_data = base64.b64encode(f.read()).decode()
                font_url = f'data:font/truetype;base64,{font_data}'
                
                # Replace font URLs with base64 data URL
                html_content = html_content.replace(f'url(\'/static/{filename}\')', f'url(\'{font_url}\')')
                html_content = html_content.replace(f'url("/static/{filename}")', f'url("{font_url}")')
            except Exception as e:
                print(f"Warning: Could not load font {filename}: {e}")
    
    html_template = f"""
    <!DOCTYPE html>
    <html>
    <head>
        <meta charset="UTF-8">
        <style>
            body {{
                margin: 0;
                padding: 0;
                width: {width}px;
                height: {height}px;
                overflow: hidden;
                font-family: Arial, sans-serif;
                background: white;
                color: black;
                box-sizing: border-box;
            }}
            * {{
                box-sizing: border-box;
            }}
        </style>
    </head>
    <body>
        {html_content}
    </body>
    </html>
    """
    
    with sync_playwright() as p:
        browser = p.chromium.launch()
        try:
            page = browser.new_page()
            
            # Set viewport to exact dimensions
            page.set_viewport_size({"width": width, "height": height})
            
            # Load HTML content
            page.set_content(html_template)
            
            # Wait for fonts to load
            page.wait_for_load_state('networkidle')
            page.wait_for_timeout(2000)  # Additional wait for font rendering
            
            # Take screenshot
            screenshot_bytes = page.screenshot(
                type='png',
                clip={'x': 0, 'y': 0, 'width': width, 'height': height}
            )
        finally:
            browser.close()
    
    # Load image and convert to monochrome
    img = Image.open(io.BytesIO(screenshot_bytes))
    img = img.resize((width, height), Image.LANCZOS)
    img = img.convert('L')  # Convert to grayscale
    img = img.point(lambda x: 0 if x < 128 else 255, mode='1')  # Convert to 1-bit
    
    return img

def image_to_mono_hlsb_bytes(img):
    """Convert PIL image to MONO_HLSB byte array for framebuf"""
    width, height = img.size
    
    # Ensure image is monochrome
    if img.mode != '1':
        img = img.convert('1')
    
    # Convert to byte array in MONO_HLSB format
    # In MONO_HLSB, pixels are packed horizontally, MSB first
    byte_array = bytearray()
    
    for y in range(height):
        for x in range(0, width, 8):
            byte_val = 0
            for bit in range(8):
                if x + bit < width:
                    pixel = img.getpixel((x + bit, y))
                    if pixel == 255:  # White pixel (inverted - set bit for white)
                        byte_val |= (1 << (7 - bit))
            byte_array.append(byte_val)
    
    return bytes(byte_array)

@app.route('/')
def index():
    """Show the editor with HTML content preloaded from /display"""
    try:
        # Read HTML from disk file
        html_content = ""
        try:
            with open('display.html', 'r') as f:
                html_content = f.read()
        except FileNotFoundError:
            pass
        
        # Generate page content using trams module if available
        try:
            from trams import generate_page
            html_content = generate_page()
        except Exception as e:
            print("exception generating doc:", str(e))
            pass
        
        # Pass the HTML content to the template
        return render_template('index.html', preloaded_content=html_content)
        
    except Exception as e:
        return f"Error: {str(e)}"

@app.route('/static/<path:filename>')
def static_files(filename):
    return send_from_directory('static', filename)

@app.route('/preview', methods=['POST'])
def preview():
    html_content = request.form.get('html_content', '')
    
    try:
        # Convert HTML to monochrome image
        img = html_to_monochrome_image(html_content)
        
        # Convert to base64 for display
        img_buffer = io.BytesIO()
        img.save(img_buffer, format='PNG')
        img_base64 = base64.b64encode(img_buffer.getvalue()).decode()
        
        return jsonify({
            'success': True,
            'image': img_base64,
            'width': img.width,
            'height': img.height
        })
        
    except Exception as e:
        return jsonify({
            'success': False,
            'error': str(e)
        })

@app.route('/download')
def download():
    try:
        global display_mode
        from datetime import datetime
        import re
        
        if display_mode == 'wifi':
            # Show wifi page with fresh time
            try:
                with open('wifi.html', 'r') as f:
                    html_content = f.read()
                
                # Update the time in the wifi.html content
                current_time_str = datetime.now().strftime("%H:%M:%S")
                # Replace the existing time in the wifi.html content
                html_content = re.sub(r'>(\d{2}:\d{2}:\d{2})<', f'>{current_time_str}<', html_content)
                
            except Exception as e:
                print(f"Error loading wifi.html: {e}")
                # Fall back to tram display if wifi.html fails
                html_content = generate_page()
        else:
            # Show tram timetable (default)
            html_content = generate_page()
        
        # Convert HTML to monochrome image
        img = html_to_monochrome_image(html_content)
        
        # Convert to byte array
        byte_array = image_to_mono_hlsb_bytes(img)
        
        # Return as downloadable file
        return send_file(
            io.BytesIO(byte_array),
            as_attachment=True,
            download_name='display.bin',
            mimetype='application/octet-stream'
        )
        
    except Exception as e:
        logging.error("error", exc_info=e)
        return jsonify({
            'success': False,
            'error': str(e)
        })

@app.route('/api/bytes')
def api_bytes():
    """API endpoint for MicroPython device to get byte array"""
    try:
        # Read HTML from disk file
        with open('display.html', 'r') as f:
            html_content = f.read()
        
        # Convert HTML to monochrome image
        img = html_to_monochrome_image(html_content)
        
        # Convert to byte array
        byte_array = image_to_mono_hlsb_bytes(img)
        
        return {
            'success': True,
            'bytes': base64.b64encode(byte_array).decode(),
            'width': img.width,
            'height': img.height,
            'length': len(byte_array)
        }
        
    except Exception as e:
        return jsonify({
            'success': False,
            'error': str(e)
        })

@app.route('/display')
def display():
    """Show the HTML page as it will appear on e-ink display"""
    try:
        global display_mode
        from datetime import datetime
        import re
        
        if display_mode == 'wifi':
            # Show wifi page with fresh time
            try:
                with open('wifi.html', 'r') as f:
                    html_content = f.read()
                
                # Update the time in the wifi.html content
                current_time_str = datetime.now().strftime("%H:%M:%S")
                # Replace the existing time in the wifi.html content
                html_content = re.sub(r'>(\d{2}:\d{2}:\d{2})<', f'>{current_time_str}<', html_content)
                
            except Exception as e:
                print(f"Error loading wifi.html: {e}")
                # Fall back to tram display if wifi.html fails
                html_content = generate_page()
        else:
            # Show tram timetable (default)
            # Read HTML from disk file
            try:
                with open('display.html', 'r') as f:
                    html_content = f.read()
            except FileNotFoundError:
                pass
            
            # Generate page content using trams module if available
            try:
                from trams import generate_page
                html_content = generate_page()
            except Exception as e:
                print("exception generating doc:", str(e))
                pass
        
        # Embed all TTF fonts as base64 data URLs for display
        static_dir = os.path.join(os.path.dirname(__file__), 'static')
        if os.path.exists(static_dir):
            for filename in os.listdir(static_dir):
                if filename.endswith('.ttf'):
                    font_path = os.path.join(static_dir, filename)
                    try:
                        with open(font_path, 'rb') as f:
                            font_data = base64.b64encode(f.read()).decode()
                        font_url = f'data:font/truetype;base64,{font_data}'
                        
                        # Replace font URLs with base64 data URL
                        html_content = html_content.replace(f'url(\'/static/{filename}\')', f'url(\'{font_url}\')')
                        html_content = html_content.replace(f'url("/static/{filename}")', f'url("{font_url}")')
                    except Exception as e:
                        print(f"Warning: Could not load font {filename}: {e}")
        
        # Create display template
        display_template = f"""
        <!DOCTYPE html>
        <html>
        <head>
            <meta charset="UTF-8">
            <style>
                body {{
                    margin: 0;
                    padding: 0;
                    width: 404px;
                    height: 304px;
                    overflow: hidden;
                    font-family: Arial, sans-serif;
                    background: white;
                    color: black;
                    box-sizing: border-box;
                    border: 2px solid #333;
                    display: flex;
                    justify-content: center;
                    align-items: center;
                }}
                .display-container {{
                    width: 400px;
                    height: 300px;
                    background: white;
                    color: black;
                    overflow: hidden;
                    position: relative;
                }}
                * {{
                    box-sizing: border-box;
                }}
            </style>
        </head>
        <body>
            <div class="display-container">
                {html_content}
            </div>
        </body>
        </html>
        """
        
        return display_template
        
    except Exception as e:
        return f"Error: {str(e)}"

@app.route('/settings')
def settings():
    """Settings page with toggle for display mode"""
    global display_mode
    return render_template('settings.html', current_mode=display_mode)

@app.route('/api/set-display-mode', methods=['POST'])
def set_display_mode():
    """API endpoint to change display mode"""
    global display_mode
    
    try:
        print("API endpoint called")
        data = request.get_json()
        print(f"Received data: {data}")
        mode = data.get('mode', '').lower()
        print(f"Mode: {mode}")
        
        if mode not in ['tram', 'wifi']:
            print(f"Invalid mode: {mode}")
            return jsonify({
                'success': False,
                'error': 'Invalid mode. Must be "tram" or "wifi".'
            })
        
        old_mode = display_mode
        display_mode = mode
        print(f"Changed display mode from {old_mode} to {display_mode}")
        
        return jsonify({
            'success': True,
            'mode': display_mode
        })
        
    except Exception as e:
        print(f"Error in set_display_mode: {e}")
        return jsonify({
            'success': False,
            'error': str(e)
        })


if __name__ == '__main__':
    app.run(debug=True, host='0.0.0.0', port=8080)