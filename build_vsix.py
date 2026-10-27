#!/usr/bin/env python3
import os
import json
import zipfile
import zlib
import struct

# --- 1. Generates a 128x128 Ube-Purple PNG Icon ---
def generate_png_icon():
    width, height = 128, 128
    raw_data = bytearray()
    for y in range(height):
        raw_data.append(0)  # Filter byte: None
        for x in range(width):
            # Distance from center for circular crop
            dx = x - 64
            dy = y - 64
            dist_sq = dx*dx + dy*dy
            if dist_sq < 56*56:
                # Vibrant Ube Violet (#9D4EDD)
                raw_data.extend([157, 78, 221, 255])
            elif dist_sq < 60*60:
                # Anti-aliased border (#C77DFF)
                raw_data.extend([199, 125, 255, 200])
            else:
                raw_data.extend([0, 0, 0, 0])

    compressed = zlib.compress(bytes(raw_data))

    def make_chunk(tag, data):
        return struct.pack(">I", len(data)) + tag + data + struct.pack(">I", zlib.crc32(tag + data) & 0xffffffff)

    png = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0)
    png += make_chunk(b"IHDR", ihdr)
    png += make_chunk(b"IDAT", compressed)
    png += make_chunk(b"IEND", b"")
    return png

# --- 2. VS Code Extension Package Manifest ---
PACKAGE_JSON = {
    "name": "ube",
    "displayName": "Ube Language",
    "description": "Rich language support for Ube: syntax highlighting, snippets, and bracket matching 🍠",
    "version": "0.4.0",
    "publisher": "ube-lang",
    "icon": "icon.png",
    "engines": {
        "vscode": "^1.70.0"
    },
    "categories": [
        "Programming Languages",
        "Snippets"
    ],
    "keywords": ["ube", "systems", "go", "c", "zero-gc"],
    "contributes": {
        "languages": [
            {
                "id": "ube",
                "aliases": ["Ube", "ube"],
                "extensions": [".ube"],
                "configuration": "./language-configuration.json",
                "icon": {
                    "light": "./icon.png",
                    "dark": "./icon.png"
                }
            }
        ],
        "grammars": [
            {
                "language": "ube",
                "scopeName": "source.ube",
                "path": "./syntaxes/ube.tmLanguage.json"
            }
        ],
        "snippets": [
            {
                "language": "ube",
                "path": "./snippets/ube.code-snippets"
            }
        ]
    }
}

# --- 3. Language Configuration (Auto-brackets, Comments, Indents) ---
LANG_CONFIG = {
    "comments": {
        "lineComment": "//",
        "blockComment": ["/*", "*/"]
    },
    "brackets": [
        ["{", "}"],
        ["[", "]"],
        ["(", ")"]
    ],
    "autoClosingPairs": [
        { "open": "{", "close": "}" },
        { "open": "[", "close": "]" },
        { "open": "(", "close": ")" },
        { "open": "\"", "close": "\"", "notIn": ["string"] }
    ],
    "surroundingPairs": [
        ["{", "}"],
        ["[", "]"],
        ["(", ")"],
        ["\"", "\""]
    ],
    "indentationRules": {
        "increaseIndentPattern": "^.*\\{[^}\"']*$",
        "decreaseIndentPattern": "^\\s*\\}"
    }
}

# --- 4. TextMate Grammar for Syntax Highlighting ---
TM_LANGUAGE = {
    "$schema": "https://raw.githubusercontent.com/martinring/tmlanguage/master/tmlanguage.json",
    "name": "Ube",
    "scopeName": "source.ube",
    "patterns": [
        { "include": "#comments" },
        { "include": "#strings" },
        { "include": "#keywords" },
        { "include": "#types" },
        { "include": "#constants" },
        { "include": "#functions" },
        { "include": "#operators" },
        { "include": "#pragmas" }
    ],
    "repository": {
        "comments": {
            "patterns": [
                { "name": "comment.line.double-slash.ube", "match": "//.*$" },
                { "name": "comment.block.ube", "begin": "/\\*", "end": "\\*/" }
            ]
        },
        "strings": {
            "patterns": [
                {
                    "name": "string.quoted.double.ube",
                    "begin": "\"",
                    "end": "\"",
                    "patterns": [{ "name": "constant.character.escape.ube", "match": "\\\\." }]
                }
            ]
        },
        "pragmas": {
            "patterns": [
                { "name": "keyword.preprocessor.ube", "match": "^#(c_include|c_source)\\b" }
            ]
        },
        "keywords": {
            "patterns": [
                { "name": "keyword.control.ube", "match": "\\b(if|else|while|for|in|return|match|select|spawn|tuber|or_else)\\b" },
                { "name": "keyword.other.ube", "match": "\\b(import|package|struct|enum|let|mut|extern)\\b" }
            ]
        },
        "types": {
            "patterns": [
                { "name": "support.type.primitive.ube", "match": "\\b(i32|u32|i64|u64|f32|f64|bool|string|void)\\b" },
                { "name": "support.type.collection.ube", "match": "\\b(Chan|Pool|Option|Result)\\b" },
                { "name": "entity.name.type.ube", "match": "\\b[A-Z][a-zA-Z0-9_]*\\b" }
            ]
        },
        "constants": {
            "patterns": [
                { "name": "constant.language.ube", "match": "\\b(true|false|null)\\b" },
                { "name": "constant.numeric.integer.ube", "match": "\\b[0-9]+(_[0-9]+)*\\b" },
                { "name": "constant.numeric.float.ube", "match": "\\b[0-9]+\\.[0-9]+([eE][+-]?[0-9]+)?\\b" },
                { "name": "constant.numeric.hex.ube", "match": "\\b0x[0-9a-fA-F]+(_[0-9a-fA-F]+)*\\b" }
            ]
        },
        "functions": {
            "patterns": [
                {
                    "match": "\\bfn\\s*\\(\\s*(\\w+)\\s+([\\w*]+)\\s*\\)\\s*([a-zA-Z_][a-zA-Z0-9_]*)",
                    "captures": {
                        "1": { "name": "variable.parameter.receiver.ube" },
                        "2": { "name": "entity.name.type.ube" },
                        "3": { "name": "entity.name.function.ube" }
                    }
                },
                {
                    "match": "\\bfn\\s+([a-zA-Z_][a-zA-Z0-9_]*)",
                    "captures": {
                        "1": { "name": "entity.name.function.ube" }
                    }
                }
            ]
        },
        "operators": {
            "patterns": [
                { "name": "keyword.operator.arrow.ube", "match": "(->|=>)" },
                { "name": "keyword.operator.try.ube", "match": "\\?" },
                { "name": "keyword.operator.result.ube", "match": "!" },
                { "name": "keyword.operator.comparison.ube", "match": "(==|!=|<=|>=|<|>)" },
                { "name": "keyword.operator.assignment.ube", "match": "(=|\\+=|-=|\\*=|/=|%=)" },
                { "name": "keyword.operator.arithmetic.ube", "match": "(\\+|-|\\*|/|%)" },
                { "name": "keyword.operator.pointer.ube", "match": "(&|\\*)" }
            ]
        }
    }
}

# --- 5. Code Snippets ---
SNIPPETS = {
    "Main Function": {
        "prefix": "main",
        "body": [
            "fn main() {",
            "\t${1:println(\"Kamusta, Ube! 🍠\")}",
            "}"
        ],
        "description": "Standard main entry point"
    },
    "Function": {
        "prefix": "fn",
        "body": [
            "fn ${1:name}(${2:params}) -> ${3:i32} {",
            "\t${0}",
            "}"
        ],
        "description": "Standard function"
    },
    "Receiver Method": {
        "prefix": "fnrecv",
        "body": [
            "fn (${1:self} ${2:Type}) ${3:method_name}(${4:params}) -> ${5:void} {",
            "\t${0}",
            "}"
        ],
        "description": "Go-style struct receiver method"
    },
    "Struct": {
        "prefix": "struct",
        "body": [
            "struct ${1:Name} {",
            "\t${2:field}: ${3:i32},",
            "}"
        ],
        "description": "Struct declaration"
    },
    "Tagged Enum": {
        "prefix": "enum",
        "body": [
            "enum ${1:Name} {",
            "\t${2:Variant}(${3:i32}),",
            "\t${4:Other}",
            "}"
        ],
        "description": "Tagged union / sum type"
    },
    "Pattern Match": {
        "prefix": "match",
        "body": [
            "match ${1:variable} {",
            "\t${2:Variant}(${3:x}) => {",
            "\t\t${0}",
            "\t},",
            "}"
        ],
        "description": "Pattern matching on enums"
    },
    "Tuber Scratchpad": {
        "prefix": "tuber",
        "body": [
            "tuber {",
            "\t${0}",
            "} // Wiped in 1 instruction!"
        ],
        "description": "Scoped regional memory reset block"
    },
    "Spawn Vine": {
        "prefix": "spawn",
        "body": [
            "spawn ${1:worker_function}(${2:channel})"
        ],
        "description": "Spawn a concurrent task"
    }
}

README_MD = """# 🍠 Ube Language Support for VS Code

Rich language features and syntax highlighting for **Ube** — the sweet, grounded systems language.

## Features
* **Full Syntax Highlighting:** Primitives, structs, enums, receiver methods, and operators (`?`, `!`, `->`).
* **Snippets Included:** Type `fn`, `struct`, `enum`, `match`, `tuber`, or `spawn` to expand complete templates.
* **Auto-closing & Indentation:** Smart bracket and quote management.
"""

VSIX_MANIFEST = """<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011">
  <Metadata>
    <Identity Id="ube" Version="0.4.0" Language="en-US" Publisher="ube-lang" />
    <DisplayName>Ube Language</DisplayName>
    <Description xml:space="preserve">Rich language support for Ube 🍠</Description>
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
</PackageManifest>
"""

CONTENT_TYPES_XML = """<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="json" ContentType="application/json" />
  <Default Extension="md" ContentType="text/markdown" />
  <Default Extension="png" ContentType="image/png" />
  <Default Extension="vsixmanifest" ContentType="text/xml" />
</Types>
"""

def build_vsix(out_filename="ube-0.4.0.vsix"):
    print(f"\033[1;35m🍠 Ube VSIX Packager\033[0m")
    print(f"   Building package: \033[1;32m{out_filename}\033[0m...")

    icon_png = generate_png_icon()

    files = {
        "[Content_Types].xml": CONTENT_TYPES_XML.encode("utf-8"),
        "extension.vsixmanifest": VSIX_MANIFEST.encode("utf-8"),
        "extension/package.json": json.dumps(PACKAGE_JSON, indent=2).encode("utf-8"),
        "extension/language-configuration.json": json.dumps(LANG_CONFIG, indent=2).encode("utf-8"),
        "extension/syntaxes/ube.tmLanguage.json": json.dumps(TM_LANGUAGE, indent=2).encode("utf-8"),
        "extension/snippets/ube.code-snippets": json.dumps(SNIPPETS, indent=2).encode("utf-8"),
        "extension/README.md": README_MD.encode("utf-8"),
        "extension/icon.png": icon_png
    }

    with zipfile.ZipFile(out_filename, "w", zipfile.ZIP_DEFLATED) as zf:
        for archive_name, data in files.items():
            zf.writestr(archive_name, data)
            print(f"   + packed {archive_name}")

    print(f"\n\033[1;32m✔ Success!\033[0m Created \033[1m{out_filename}\033[0m ({os.path.getsize(out_filename)} bytes)\n")
    print("To install in VS Code:")
    print(f"  \033[1;36mcode --install-extension {out_filename}\033[0m")

if __name__ == "__main__":
    build_vsix()
