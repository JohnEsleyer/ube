#!/usr/bin/env python3
import sys
import os
import re

class CompileError(Exception):
    def __init__(self, filename, line_num, msg, line_src=""):
        self.filename = filename
        self.line_num = line_num
        self.msg = msg
        self.line_src = line_src
        super().__init__(f"[{filename}:{line_num}] Error: {msg}\n    | {line_src.strip()}")

class UbeCompiler:
    def __init__(self, entry_file, is_wasm=False):
        self.entry_file = entry_file
        self.is_wasm = is_wasm
        self.output = []
        self.c_includes = set()
        self.c_sources = []
        
        # Type Registries
        self.structs = {}          # name -> [(field, type)]
        self.enums = {}            # name -> [(variant, [types])]
        self.generic_slices = set() # { 'User', 'Particle', 'f32', 'i32', ... }
        self.generic_options = set()# { 'i32', 'User', ... }
        self.generic_results = set()# { ('i32', 'string'), ('Config', 'string'), ... }
        self.generic_pools = set()  # { 'Entity', 'Particle', ... }
        
        self.current_fn_ret = None
        self.parsed_files = set()
        self.indent = 0

    def error(self, msg, line_num=0, line_src=""):
        raise CompileError(self.entry_file, line_num, msg, line_src)

    def mangle_type(self, t):
        clean = t.replace(" ", "_").replace("*", "ptr_").replace("[]", "slice_").replace("?", "_opt")
        return re.sub(r'[^a-zA-Z0-9_]', '_', clean)

    def to_c_type(self, t):
        t = t.strip()
        if t == "i32": return "int32_t"
        if t == "u32": return "uint32_t"
        if t == "u8": return "uint8_t"
        if t == "i64": return "int64_t"
        if t == "u64": return "uint64_t"
        if t == "f32": return "float"
        if t == "f64": return "double"
        if t == "bool": return "bool"
        if t == "string": return "string"
        if t == "void": return "void"
        if t == "*void": return "void*"
        if t == "**void": return "void**"
        if t.startswith("*"):
            inner = t[1:].strip()
            return f"{self.to_c_type(inner)}*"
        if t.startswith("[]"):
            inner = t[2:].strip()
            m = self.mangle_type(inner)
            self.generic_slices.add(inner)
            return f"UbeSlice_{m}"
        if t.endswith("?"):
            inner = t[:-1].strip()
            m = self.mangle_type(inner)
            self.generic_options.add(inner)
            return f"UbeOption_{m}"
        if "!" in t:
            parts = [p.strip() for p in t.split("!")]
            val_t, err_t = parts[0], parts[1]
            m = f"{self.mangle_type(val_t)}_{self.mangle_type(err_t)}"
            self.generic_results.add((val_t, err_t))
            return f"UbeResult_{m}"
        if t.startswith("Chan[") and t.endswith("]"):
            return "UbeRawChan*"
        if t.startswith("Pool[") and t.endswith("]"):
            inner = t[5:-1].strip()
            self.generic_pools.add(inner)
            return f"Pool_{inner}*"
        return t

    def emit(self, line):
        self.output.append("    " * self.indent + line)

    def resolve_import_path(self, current_file, import_str):
        vendor_path = os.path.join(os.getcwd(), "vendor", import_str)
        if os.path.isdir(vendor_path):
            mod_file = os.path.join(vendor_path, "mod.ube")
            if os.path.exists(mod_file): return mod_file
            
        cur_dir = os.path.dirname(os.path.abspath(current_file))
        rel_path = os.path.join(cur_dir, import_str + ".ube")
        if os.path.exists(rel_path): return rel_path
        
        cache_path = os.path.expanduser(f"~/.ube/pkg/{import_str}")
        if os.path.isdir(cache_path):
            main_file = os.path.join(cache_path, "main.ube")
            if os.path.exists(main_file): return main_file
            
        if import_str.startswith("std/"):
            return None
        return None

    def preprocess_and_parse(self, filepath):
        if filepath in self.parsed_files or filepath is None:
            return []
        self.parsed_files.add(filepath)

        if not os.path.exists(filepath):
            self.error(f"Cannot find source file: {filepath}")

        with open(filepath, "r", encoding="utf-8", errors="ignore") as f:
            raw_lines = f.readlines()

        cleaned = []
        for idx, l in enumerate(raw_lines):
            line_no = idx + 1
            s = l.strip()
            if not s or s.startswith("//"):
                continue
            if s.startswith("#c_include"):
                inc = s.split(None, 1)[1].strip()
                self.c_includes.add(inc)
                continue
            if s.startswith("#c_source"):
                src = s.split(None, 1)[1].strip().strip('"')
                self.c_sources.append(src)
                continue
            if s.startswith("import "):
                imp_match = re.match(r'import\s+"([^"]+)"', s)
                if imp_match:
                    imp_str = imp_match.group(1)
                    sub_file = self.resolve_import_path(filepath, imp_str)
                    if sub_file:
                        cleaned.extend(self.preprocess_and_parse(sub_file))
                continue
            cleaned.append((filepath, line_no, s))
        return cleaned

    def compile(self):
        token_lines = self.preprocess_and_parse(self.entry_file)
        
        # Pre-pass: Discover structs & enums across all files
        i = 0
        total = len(token_lines)
        while i < total:
            fpath, lno, line = token_lines[i]
            if line.startswith("struct ") and line.endswith("{"):
                sname = line.split()[1]
                fields = []
                i += 1
                while i < total and token_lines[i][2] != "}":
                    fline = token_lines[i][2].rstrip(",")
                    parts = [p.strip() for p in fline.split(":")]
                    if len(parts) == 2:
                        fields.append((parts[0], parts[1]))
                    i += 1
                self.structs[sname] = fields
            elif line.startswith("enum ") and line.endswith("{"):
                ename = line.split()[1]
                variants = []
                i += 1
                while i < total and token_lines[i][2] != "}":
                    vline = token_lines[i][2].rstrip(",")
                    m_v = re.match(r"(\w+)(?:\((.*?)\))?", vline)
                    if m_v:
                        v_name = m_v.group(1)
                        v_types = [t.strip() for t in m_v.group(2).split(",")] if m_v.group(2) else []
                        variants.append((v_name, v_types))
                    i += 1
                self.enums[ename] = variants
            i += 1

        body_lines = []
        old_emit = self.emit
        self.emit = lambda l: body_lines.append("    " * self.indent + l)

        i = 0
        while i < total:
            fpath, lno, line = token_lines[i]

            # 1. Struct Definition (Already scanned, skip tokens)
            if line.startswith("struct ") and line.endswith("{"):
                i += 1
                while i < total and token_lines[i][2] != "}":
                    i += 1
                i += 1
                continue

            # 2. Enum Definition (Already scanned, skip tokens)
            if line.startswith("enum ") and line.endswith("{"):
                i += 1
                while i < total and token_lines[i][2] != "}":
                    i += 1
                i += 1
                continue

            # 3. extern fn declaration
            if line.startswith("extern fn "):
                m_ext = re.match(r"extern\s+fn\s+(\w+)\s*\((.*?)\)(?:\s*->\s*(.+))?", line)
                if not m_ext:
                    self.error("Malformed extern fn declaration", lno, line)
                ename, params, ret = m_ext.groups()
                # If puts, avoid conflicting redeclaration with stdio puts
                if ename == "puts":
                    i += 1
                    continue
                c_ret = self.to_c_type(ret) if ret else "void"
                c_params = []
                if params.strip():
                    for p in params.split(","):
                        p_parts = [x.strip() for x in p.split(":")]
                        c_params.append(f"{self.to_c_type(p_parts[1])} {p_parts[0]}")
                self.emit(f"extern {c_ret} {ename}({', '.join(c_params)});")
                i += 1
                continue

            # 4. Receiver Methods
            m_recv = re.match(r"fn\s*\(\s*(\w+)\s+([\w\*]+)\s*\)\s*(\w+)\s*\((.*?)\)(?:\s*->\s*(.+?))?\s*\{", line)
            if m_recv:
                r_var, r_type, fn_name, params, ret_t = m_recv.groups()
                c_ret = self.to_c_type(ret_t) if ret_t else "void"
                self.current_fn_ret = ret_t
                c_params = [f"{self.to_c_type(r_type)} {r_var}"]
                if params.strip():
                    for p in params.split(","):
                        p_parts = [x.strip() for x in p.split(":")]
                        c_params.append(f"{self.to_c_type(p_parts[1])} {p_parts[0]}")
                clean_rtype = r_type.replace("*", "_ptr")
                self.emit(f"{c_ret} {clean_rtype}_{fn_name}({', '.join(c_params)}) {{")
                self.indent += 1
                self.emit("size_t __watermark = ube_tuber_save();")
                i += 1
                continue

            # 5. Functions
            m_fn = re.match(r"fn\s+(\w+)\s*\((.*?)\)(?:\s*->\s*(.+?))?\s*\{", line)
            if m_fn:
                fn_name, params, ret_t = m_fn.groups()
                c_ret = self.to_c_type(ret_t) if ret_t else "void"
                self.current_fn_ret = ret_t
                c_params = []
                if params.strip():
                    for p in params.split(","):
                        p_parts = [x.strip() for x in p.split(":")]
                        c_params.append(f"{self.to_c_type(p_parts[1])} {p_parts[0]}")
                if fn_name == "main":
                    self.emit("int main(int argc, char** argv) {")
                    self.indent += 1
                    self.emit("(void)argc; (void)argv;")
                    self.emit("ube_init();")
                    self.emit("size_t __watermark = ube_tuber_save();")
                else:
                    self.emit(f"{c_ret} {fn_name}({', '.join(c_params)}) {{")
                    self.indent += 1
                    if ret_t and not ret_t.startswith("[]"):
                        self.emit("size_t __watermark = ube_tuber_save();")
                i += 1
                continue

            # 6. Returns
            if line.startswith("return"):
                ret_val = line[6:].strip()
                if not ret_val:
                    if self.current_fn_ret:
                        self.emit("ube_tuber_restore(__watermark);")
                    self.emit("return;")
                elif self.current_fn_ret and self.current_fn_ret.startswith("[]"):
                    self.emit(f"return {ret_val};")
                elif self.current_fn_ret and self.current_fn_ret.endswith("?"):
                    inner = self.current_fn_ret[:-1].strip()
                    m = self.mangle_type(inner)
                    self.emit("ube_tuber_restore(__watermark);")
                    if ret_val == "null":
                        self.emit(f"return (UbeOption_{m}){{ .has_val = false }};")
                    else:
                        self.emit(f"return (UbeOption_{m}){{ .has_val = true, .val = {ret_val} }};")
                elif self.current_fn_ret and "!" in self.current_fn_ret:
                    parts = [p.strip() for p in self.current_fn_ret.split("!")]
                    m = f"{self.mangle_type(parts[0])}_{self.mangle_type(parts[1])}"
                    self.emit("ube_tuber_restore(__watermark);")
                    if ret_val.startswith("error(") and ret_val.endswith(")"):
                        self.emit(f"return (UbeResult_{m}){{ .ok = false, .err = ube_str_lit({ret_val[6:-1]}) }};")
                    else:
                        self.emit(f"return (UbeResult_{m}){{ .ok = true, .val = {ret_val} }};")
                else:
                    if self.current_fn_ret != "main":
                        self.emit("ube_tuber_restore(__watermark);")
                    self.emit(f"return {ret_val};")
                i += 1
                continue

            # 7. Multi-line Block Pattern Matching (match x { ... })
            m_match = re.match(r"match\s+(\w+)\s*\{", line)
            if m_match:
                m_var = m_match.group(1)
                self.emit(f"switch ({m_var}.tag) {{")
                self.indent += 1
                i += 1
                while i < total and token_lines[i][2] != "}":
                    _, alno, aline = token_lines[i]
                    m_arm = re.match(r"(\w+)(?:\((.*?)\))?\s*=>\s*(.*)", aline.rstrip(","))
                    if m_arm:
                        v_name, binding, body_start = m_arm.groups()
                        target_enum = None
                        for ename, evars in self.enums.items():
                            if any(v[0] == v_name for v in evars):
                                target_enum = ename
                                break
                        self.emit(f"case {target_enum}_{v_name}: {{")
                        self.indent += 1
                        if binding:
                            for b_idx, b_var in enumerate([b.strip() for b in binding.split(",")]):
                                self.emit(f"__auto_type {b_var} = {m_var}.as.{v_name}._{b_idx};")
                        
                        if body_start.startswith("{"):
                            i += 1
                            depth = 1
                            while i < total and depth > 0:
                                _, blno, bline = token_lines[i]
                                if "{" in bline: depth += bline.count("{")
                                if "}" in bline: depth -= bline.count("}")
                                if depth > 0:
                                    self.translate_statement(blno, bline)
                                i += 1
                            self.emit("break;")
                            self.indent -= 1
                            self.emit("}")
                            continue
                        else:
                            self.translate_statement(alno, body_start)
                            self.emit("break;")
                            self.indent -= 1
                            self.emit("}")
                    i += 1
                self.indent -= 1
                self.emit("}")
                i += 1
                continue

            # 8. while let (Closed channel loops)
            m_wlet = re.match(r"while\s+let\s+(\w+)\s*=\s*(\w+)\.recv\(\)\s*\{", line)
            if m_wlet:
                val_name, chan_name = m_wlet.groups()
                self.emit(f"while (1) {{")
                self.indent += 1
                self.emit(f"int32_t {val_name};")
                self.emit(f"if (!ube_raw_chan_recv({chan_name}, &{val_name})) break;")
                i += 1
                continue

            # 9. if let ... else
            m_iflet = re.match(r"if\s+let\s+(\w+)\s*=\s*(.+?)\s*\{", line)
            if m_iflet:
                vname, opt_expr = m_iflet.groups()
                tmp = f"__opt_{vname}_{lno}"
                self.emit(f"__auto_type {tmp} = {opt_expr};")
                self.emit(f"if ({tmp}.has_val) {{")
                self.indent += 1
                self.emit(f"__auto_type {vname} = {tmp}.val;")
                i += 1
                continue

            # 10. Control flow: else if / else / if
            if line.startswith("else if ") and line.endswith("{"):
                cond = line[8:-1].strip()
                self.indent -= 1
                self.emit(f"}} else if ({cond}) {{")
                self.indent += 1
                i += 1
                continue

            if line == "else {":
                self.indent -= 1
                self.emit("} else {")
                self.indent += 1
                i += 1
                continue

            if line.startswith("if ") and line.endswith("{"):
                cond = line[3:-1].strip()
                self.emit(f"if ({cond}) {{")
                self.indent += 1
                i += 1
                continue

            # 11. Select statement (Channel multiplexer)
            if line == "select {":
                self.emit("while (1) {")
                self.indent += 1
                i += 1
                while i < total and token_lines[i][2] != "}":
                    _, slno, sline = token_lines[i]
                    m_sarm = re.match(r"(\w+)\s*=\s*(\w+)\.recv\(\)\s*=>\s*\{", sline)
                    if m_sarm:
                        item_v, chan_v = m_sarm.groups()
                        self.emit(f"int32_t {item_v};")
                        self.emit(f"if (ube_raw_chan_try_recv({chan_v}, &{item_v})) {{")
                        self.indent += 1
                        i += 1
                        while i < total and token_lines[i][2] != "}":
                            self.translate_statement(token_lines[i][1], token_lines[i][2])
                            i += 1
                        self.emit("break;")
                        self.indent -= 1
                        self.emit("}")
                    i += 1
                self.emit("struct timespec __ts = {0, 1000000}; nanosleep(&__ts, NULL);")
                self.indent -= 1
                self.emit("}")
                i += 1
                continue

            # 12. Standard statement translation
            self.translate_statement(lno, line)
            i += 1

        self.emit = old_emit
        
        # --- Output Generation ---
        header = ['#include "ube_runtime.h"']
        for inc in sorted(self.c_includes):
            header.append(f'#include "{inc.strip()}"')
        header.append("")

        # 1. Forward declare Structs
        for sname in sorted(self.structs.keys()):
            header.append(f"typedef struct {sname} {sname};")
        header.append("")

        # 2. Define Structs
        for sname in sorted(self.structs.keys()):
            header.append(f"struct {sname} {{")
            for fname, ftype in self.structs[sname]:
                header.append(f"    {self.to_c_type(ftype)} {fname};")
            header.append("};")
            header.append("")

        # 3. Define Enums
        for ename, variants in sorted(self.enums.items()):
            header.append("typedef enum {")
            for vn, _ in variants:
                header.append(f"    {ename}_{vn},")
            header.append(f"}} {ename}_Tag;")
            header.append("typedef struct {")
            header.append(f"    {ename}_Tag tag;")
            header.append("    union {")
            for vn, vtypes in variants:
                if vtypes:
                    flds = [f"{self.to_c_type(t)} _{idx};" for idx, t in enumerate(vtypes)]
                    header.append(f"        struct {{ {' '.join(flds)} }} {vn};")
            header.append("    } as;")
            header.append(f"}} {ename};")
            header.append("")

        # 4. Generate Slices
        for inner in sorted(self.generic_slices):
            m = self.mangle_type(inner)
            ct = self.to_c_type(inner)
            header.append(f"typedef struct {{ {ct}* data; size_t len; }} UbeSlice_{m};")
            header.append(f"static inline UbeSlice_{m} ube_slice_{m}_new() {{ UbeSlice_{m} s; s.data = ({ct}*)&current_tuber.buffer[current_tuber.top]; s.len = 0; return s; }}")
            header.append(f"static inline void ube_slice_{m}_push(UbeSlice_{m}* s, {ct} val) {{ {ct}* slot = ({ct}*)ube_bump_alloc_unaligned(sizeof({ct})); if (s->len == 0) s->data = slot; *slot = val; s->len++; }}")

        # 5. Generate Options
        for inner in sorted(self.generic_options):
            m = self.mangle_type(inner)
            ct = self.to_c_type(inner)
            header.append(f"typedef struct {{ bool has_val; {ct} val; }} UbeOption_{m};")

        # 6. Generate Results
        for val_t, err_t in sorted(self.generic_results):
            m = f"{self.mangle_type(val_t)}_{self.mangle_type(err_t)}"
            c_val = self.to_c_type(val_t)
            c_err = self.to_c_type(err_t)
            header.append(f"typedef struct {{ bool ok; union {{ {c_val} val; {c_err} err; }}; }} UbeResult_{m};")

        # 7. Generate Pools
        for p_t in sorted(self.generic_pools):
            header.append(f"DEFINE_UBE_POOL({p_t})")

        header.append("")
        header.append("static inline void ube_fmt_println_str_helper(string s) { printf(\"%.*s\\n\", (int)s.len, s.data); }")
        header.append("static inline void ube_fmt_println_c_str_helper(const char* s) { printf(\"%s\\n\", s); }")
        header.append("static inline void ube_fmt_println_ptr_helper(const void* p) { printf(\"%p\\n\", p); }")
        header.append("")

        return "\n".join(header) + "\n" + "\n".join(body_lines)

    def convert_struct_literal(self, expr):
        def repl(match):
            sname = match.group(1)
            flds = match.group(2)
            c_flds = re.sub(r'(\b\w+\b)\s*:', r'.\1 =', flds)
            return f"({sname}){{ {c_flds} }}"
        return re.sub(r'\b([A-Z]\w*)\s*\{([^}]*)\}', repl, expr)

    def translate_statement(self, lno, line):
        # Struct Field Mutation: user.name = "Alice" or particles.data[0].x = 99.0
        m_mut = re.match(r"([\w\.\[\]]+)\s*=\s*(.*)", line)
        if m_mut and not line.startswith("let ") and not line.startswith("if ") and not line.startswith("while "):
            lhs, rhs = m_mut.groups()
            rhs_c = self.convert_struct_literal(rhs.strip())
            self.emit(f"{lhs} = {rhs_c};")
            return

        # Slices: let mut team = []User{}
        m_sl = re.match(r"let\s+mut\s+(\w+)\s*=\s*\[\](\w+)\{\}", line)
        if m_sl:
            var, inner = m_sl.groups()
            m = self.mangle_type(inner)
            self.generic_slices.add(inner)
            self.emit(f"UbeSlice_{m} {var} = ube_slice_{m}_new();")
            return

        # Explicit Types: let x: f32 = 3.14
        m_exp = re.match(r"let\s+(mut\s+)?(\w+)\s*:\s*([^=]+?)\s*=\s*(.*)", line)
        if m_exp:
            _, var, t_str, val = m_exp.groups()
            t_str = t_str.strip()
            val_c = self.convert_struct_literal(val.strip())
            if t_str.endswith("?"):
                inner = t_str[:-1].strip()
                m = self.mangle_type(inner)
                self.generic_options.add(inner)
                if val.strip() == "null":
                    self.emit(f"UbeOption_{m} {var} = (UbeOption_{m}){{ .has_val = false }};")
                else:
                    self.emit(f"UbeOption_{m} {var} = (UbeOption_{m}){{ .has_val = true, .val = {val_c} }};")
            else:
                c_type = self.to_c_type(t_str)
                self.emit(f"{c_type} {var} = {val_c};")
            return

        # Generic .push()
        m_psh = re.match(r"(\w+)\.push\((.*)\)", line)
        if m_psh:
            var, val = m_psh.groups()
            val_c = self.convert_struct_literal(val.strip())
            self.emit(f"{{ __auto_type _val = {val_c}; void* _slot = ube_bump_alloc_unaligned(sizeof(_val)); if (({var}).len == 0) ({var}).data = (__typeof__(({var}).data))_slot; memcpy(_slot, &_val, sizeof(_val)); ({var}).len++; }};")
            return

        # Error bubbling: let x = call()?
        m_try = re.match(r"let\s+(\w+)\s*=\s*(.+?)\?\s*$", line)
        if m_try:
            var, call = m_try.groups()
            tmp = f"__res_{var}_{lno}"
            self.emit(f"__auto_type {tmp} = {call};")
            self.emit(f"if (!{tmp}.ok) {{ ube_tuber_restore(__watermark); return {tmp}; }}")
            self.emit(f"__auto_type {var} = {tmp}.val;")
            return

        # Fallback: let x = call() or_else default
        m_or = re.match(r"let\s+(\w+)\s*=\s*(.+?)\s+or_else\s+(.*)", line)
        if m_or:
            var, call, default = m_or.groups()
            tmp = f"__res_{var}_{lno}"
            default_c = self.convert_struct_literal(default.strip())
            self.emit(f"__auto_type {tmp} = {call};")
            self.emit(f"__auto_type {var} = {tmp}.ok ? {tmp}.val : ({default_c});")
            return

        # Channel creation: let jobs = Chan[i32].new(4)
        m_chn = re.match(r"let\s+(\w+)\s*=\s*Chan\[(.*?)\]\.new\((\d+)\)", line)
        if m_chn:
            var, ct, cap = m_chn.groups()
            self.emit(f"UbeRawChan* {var} = ube_raw_chan_new({cap}, sizeof({self.to_c_type(ct)}));")
            return

        # Generic let
        m_let = re.match(r"let\s+(mut\s+)?(\w+)\s*=\s*(.*)", line)
        if m_let:
            _, var, rhs = m_let.groups()
            rhs = rhs.strip()
            # Struct init
            m_si = re.match(r"(\w+)\s*\{(.*)\}", rhs)
            if m_si and m_si.group(1) in self.structs:
                sname, flds = m_si.groups()
                c_flds = re.sub(r'(\b\w+\b)\s*:', r'.\1 =', flds)
                self.emit(f"{sname} {var} = {{ {c_flds} }};")
                return
            # Enum init
            m_ei = re.match(r"(\w+)\.(\w+)(?:\((.*?)\))?", rhs)
            if m_ei and m_ei.group(1) in self.enums:
                ename, vname, args = m_ei.groups()
                self.emit(f"{ename} {var};")
                self.emit(f"{var}.tag = {ename}_{vname};")
                if args:
                    for aidx, a in enumerate([x.strip() for x in args.split(",")]):
                        self.emit(f"{var}.as.{vname}._{aidx} = {a};")
                return
            # Reference: let r = &v
            if rhs.startswith("&"):
                self.emit(f"__auto_type {var} = {rhs};")
                return
            self.emit(f"__auto_type {var} = {self.convert_struct_literal(rhs)};")
            return

        # Loops
        m_for = re.match(r"for\s+(\w+)\s+in\s+(\w+)\s*\{", line)
        if m_for:
            it, s_var = m_for.groups()
            self.emit(f"for (size_t _i = 0; _i < {s_var}.len; _i++) {{")
            self.indent += 1
            self.emit(f"__auto_type {it} = {s_var}.data[_i];")
            return

        if ".send(" in line:
            m = re.match(r"(\w+)\.send\((.*)\)", line)
            if m:
                ch, val = m.groups()
                self.emit(f"{{ __auto_type __tmp = {val}; ube_raw_chan_send({ch}, &__tmp); }}")
                return

        if ".close()" in line:
            m = re.match(r"(\w+)\.close\(\)", line)
            if m:
                self.emit(f"ube_raw_chan_close({m.group(1)});")
                return

        # Spawn
        m_sp = re.match(r"spawn\s+(\w+)\((.*?)\)", line)
        if m_sp:
            target, arg = m_sp.groups()
            self.emit(f"ube_spawn((void*(*)(void*)){target}, (void*){arg});")
            return

        # Print helpers
        if line.startswith("fmt.println(") or line.startswith("println("):
            val = line[line.find("(")+1:-1].strip()
            if val.endswith(".ptr"):
                inner_val = val[:-4].strip()
                if inner_val.startswith('"') and inner_val.endswith('"'):
                    val = inner_val
                else:
                    val = f"({inner_val}).data"
            if val.startswith('"') and val.endswith('"'):
                self.emit(f'printf("%s\\n", {val});')
                return
            self.emit(f'_Generic(({val}), \\\n'
                      f'    int32_t: ube_fmt_println_i32, \\\n'
                      f'    uint32_t: ube_fmt_println_i32, \\\n'
                      f'    float: ube_fmt_println_f32, \\\n'
                      f'    double: ube_fmt_println_f32, \\\n'
                      f'    bool: ube_fmt_println_bool, \\\n'
                      f'    string: ube_fmt_println_str_helper, \\\n'
                      f'    char*: ube_fmt_println_c_str_helper, \\\n'
                      f'    const char*: ube_fmt_println_c_str_helper, \\\n'
                      f'    default: ube_fmt_println_ptr_helper \\\n'
                      f')({val});')
            return

        if ".ptr" in line:
            line = re.sub(r'(".*?")\.ptr', r'(uint8_t*)(\1)', line)
            line = re.sub(r'(\w+)\.ptr', r'(uint8_t*)((\1).data)', line)

        if line == "}":
            if self.indent == 1 and self.current_fn_ret == "main":
                self.emit("ube_tuber_restore(__watermark);")
                self.emit("return 0;")
            self.indent -= 1
            self.emit("}")
            return

        self.emit(f"{line};")

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 ubec.py <file.ube> [--wasm]")
        sys.exit(1)

    src_file = sys.argv[1]
    is_wasm = "--wasm" in sys.argv
    compiler = UbeCompiler(src_file, is_wasm=is_wasm)
    try:
        c_code = compiler.compile()
        out_path = "output.c"
        with open(out_path, "w", encoding="utf-8") as f:
            f.write(c_code)
        print(f"[ubec] Compiled {src_file} -> {out_path}")
    except CompileError as e:
        print(e, file=sys.stderr)
        sys.exit(1)
