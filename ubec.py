import sys
import re

# v0.4: shared-nothing Vines with private per-thread Tubers, typed flat
# channels (Chan[T] with new/send/recv/close) and `spawn`.
# Keeps v0.1-v0.3: slices/Tuber scopes, structs/receivers, strings,
# Result + `?`, enums + match, caller-allocated returns.

TYPE_MAP = {
    "i32": "int32_t",
    "i64": "int64_t",
    "u32": "uint32_t",
    "u64": "uint64_t",
    "u8": "uint8_t",
    "f32": "float",
    "f64": "double",
    "bool": "bool",
    "string": "string",
    "[]i32": "UbeSlice_i32",
}

# Types that may cross a Vine boundary by memcpy.
FLAT_SCALARS = {"i32", "i64", "u32", "u64", "u8", "f32", "f64", "bool"}

# Base types with a generated UbeOption_<base> runtime helper.
OPTION_BASES = {"i32", "i64", "u32", "u64", "u8", "f32", "f64", "bool",
                "string"}


def ctype(t):
    if not t:
        return "void"
    t = t.strip()
    if t.endswith("?"):
        return "UbeOption_%s" % t[:-1].strip()
    if re.match(r"^Chan\[.+\]$", t):
        return "UbeChan"
    return TYPE_MAP.get(t, t)


def result_parts(ret_type):
    """('value type', 'error type') for 'T ! E', else None."""
    if ret_type and "!" in ret_type:
        val, _, err = ret_type.partition("!")
        return val.strip(), err.strip()
    return None


def strip_line_comment(line):
    """Remove a trailing // comment, but not one inside a string literal."""
    in_string = False
    escaped = False
    for idx, ch in enumerate(line):
        if in_string:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                in_string = False
        elif ch == '"':
            in_string = True
        elif ch == "/" and idx + 1 < len(line) and line[idx + 1] == "/":
            return line[:idx]
    return line


def code_only(line):
    """Drop string-literal contents so keyword scans ignore prose."""
    out = []
    in_string = False
    escaped = False
    for ch in line:
        if in_string:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == '"':
                in_string = False
            continue
        if ch == '"':
            in_string = True
            continue
        out.append(ch)
    return "".join(out)


def split_top(text):
    """Split on top-level commas, ignoring commas inside [] () {}."""
    parts, cur, depth = [], "", 0
    for ch in text:
        if ch in "[({":
            depth += 1
        elif ch in "])}":
            depth -= 1
        if ch == "," and depth == 0:
            parts.append(cur)
            cur = ""
        else:
            cur += ch
    if cur.strip():
        parts.append(cur)
    return [p.strip() for p in parts if p.strip()]


class UbeTranspiler:
    def __init__(self, source):
        self.source = source
        self.output = []
        self.indent = 0
        # semantic tables
        self.structs = {}       # struct name -> [(field, ube type)]
        self.enums = {}         # enum name -> [(variant, [field types])]
        self.type_order = []    # declaration order of ("struct"|"enum", name)
        self.var_types = {}     # variable -> type text
        self.chan_elem = {}     # channel variable -> element type text
        self.fn_rets = {}       # function -> return type text (None = void)
        self.fn_params = {}     # function -> [(param, type)]
        self.methods = {}       # (struct, method) -> return type text
        self.spawn_targets = set()
        self.spawn_order = []
        self.current_fn_return_type = None

    def emit(self, line):
        self.output.append("    " * self.indent + line)

    # --- type helpers -------------------------------------------------
    def c_ret(self, ret_type):
        if not ret_type:
            return "void"
        parts = result_parts(ret_type)
        if parts:
            if ctype(parts[0]) != "int32_t":
                sys.stderr.write(
                    "[ubec]: warning: result value type '%s' has no runtime "
                    "helper yet; using UbeResult_i32\n" % parts[0])
            return "UbeResult_i32"
        return ctype(ret_type)

    def val_type(self, ret_type):
        if not ret_type:
            return "void"
        parts = result_parts(ret_type)
        return ctype(parts[0]) if parts else ctype(ret_type)

    def param_list(self, params, record=False):
        out = []
        if params.strip():
            for p in split_top(params):
                pname, sep, ptype = p.partition(":")
                if not sep:
                    continue
                pname, ptype = pname.strip(), ptype.strip()
                out.append("%s %s" % (ctype(ptype), pname))
                if record:
                    self.var_types[pname] = ptype
                    m_chan = re.match(r"^Chan\[(.+)\]$", ptype)
                    if m_chan:
                        elem = m_chan.group(1).strip()
                        self.chan_elem[pname] = elem
                        self.check_flat(elem, "channel parameter '%s'" % pname)
        return out

    # --- flat-only rule -----------------------------------------------
    def is_flat(self, t, seen=None):
        """True if t may be copied by memcpy across a Vine boundary."""
        if not t:
            return False
        t = t.strip()
        if t in FLAT_SCALARS:
            return True
        if re.match(r"^Chan\[.+\]$", t):
            return True
        if t.startswith("&") or t.startswith("*"):
            return False
        if t in ("string", "[]i32"):
            return False
        if seen is None:
            seen = set()
        if t in seen:
            return False
        seen = seen | {t}
        if t in self.structs:
            return all(self.is_flat(ft, seen) for _, ft in self.structs[t])
        if t in self.enums:
            return all(self.is_flat(ft, seen)
                       for _, payload in self.enums[t] for ft in payload)
        return False

    def check_flat(self, t, context):
        if not self.is_flat(t):
            sys.stderr.write(
                "[ubec]: error: cannot pass non-flat type '%s' across Vines "
                "(%s)\n        channels and spawn arguments may only carry "
                "flat data: primitives, channel handles, flat structs, and "
                "flat enums\n" % (t, context))
            sys.exit(1)

    def expr_type(self, expr):
        expr = expr.strip()
        if expr in self.var_types:
            return self.var_types[expr]
        if self.is_string_literal(expr):
            return "string"
        m = re.match(r"(\w+)\s*\{", expr)
        if m and (m.group(1) in self.structs or m.group(1) in self.enums):
            return m.group(1)
        m = re.match(r"(\w+)\.(\w+)\s*\(", expr)
        if m and m.group(1) in self.enums:
            return m.group(1)
        if re.match(r"^-?\d+$", expr):
            return "i32"
        m = re.match(r"(\w+)\.(\w+)$", expr)
        if m and m.group(1) in self.var_types:
            stype = self.var_types[m.group(1)]
            for fname, ftype in self.structs.get(stype, []):
                if fname == m.group(2):
                    return ftype
        ret = self.call_ret(expr)
        if ret:
            parts = result_parts(ret)
            return parts[0] if parts else ret
        return None

    # --- string expressions -------------------------------------------
    @staticmethod
    def is_string_literal(term):
        return len(term) >= 2 and term[0] == '"' and term[-1] == '"'

    def is_string_expr(self, expr):
        terms = [t.strip() for t in expr.split("+")]
        return bool(terms) and (
            self.is_string_literal(terms[0])
            or self.var_types.get(terms[0]) == "string")

    def gen_string_expr(self, expr):
        """Lower `a + b + "c"` into nested ube_str_concat(...) calls."""
        out = None
        for term in [t.strip() for t in expr.split("+")]:
            if self.is_string_literal(term):
                piece = "ube_str_lit(%s)" % term
            elif self.var_types.get(term) == "string":
                piece = term
            else:
                sys.stderr.write("[ubec]: error: '%s' is not a string\n" % term)
                sys.exit(1)
            out = piece if out is None else "ube_str_concat(%s, %s)" % (out, piece)
        return out

    # --- look up the declared return type of a call -------------------
    def call_ret(self, expr):
        m = re.match(r"(\w+)\.(\w+)\s*\(", expr)
        if m:
            stype = self.var_types.get(m.group(1))
            if stype and (stype, m.group(2)) in self.methods:
                return self.methods[(stype, m.group(2))]
            return None
        m = re.match(r"(\w+)\s*\(", expr)
        if m:
            return self.fn_rets.get(m.group(1))
        return None

    # --- struct literals ----------------------------------------------
    def struct_init_literal(self, expr):
        """Lower `Job{ id: 1, weight: 2 }` to a C compound literal."""
        m = re.match(r"(\w+)\s*\{(.*)\}\s*$", expr)
        if not m or m.group(1) not in self.structs:
            return None
        tname, vals = m.group(1), m.group(2)
        inits = []
        for part in split_top(vals):
            fname, sep, fval = part.partition(":")
            if not sep:
                return None
            inits.append(".%s = %s" % (fname.strip(), fval.strip()))
        return "(%s){ %s }" % (ctype(tname), ", ".join(inits))

    # --- pre-scan: signatures, spawn targets, type declarations -------
    @staticmethod
    def parse_params(text):
        out = []
        if not text.strip():
            return out
        for p in split_top(text):
            name, sep, ptype = p.partition(":")
            if sep:
                out.append((name.strip(), ptype.strip()))
        return out

    def prescan(self, lines):
        i = 0
        while i < len(lines):
            line = lines[i]

            if line.startswith("enum ") and line.endswith("{"):
                ename = line.split()[1]
                variants = []
                i += 1
                while i < len(lines) and lines[i] != "}":
                    m_v = re.match(r"(\w+)\s*(?:\((.*?)\))?\s*,?$", lines[i])
                    if m_v:
                        v_types = ([t.strip() for t in split_top(m_v.group(2))]
                                   if m_v.group(2) else [])
                        variants.append((m_v.group(1), v_types))
                    i += 1
                self.enums[ename] = variants
                self.type_order.append(("enum", ename))
                i += 1
                continue

            if line.startswith("struct ") and line.endswith("{"):
                sname = line.split()[1]
                fields = []
                i += 1
                while i < len(lines) and lines[i] != "}":
                    parts = lines[i].rstrip(",").split(":")
                    if len(parts) >= 2:
                        fields.append((parts[0].strip(), parts[1].strip()))
                    i += 1
                self.structs[sname] = fields
                self.type_order.append(("struct", sname))
                i += 1
                continue

            m_fn = re.match(
                r"fn\s+(\w+)\s*\((.*?)\)\s*(?:->\s*(.+?))?\s*\{$", line)
            if m_fn and m_fn.group(1) != "main":
                fname = m_fn.group(1)
                self.fn_params[fname] = self.parse_params(m_fn.group(2))
                self.fn_rets[fname] = (m_fn.group(3).strip()
                                       if m_fn.group(3) else None)

            m_spawn = re.match(r"spawn\s+(\w+)\s*\(", line)
            if m_spawn and m_spawn.group(1) not in self.spawn_targets:
                self.spawn_targets.add(m_spawn.group(1))
                self.spawn_order.append(m_spawn.group(1))

            i += 1

    # --- declaration emission (types first, so code can use them) -----
    def emit_types(self):
        for kind, name in self.type_order:
            if kind == "enum":
                variants = self.enums[name]
                self.emit("typedef enum {")
                self.indent += 1
                for v_name, _ in variants:
                    self.emit("%s_%s," % (name, v_name))
                self.indent -= 1
                self.emit("} %s_Tag;" % name)
                self.emit("")
                self.emit("typedef struct {")
                self.indent += 1
                self.emit("%s_Tag tag;" % name)
                self.emit("union {")
                self.indent += 1
                for v_name, v_types in variants:
                    if v_types:
                        fields = " ".join("%s _%d;" % (ctype(t), k)
                                          for k, t in enumerate(v_types))
                        self.emit("struct { %s } %s;" % (fields, v_name))
                self.indent -= 1
                self.emit("} as;")
                self.indent -= 1
                self.emit("} %s;" % name)
                self.emit("")
            else:
                self.emit("typedef struct %s %s;" % (name, name))
                self.emit("struct %s {" % name)
                self.indent += 1
                for fname, ftype in self.structs[name]:
                    self.emit("%s %s;" % (ctype(ftype), fname))
                self.indent -= 1
                self.emit("};")
                self.emit("")

    def emit_spawn_structs(self):
        for fname in self.spawn_order:
            params = self.fn_params.get(fname)
            if params is None:
                sys.stderr.write(
                    "[ubec]: error: spawn of unknown function '%s'\n" % fname)
                sys.exit(1)
            self.emit("typedef struct {")
            self.indent += 1
            for pname, ptype in params:
                self.check_flat(
                    ptype, "parameter '%s' of spawned function '%s'"
                    % (pname, fname))
                self.emit("%s p_%s;" % (ctype(ptype), pname))
            self.indent -= 1
            self.emit("} UbeSpawn_%s;" % fname)
            self.emit("")

    def emit_trampolines(self):
        for fname in self.spawn_order:
            params = self.fn_params[fname]
            self.emit("static void* ube_tramp_%s(void* p) {" % fname)
            self.indent += 1
            self.emit("UbeSpawn_%s* a = (UbeSpawn_%s*)p;" % (fname, fname))
            self.emit("ube_thread_init();")
            self.emit("%s(%s);" % (
                fname, ", ".join("a->p_%s" % p for p, _ in params)))
            self.emit("ube_thread_shutdown();")
            self.emit("free(a);")
            self.emit("return NULL;")
            self.indent -= 1
            self.emit("}")
            self.emit("")

    # --- main lowering -------------------------------------------------
    def compile(self):
        lines = [strip_line_comment(line).strip()
                 for line in self.source.splitlines()]
        lines = [line for line in lines if line]

        # Ube is direct-style only: reject colored concurrency outright.
        for line in lines:
            m_async = re.search(r"\b(async|await)\b", code_only(line))
            if m_async:
                sys.stderr.write(
                    "[ubec]: error: '%s' is not part of Ube; concurrency is "
                    "direct-style.\n        Write plain blocking code, use "
                    "`spawn` for background Vines, and `Chan[T]` for "
                    "coordination.\n" % m_async.group(1))
                sys.exit(1)

        self.prescan(lines)

        self.emit('#include "ube_runtime.h"')
        self.emit("")
        for fname in self.spawn_order:
            self.emit("static void* ube_tramp_%s(void* p);" % fname)
        if self.spawn_order:
            self.emit("")
        self.emit_types()
        self.emit_spawn_structs()

        i = 0
        while i < len(lines):
            line = lines[i]

            # 1. Type declarations were emitted by prescan/emit_types.
            if (line.startswith("enum ") or line.startswith("struct ")) \
                    and line.endswith("{"):
                i += 1
                while i < len(lines) and lines[i] != "}":
                    i += 1
                i += 1
                continue

            # 2. Entry point (before the generic function matcher).
            if line.startswith("fn main()"):
                self.emit("int main() {")
                self.indent += 1
                self.emit("ube_init();")
                self.emit("size_t __watermark = ube_tuber_save();")
                self.current_fn_return_type = "main"
                i += 1
                continue

            # 3. Receiver function: fn (r Rect) area() -> i32 {
            m_recv = re.match(
                r"fn\s*\(\s*(\w+)\s+(\w+)\s*\)\s*(\w+)\s*\((.*?)\)\s*->\s*(.+?)\s*\{$",
                line)
            if m_recv:
                recv_var, recv_type, fn_name, params, ret_type = m_recv.groups()
                self.current_fn_return_type = ret_type
                self.methods[(recv_type, fn_name)] = ret_type
                self.var_types[recv_var] = recv_type
                args = ["%s %s" % (recv_type, recv_var)] + self.param_list(
                    params, record=True)
                self.emit("%s %s_%s(%s) {" % (self.c_ret(ret_type), recv_type,
                                              fn_name, ", ".join(args)))
                self.indent += 1
                self.emit("size_t __watermark = ube_tuber_save();")
                i += 1
                continue

            # 4. Standard function, possibly void: fn worker(...) { }
            m_fn = re.match(
                r"fn\s+(\w+)\s*\((.*?)\)\s*(?:->\s*(.+?))?\s*\{$", line)
            if m_fn:
                fn_name, params = m_fn.group(1), m_fn.group(2)
                ret_type = m_fn.group(3).strip() if m_fn.group(3) else None
                self.current_fn_return_type = ret_type
                self.fn_rets[fn_name] = ret_type
                self.emit("%s %s(%s) {" % (self.c_ret(ret_type), fn_name,
                                           ", ".join(self.param_list(
                                               params, record=True))))
                self.indent += 1
                # A caller-allocated return keeps everything above the
                # caller's watermark: no restore inside this frame.
                if ret_type is not None and ctype(ret_type) != "UbeSlice_i32":
                    self.emit("size_t __watermark = ube_tuber_save();")
                i += 1
                continue

            # 5. Scoped Tuber reset close
            if line == "} // tuber":
                self.emit("ube_tuber_restore(__watermark_scope);")
                self.indent -= 1
                self.emit("}")
                i += 1
                continue

            # 6. Block close
            if line == "}":
                if self.indent == 1 and self.current_fn_return_type == "main":
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit("return 0;")
                self.indent -= 1
                self.emit("}")
                i += 1
                continue

            # 7. Scoped Tuber reset open
            if line == "tuber {":
                self.emit("{")
                self.indent += 1
                self.emit("size_t __watermark_scope = ube_tuber_save();")
                i += 1
                continue

            # 8. Return statements
            if line == "return":
                if self.current_fn_return_type == "main":
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit("return 0;")
                else:
                    # Void functions (None) return nothing; anything else is
                    # a source error caught by the C compiler.
                    self.emit("return;")
                i += 1
                continue
            if line.startswith("return "):
                ret_val = line[7:].strip()
                if self.current_fn_return_type == "main":
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit("return %s;" % ret_val)
                elif ctype(self.current_fn_return_type) == "UbeSlice_i32":
                    # Zero-GC caller allocation: keep the bump where it is.
                    self.emit("return %s;" % ret_val)
                elif result_parts(self.current_fn_return_type):
                    self.emit("ube_tuber_restore(__watermark);")
                    if ret_val.startswith("error(") and ret_val.endswith(")"):
                        self.emit("return ube_err_i32(ube_str_lit(%s));"
                                  % ret_val[6:-1])
                    else:
                        self.emit("return ube_ok_i32(%s);" % ret_val)
                else:
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit("return %s;" % ret_val)
                i += 1
                continue

            # 9. match <enum> { Variant(bind, ..) => stmt, ... }
            m_match = re.match(r"match\s+(\w+)\s*\{$", line)
            if m_match:
                mvar = m_match.group(1)
                etype = self.var_types.get(mvar)
                if etype not in self.enums:
                    sys.stderr.write(
                        "[ubec]: error: match on unknown enum variable '%s'\n"
                        % mvar)
                    sys.exit(1)
                variant_types = dict(self.enums[etype])
                self.emit("switch (%s.tag) {" % mvar)
                self.indent += 1
                i += 1
                while i < len(lines) and lines[i] != "}":
                    m_arm = re.match(r"(\w+)\s*(?:\((.*?)\))?\s*=>\s*(.+?)\s*,?$",
                                     lines[i])
                    if m_arm:
                        v_name, binding, body = m_arm.groups()
                        if v_name == "_":
                            self.emit("default: {")
                        else:
                            self.emit("case %s_%s: {" % (etype, v_name))
                        self.indent += 1
                        if binding:
                            for idx, bvar in enumerate(
                                    b.strip() for b in binding.split(",")):
                                btype = variant_types.get(v_name, ["i32"])[idx]
                                self.emit("%s %s = %s.as.%s._%d;"
                                          % (ctype(btype), bvar, mvar, v_name, idx))
                        body = body.strip()
                        if body.startswith("println(") and body.endswith(")"):
                            self.emit('printf("%%d\\n", (int)(%s));'
                                      % body[8:-1])
                        elif (body.startswith("println_str(")
                              and body.endswith(")")):
                            expr = body[12:-1].strip()
                            if self.is_string_literal(expr):
                                self.emit('printf("%%s\\n", %s);' % expr)
                            elif self.var_types.get(expr) == "string":
                                self.emit('printf("%%.*s\\n", (int)(%s).len, '
                                          '(%s).data);' % (expr, expr))
                            else:
                                self.emit('printf("%%s\\n", %s);' % expr)
                        elif body.startswith("return "):
                            rv = body[7:].strip()
                            if ctype(self.current_fn_return_type) == "UbeSlice_i32":
                                self.emit("return %s;" % rv)
                            elif result_parts(self.current_fn_return_type):
                                self.emit("ube_tuber_restore(__watermark);")
                                self.emit("return ube_ok_i32(%s);" % rv)
                            else:
                                self.emit("ube_tuber_restore(__watermark);")
                                self.emit("return %s;" % rv)
                        else:
                            self.emit(body + ";")
                        self.emit("break;")
                        self.indent -= 1
                        self.emit("}")
                    i += 1
                self.indent -= 1
                self.emit("}")
                i += 1
                continue

            # 10. Enum instantiation: let s = Shape.Circle(10)
            m_enum_inst = re.match(
                r"let\s+(?:mut\s+)?(\w+)\s*=\s*(\w+)\.(\w+)\s*(?:\((.*?)\))?\s*$",
                line)
            if m_enum_inst and m_enum_inst.group(2) in self.enums:
                var_name, enum_type, v_name, args = m_enum_inst.groups()
                self.emit("%s %s;" % (enum_type, var_name))
                self.emit("%s.tag = %s_%s;" % (var_name, enum_type, v_name))
                if args:
                    for idx, arg in enumerate(
                            a.strip() for a in split_top(args)):
                        self.emit("%s.as.%s._%d = %s;"
                                  % (var_name, v_name, idx, arg))
                self.var_types[var_name] = enum_type
                i += 1
                continue

            # 11. Channel creation: let ch = Chan[Job].new(8)
            m_chan_new = re.match(
                r"let\s+(?:mut\s+)?(\w+)\s*=\s*Chan\[(.+?)\]\.new\((.*?)\)\s*$",
                line)
            if m_chan_new:
                var_name, elem, cap = m_chan_new.groups()
                elem = elem.strip()
                self.check_flat(elem, "Chan[%s]" % elem)
                self.emit("UbeChan %s = ube_chan_new(sizeof(%s), %s);"
                          % (var_name, ctype(elem), cap.strip() or "1"))
                self.var_types[var_name] = "Chan[%s]" % elem
                self.chan_elem[var_name] = elem
                i += 1
                continue

            # 12. Channel receive: let job = jobs.recv()
            m_recv = re.match(
                r"let\s+(?:mut\s+)?(\w+)\s*=\s*(\w+)\.recv\(\)\s*\??\s*$", line)
            if m_recv:
                var_name, ch = m_recv.groups()
                elem = self.chan_elem.get(ch)
                if elem is None:
                    sys.stderr.write(
                        "[ubec]: error: '%s' is not a known channel\n" % ch)
                    sys.exit(1)
                self.emit("%s %s;" % (ctype(elem), var_name))
                self.emit("ube_chan_recv(%s, &%s);" % (ch, var_name))
                self.var_types[var_name] = elem
                i += 1
                continue

            # 13. Channel send: ch.send(value)
            m_send = re.match(r"(\w+)\.send\((.*)\)\s*$", line)
            if m_send:
                ch, expr = m_send.group(1), m_send.group(2).strip()
                elem = self.chan_elem.get(ch)
                if elem is None:
                    sys.stderr.write(
                        "[ubec]: error: '%s' is not a known channel\n" % ch)
                    sys.exit(1)
                self.check_flat(elem, "send on '%s'" % ch)
                sent_type = self.expr_type(expr)
                if sent_type is not None:
                    self.check_flat(sent_type, "sent on '%s'" % ch)
                m_enum_lit = re.match(
                    r"(\w+)\.(\w+)\s*(?:\((.*?)\))?\s*$", expr)
                init = self.struct_init_literal(expr)
                if m_enum_lit and m_enum_lit.group(1) in self.enums:
                    enum_type, v_name = m_enum_lit.group(1), m_enum_lit.group(2)
                    args = m_enum_lit.group(3)
                    tmp = "__chan_val"
                    self.emit("%s %s;" % (enum_type, tmp))
                    self.emit("%s.tag = %s_%s;" % (tmp, enum_type, v_name))
                    if args:
                        for idx, arg in enumerate(split_top(args)):
                            self.emit("%s.as.%s._%d = %s;"
                                      % (tmp, v_name, idx, arg))
                    self.emit("ube_chan_send(%s, &%s);" % (ch, tmp))
                elif init is not None:
                    self.emit("ube_chan_send(%s, &%s);" % (ch, init))
                elif expr.isidentifier() and sent_type == elem:
                    self.emit("ube_chan_send(%s, &%s);" % (ch, expr))
                else:
                    self.emit("ube_chan_send(%s, &(%s){ %s });"
                              % (ch, ctype(elem), expr))
                i += 1
                continue

            # 14. Channel close: ch.close()
            m_chan_close = re.match(r"(\w+)\.close\(\)\s*$", line)
            if m_chan_close and m_chan_close.group(1) in self.chan_elem:
                self.emit("ube_chan_close(%s);" % m_chan_close.group(1))
                i += 1
                continue

            # 15. Spawn a Vine: spawn worker(jobs, results)
            m_spawn = re.match(r"spawn\s+(\w+)\s*\((.*)\)\s*$", line)
            if m_spawn:
                fname, args_text = m_spawn.group(1), m_spawn.group(2)
                params = self.fn_params.get(fname)
                if params is None:
                    sys.stderr.write(
                        "[ubec]: error: spawn of unknown function '%s'\n"
                        % fname)
                    sys.exit(1)
                args = split_top(args_text)
                if len(args) != len(params):
                    sys.stderr.write(
                        "[ubec]: error: spawn %s expects %d argument(s), "
                        "got %d\n" % (fname, len(params), len(args)))
                    sys.exit(1)
                for (pname, ptype), arg in zip(params, args):
                    self.check_flat(
                        ptype, "parameter '%s' of spawned function '%s'"
                        % (pname, fname))
                    arg_type = self.expr_type(arg)
                    if arg_type is not None:
                        self.check_flat(arg_type,
                                        "spawn argument '%s'" % arg)
                self.emit("{")
                self.indent += 1
                self.emit("UbeSpawn_%s* __a = (UbeSpawn_%s*)calloc(1, "
                          "sizeof(UbeSpawn_%s));" % (fname, fname, fname))
                for (pname, _), arg in zip(params, args):
                    m_enum_lit = re.match(
                        r"(\w+)\.(\w+)\s*(?:\((.*?)\))?\s*$", arg)
                    if m_enum_lit and m_enum_lit.group(1) in self.enums:
                        enum_type, v_name = (m_enum_lit.group(1),
                                             m_enum_lit.group(2))
                        tmp = "__e_%s" % pname
                        self.emit("%s %s;" % (enum_type, tmp))
                        self.emit("%s.tag = %s_%s;"
                                  % (tmp, enum_type, v_name))
                        if m_enum_lit.group(3):
                            for idx, a in enumerate(
                                    split_top(m_enum_lit.group(3))):
                                self.emit("%s.as.%s._%d = %s;"
                                          % (tmp, v_name, idx, a))
                        arg_expr = tmp
                    else:
                        arg_expr = self.struct_init_literal(arg) or arg
                    self.emit("__a->p_%s = %s;" % (pname, arg_expr))
                self.emit("ube_spawn(ube_tramp_%s, __a);" % fname)
                self.indent -= 1
                self.emit("}")
                i += 1
                continue

            # 16. Optional declaration: let x: i32? = 777 / null
            m_opt = re.match(
                r"let\s+(?:mut\s+)?(\w+)\s*:\s*(\w+\?)\s*=\s*(.+?)\s*$", line)
            if m_opt:
                var_name, opt_type, value = m_opt.groups()
                base = opt_type[:-1]
                if base not in OPTION_BASES:
                    sys.stderr.write(
                        "[ubec]: error: optional type '%s' has no runtime "
                        "helper (supported: %s)\n"
                        % (opt_type,
                           ", ".join(sorted(OPTION_BASES)) + "?"))
                    sys.exit(1)
                if value == "null":
                    self.emit("%s %s = ube_none_%s();"
                              % (ctype(opt_type), var_name, base))
                elif self.var_types.get(value) == opt_type:
                    self.emit("%s %s = %s;"
                              % (ctype(opt_type), var_name, value))
                else:
                    if base == "string" and self.is_string_expr(value):
                        value = self.gen_string_expr(value)
                    self.emit("%s %s = ube_some_%s(%s);"
                              % (ctype(opt_type), var_name, base, value))
                self.var_types[var_name] = opt_type
                i += 1
                continue

            # 17. Fallback operator: let x = expr or_else default
            m_orelse = re.match(
                r"let\s+(?:mut\s+)?(\w+)\s*=\s*(.+?)\s+or_else\s+(.+?)\s*$",
                line)
            if m_orelse:
                var_name, expr, default_val = m_orelse.groups()
                ret_text = self.call_ret(expr)
                expr_type = self.expr_type(expr)
                if ret_text and result_parts(ret_text):
                    val_text = result_parts(ret_text)[0]
                    tmp = "__res_%s" % var_name
                    self.emit("UbeResult_i32 %s = %s;" % (tmp, expr))
                    self.emit("%s %s = %s.ok ? %s.val : (%s);"
                              % (ctype(val_text), var_name, tmp, tmp,
                                 default_val))
                    self.var_types[var_name] = val_text
                elif expr_type and expr_type.endswith("?"):
                    base = expr_type[:-1]
                    tmp = "__opt_%s" % var_name
                    default_expr = default_val
                    if base == "string" and self.is_string_expr(default_val):
                        default_expr = self.gen_string_expr(default_val)
                    self.emit("%s %s = %s;" % (ctype(expr_type), tmp, expr))
                    self.emit("%s %s = %s.has_val ? %s.val : (%s);"
                              % (ctype(base), var_name, tmp, tmp,
                                 default_expr))
                    self.var_types[var_name] = base
                else:
                    sys.stderr.write(
                        "[ubec]: error: or_else requires a Result or optional "
                        "value; '%s' has neither type\n" % expr)
                    sys.exit(1)
                i += 1
                continue

            # 18. The `?` operator: let val = call(...)?
            m_try = re.match(r"let\s+(\w+)\s*=\s*(.+?)\?\s*$", line)
            if m_try:
                var_name, call_expr = m_try.group(1), m_try.group(2).strip()
                ret_text = self.call_ret(call_expr)
                if not ret_text or not result_parts(ret_text):
                    self.emit("int32_t %s = %s;  // '?' ignored: not a Result"
                              % (var_name, call_expr))
                    self.var_types[var_name] = "i32"
                    i += 1
                    continue

                val_t = ctype(result_parts(ret_text)[0])
                tmp = "__res_%s" % var_name
                self.emit("UbeResult_i32 %s = %s;" % (tmp, call_expr))
                self.emit("if (!%s.ok) {" % tmp)
                self.indent += 1
                if result_parts(self.current_fn_return_type):
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit("return %s; // bubble up via `?`" % tmp)
                elif self.current_fn_return_type == "main":
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit('fprintf(stderr, "[Ube Error]: %%.*s\\n", '
                              '(int)%s.err.len, %s.err.data);' % (tmp, tmp))
                    self.emit("return 1;")
                else:
                    self.emit("ube_tuber_restore(__watermark);")
                    self.emit('fprintf(stderr, "[Ube Error]: %%.*s\\n", '
                              '(int)%s.err.len, %s.err.data);' % (tmp, tmp))
                    self.emit("exit(1);")
                self.indent -= 1
                self.emit("}")
                self.emit("%s %s = %s.val;" % (val_t, var_name, tmp))
                self.var_types[var_name] = val_t
                i += 1
                continue

            # 19. Receiver method call: let a = r.area()
            m_call = re.match(
                r"let\s+(?:mut\s+)?(\w+)\s*=\s*(\w+)\.(\w+)\s*\((.*?)\)\s*$",
                line)
            if m_call:
                var_name, obj, meth, args = m_call.groups()
                stype = self.var_types.get(obj)
                ret_text = self.methods.get((stype, meth)) if stype else None
                if stype is None or ret_text is None:
                    sys.stderr.write(
                        "[ubec]: warning: unknown receiver '%s.%s'; assuming "
                        "Rect -> i32\n" % (obj, meth))
                    stype = stype or "Rect"
                    ret_text = "i32"
                arg_str = obj if not args.strip() else "%s, %s" % (obj, args)
                self.emit("%s %s = %s_%s(%s);" % (self.c_ret(ret_text), var_name,
                                                  stype, meth, arg_str))
                self.var_types[var_name] = self.val_type(ret_text)
                i += 1
                continue

            # 20. for x in slice {
            m_for = re.match(r"for\s+(\w+)\s+in\s+(\w+)\s*\{$", line)
            if m_for:
                var, slice_var = m_for.groups()
                self.emit("for (size_t _i = 0; _i < %s.len; _i++) {"
                          % slice_var)
                self.indent += 1
                self.emit("int32_t %s = %s.data[_i];" % (var, slice_var))
                i += 1
                continue

            # 21. Safe optional unwrap: if let val = opt {
            m_iflet = re.match(r"if\s+let\s+(\w+)\s*=\s*(\w+)\s*\{$", line)
            if m_iflet:
                val_name, opt_var = m_iflet.groups()
                opt_type = self.var_types.get(opt_var)
                if not opt_type or not opt_type.endswith("?"):
                    sys.stderr.write(
                        "[ubec]: error: if let requires an optional variable; "
                        "'%s' has type '%s'\n"
                        % (opt_var, opt_type or "unknown"))
                    sys.exit(1)
                base = opt_type[:-1]
                self.emit("if (%s.has_val) {" % opt_var)
                self.indent += 1
                self.emit("%s %s = %s.val;"
                          % (ctype(base), val_name, opt_var))
                self.var_types[val_name] = base
                i += 1
                continue

            # 22. if <cond> {
            m_if = re.match(r"if\s+(.+?)\s*\{$", line)
            if m_if:
                self.emit("if (%s) {" % m_if.group(1).strip())
                self.indent += 1
                i += 1
                continue

            # 23. Declarations and statements
            if line.startswith("let mut ") and "[]i32{}" in line:
                var_name = line.split()[2]
                self.emit("UbeSlice_i32 %s = ube_slice_i32_new();" % var_name)
                self.var_types[var_name] = "[]i32"
            elif line.startswith("let ") and "{" in line and "}" in line:
                m_st = re.match(
                    r"let\s+(?:mut\s+)?(\w+)\s*=\s*(\w+)\s*\{(.*)\}\s*$", line)
                if m_st:
                    var, sname, vals = m_st.groups()
                    inits = []
                    for part in split_top(vals):
                        fname, sep, fval = part.partition(":")
                        if not sep:
                            continue
                        inits.append(".%s = %s" % (fname.strip(),
                                                   fval.strip()))
                    self.emit("%s %s = { %s };"
                              % (sname, var, ", ".join(inits)))
                    self.var_types[var] = sname
            elif line.startswith("let ") and "=" in line:
                decl = line[4:].strip()
                if decl.startswith("mut "):
                    decl = decl[4:].strip()
                var_name, _, expr = decl.partition("=")
                var_name, expr = var_name.strip(), expr.strip()
                call_t = self.call_ret(expr)
                if self.is_string_expr(expr):
                    self.emit("string %s = %s;"
                              % (var_name, self.gen_string_expr(expr)))
                    self.var_types[var_name] = "string"
                elif call_t and ctype(call_t) == "UbeSlice_i32":
                    self.emit("UbeSlice_i32 %s = %s;" % (var_name, expr))
                    self.var_types[var_name] = "[]i32"
                else:
                    self.emit("int32_t %s = %s;" % (var_name, expr))
                    self.var_types[var_name] = "i32"
            elif ".push(" in line:
                m = re.match(r"(\w+)\.push\((.*)\)", line)
                if m:
                    self.emit("ube_slice_i32_push(&%s, %s);"
                              % (m.group(1), m.group(2)))
            elif line.startswith("println(") and line.endswith(")"):
                self.emit('printf("%%d\\n", (int)(%s));' % line[8:-1])
            elif line.startswith("println_str(") and line.endswith(")"):
                expr = line[12:-1].strip()
                if self.is_string_literal(expr):
                    self.emit('printf("%%s\\n", %s);' % expr)
                elif self.var_types.get(expr) == "string":
                    self.emit('printf("%%.*s\\n", (int)(%s).len, (%s).data);'
                              % (expr, expr))
                else:
                    self.emit('printf("%%s\\n", %s);' % expr)
            elif re.match(r"^\w+\s*\(.*\)\s*;?$", line):
                self.emit(line if line.endswith(";") else line + ";")

            i += 1

        self.emit("")
        self.emit_trampolines()
        return "\n".join(self.output)


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 ubec.py <file.ube>")
        sys.exit(1)

    with open(sys.argv[1], "r") as f:
        src = f.read()

    compiler = UbeTranspiler(src)
    c_code = compiler.compile()

    with open("output.c", "w") as f:
        f.write(c_code)
    print("Compiled successfully -> output.c")
