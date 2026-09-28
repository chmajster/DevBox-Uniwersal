"""Integrity checks for the actual brand assets; Python standard library only.

Run from frontend: python -m unittest discover -s e2e -p 'test_branding_assets.py'
Browser decoding and layout checks live in branding_smoke.py.
"""
import struct
import unittest
import zlib
from pathlib import Path

ASSETS = Path(__file__).resolve().parents[1] / 'src' / 'assets'


def validate_png(data):
    """Check chunk bounds/CRCs and pixel-stream completeness of our RGBA PNGs."""
    if data[:8] != b'\x89PNG\r\n\x1a\n':
        raise ValueError('Invalid PNG signature')
    offset, width, height = 8, 0, 0
    compressed = bytearray()
    seen_end = False
    while offset < len(data):
        if offset + 12 > len(data):
            raise ValueError('Truncated PNG chunk header')
        length = struct.unpack_from('>I', data, offset)[0]
        end = offset + 12 + length
        if end > len(data):
            raise ValueError('Truncated PNG chunk payload')
        kind = data[offset + 4:offset + 8]
        payload = data[offset + 8:end - 4]
        crc = struct.unpack_from('>I', data, end - 4)[0]
        if zlib.crc32(kind + payload) != crc:
            raise ValueError('Invalid PNG chunk checksum')
        if offset == 8 and kind != b'IHDR':
            raise ValueError('Missing PNG header')
        if kind == b'IHDR':
            if length != 13 or width:
                raise ValueError('Invalid PNG header')
            width, height, depth, color, compression, filtering, interlace = struct.unpack('>IIBBBBB', payload)
            if not (0 < width <= 4096 and 0 < height <= 4096):
                raise ValueError('Invalid image dimensions')
            if (depth, color, compression, filtering, interlace) != (8, 6, 0, 0, 0):
                raise ValueError('Brand artwork must be non-interlaced RGBA8')
        elif kind == b'IDAT':
            compressed.extend(payload)
        elif kind == b'IEND':
            if length or end != len(data):
                raise ValueError('Invalid PNG end marker')
            seen_end = True
        offset = end
    if not seen_end or not compressed:
        raise ValueError('Incomplete PNG')
    decoder = zlib.decompressobj()
    pixels = decoder.decompress(compressed) + decoder.flush()
    stride = 1 + width * 4
    if not decoder.eof or decoder.unused_data or len(pixels) != height * stride:
        raise ValueError('Incomplete PNG pixel stream')
    if any(pixels[row * stride] > 4 for row in range(height)):
        raise ValueError('Invalid PNG scanline filter')
    return width, height


def favicon_frames(data):
    if len(data) < 6 or struct.unpack_from('<HH', data) != (0, 1):
        raise ValueError('Invalid ICO signature')
    count = struct.unpack_from('<H', data, 4)[0]
    directory_end = 6 + count * 16
    if not count or directory_end > len(data):
        raise ValueError('Incomplete ICO directory')
    frames = []
    for index in range(count):
        width, height, _, reserved, planes, depth, size, offset = struct.unpack_from('<BBBBHHII', data, 6 + index * 16)
        if reserved or planes not in (0, 1) or depth != 32 or not size or offset < directory_end or offset + size > len(data):
            raise ValueError('Invalid ICO frame')
        frame = data[offset:offset + size]
        if validate_png(frame) != (width or 256, height or 256):
            raise ValueError('ICO dimensions do not match its PNG')
        frames.append(frame)
    return frames


class BrandingAssetsTest(unittest.TestCase):
    def test_logo_is_complete_png(self):
        width, height = validate_png((ASSETS / 'devbox-logo.png').read_bytes())
        self.assertEqual(width, height)
        self.assertGreaterEqual(width, 32)

    def test_favicon_preserves_approved_artwork(self):
        frames = favicon_frames((ASSETS / 'devbox-favicon.ico').read_bytes())
        self.assertEqual({validate_png(frame) for frame in frames}, {(16, 16), (32, 32)})
        self.assertIn((ASSETS / 'devbox-logo.png').read_bytes(), frames)

    def test_rejects_truncated_png(self):
        data = (ASSETS / 'devbox-logo.png').read_bytes()
        with self.assertRaisesRegex(ValueError, 'Truncated'):
            validate_png(data[:len(data) // 2])

    def test_rejects_corrupt_png(self):
        data = bytearray((ASSETS / 'devbox-logo.png').read_bytes())
        data[50] ^= 1
        with self.assertRaisesRegex(ValueError, 'checksum'):
            validate_png(data)

    def test_rejects_truncated_favicon(self):
        data = (ASSETS / 'devbox-favicon.ico').read_bytes()
        with self.assertRaises(ValueError):
            favicon_frames(data[:-20])


if __name__ == '__main__':
    unittest.main()
