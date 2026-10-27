#!/usr/bin/env python3
import os
import json
import zipfile
import zlib
import struct

OFFICIAL_SVG = """<svg viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg">
  <!-- White Ceramic/Glass Lid -->
  <rect x="25" y="8" width="50" height="13" rx="4" fill="#FFFFFF"/>
  <rect x="25" y="18" width="50" height="3" rx="1.5" fill="#E2E8F0"/>
  <line x1="36" y1="10" x2="36" y2="16" stroke="#CBD5E1" stroke-width="2" stroke-linecap="round"/>
  <line x1="50" y1="10" x2="50" y2="16" stroke="#CBD5E1" stroke-width="2" stroke-linecap="round"/>
  <line x1="64" y1="10" x2="64" y2="16" stroke="#CBD5E1" stroke-width="2" stroke-linecap="round"/>

  <!-- Jar Body Outline -->
  <rect x="18" y="24" width="64" height="66" rx="18" fill="#FFFFFF" fill-opacity="0.08" stroke="#FFFFFF" stroke-width="4"/>

  <!-- Inside: Pure Ube Jam Fill -->
  <path d="M 22 43 C 30 38, 42 45, 50 41 C 58 37, 70 44, 78 43 V 72 C 78 81.94, 69.94 90, 60 90 H 40 C 30.06 90, 22 81.94, 22 72 Z" fill="url(#ube_jam_glow)"/>
  <path d="M 24 43 C 32 39, 42 45, 50 41 C 58 37, 68 44, 76 43" stroke="#E9D5FF" stroke-width="2.5" stroke-linecap="round" stroke-opacity="0.8"/>
  <path d="M 25 32 C 25 32 26 76 34 80" stroke="#FFFFFF" stroke-width="3" stroke-linecap="round" stroke-opacity="0.6"/>
  <circle cx="28" cy="83" r="2" fill="#FFFFFF" fill-opacity="0.6"/>

  <defs>
    <linearGradient id="ube_jam_glow" x1="50" y1="38" x2="50" y2="90" gradientUnits="userSpaceOnUse">
      <stop stop-color="#C084FC"/>
      <stop offset="0.35" stop-color="#9333EA"/>
      <stop offset="1" stop-color="#581C87"/>
    </linearGradient>
  </defs>
</svg>"""

def generate_png_icon():
    width, height = 128, 128
    raw_data = bytearray()
    for y in range(height):
        raw_data.append(0)
        for x in range(width):
            dx = x - 64
            dy = y - 64
            dist_sq = dx*dx + dy*dy
            # Render a crisp circular badge with dark purple and white rim
            if dist_sq < 48*48:
                raw_data.extend([147, 51, 234, 255]) # Ube Jam purple
            elif dist_sq < 56*56:
                raw_data.extend([255, 255, 255, 255]) # Clean White jar rim
            elif dist_sq < 60*60:
                raw_data.extend([226, 232, 240, 180])
            else:
                raw_data.extend([0, 0, 0, 0])

    compressed = zlib.compress(bytes(raw_data))
    def make_chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xffffffff)

    png = b"\x89PNG\r\n\x1a\n"
    png += make_chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
    png += make_chunk(b"IDAT", compressed)
    png += make_chunk(b"IEND", b"")
    return png

def build_vsix(out_filename="ube-0.4.0.vsix"):
    print("\033[1;35m🍠 Building Ube VSIX with Official Jam Jar Icon...\033[0m")
    
    # Save icon.svg to local disk if not present
    with open("icon.svg", "w") as f:
        f.write(OFFICIAL_SVG)

    # Load existing package files
    with open("package.json", "r") as f: pkg_json = f.read()
    with open("language-configuration.json", "r") as f: lang_cfg = f.read()
    with open("syntaxes/ube.tmLanguage.json", "r") as f: tm_lang = f.read()
    with open("snippets/ube.code-snippets", "r") as f: snippets = f.read()
    with open("README.md", "r") as f: readme = f.read()

    vsix_manifest = """<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011">
  <Metadata>
    <Identity Id="ube" Version="0.4.0" Language="en-US" Publisher="ube-lang" />
    <DisplayName>Ube Language</DisplayName>
    <Description xml:space="preserve">Official Ube language support with White Jam Jar file icon 🍠</Description>
    <Tags>ube,systems,go,c</Tags>
    <Icon>extension/icon.png</Icon>
  </Metadata>
  <Installation>
    <InstallationTarget Id="Microsoft.VisualStudio.Code"/>
  </Installation>
  <Dependencies/>
  <Assets>
    <Asset Type="Microsoft.VisualStudio.Code.Manifest" Path="extension/package.json" Addressable="true" />
    <Asset Type="Microsoft.VisualStudio.Services.Content.Details" Path="extension/README.md" Addressable="true" />
    <Asset Type="Microsoft.VisualStudio.Services.Icons.Default" Path="extension/icon.png" Addressable="true" />
  </Assets>
</PackageManifest>"""

    content_types = """<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="json" ContentType="application/json" />
  <Default Extension="md" ContentType="text/markdown" />
  <Default Extension="png" ContentType="image/png" />
  <Default Extension="svg" ContentType="image/svg+xml" />
  <Default Extension="vsixmanifest" ContentType="text/xml" />
</Types>"""

    files = {
        "[Content_Types].xml": content_types.encode("utf-8"),
        "extension.vsixmanifest": vsix_manifest.encode("utf-8"),
        "extension/package.json": pkg_json.encode("utf-8"),
        "extension/language-configuration.json": lang_cfg.encode("utf-8"),
        "extension/syntaxes/ube.tmLanguage.json": tm_lang.encode("utf-8"),
        "extension/snippets/ube.code-snippets": snippets.encode("utf-8"),
        "extension/README.md": readme.encode("utf-8"),
        "extension/icon.svg": OFFICIAL_SVG.encode("utf-8"),
        "extension/icon.png": generate_png_icon()
    }

    with zipfile.ZipFile(out_filename, "w", zipfile.ZIP_DEFLATED) as zf:
        for archive_name, data in files.items():
            zf.writestr(archive_name, data)
            print(f"   + packed {archive_name}")

    print(f"\n\033[1;32m✔ Success!\033[0m Created \033[1m{out_filename}\033[0m with official file icon.")

if __name__ == "__main__":
    build_vsix()
