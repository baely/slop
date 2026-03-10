# E-ink Display Generator

Convert HTML content to monochrome byte arrays for MicroPython e-ink displays.

## Features

- **Web Interface**: Edit HTML content and preview the result
- **Exact Dimensions**: 400×300 pixels output
- **Monochrome**: True 1-bit black and white (no grayscale)
- **MONO_HLSB Format**: Compatible with `framebuf.MONO_HLSB` in MicroPython
- **Device API**: Endpoint for MicroPython devices to download byte arrays

## Setup

1. Install Python dependencies:
   ```bash
   pip install -r requirements.txt
   ```

2. Install Playwright browser:
   ```bash
   playwright install chromium
   ```

3. Run the app:
   ```bash
   python app.py
   ```

4. Open http://localhost:8080 in your browser

## Usage

### Web Interface
- Navigate to http://localhost:8080
- Edit HTML content in the left panel
- Click "Preview" to see the monochrome result
- Click "Download Bytes" to get the binary file

### API for MicroPython Device

```python
import urequests
import json

# Send HTML content to get byte array
data = {'html_content': '<div>Hello World</div>'}
response = urequests.post('http://your-server:8080/api/bytes', json=data)
result = response.json()

if result['success']:
    import ubinascii
    byte_data = ubinascii.a2b_base64(result['bytes'])
    
    # Use with framebuf
    import framebuf
    fbuf = framebuf.FrameBuffer(byte_data, 400, 300, framebuf.MONO_HLSB)
    display.blit(fbuf, 0, 0)
```

## File Structure

- `app.py` - Main Flask application
- `templates/index.html` - Web interface
- `requirements.txt` - Python dependencies
- Generated files are in MONO_HLSB format for direct use with MicroPython's framebuf