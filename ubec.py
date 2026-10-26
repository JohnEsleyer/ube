import sys
import re

# v0.2: structs + receiver methods, Tuber strings, Result types and the `?` operator.

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
}


def ctype(t):
    t = t.strip()
    return TYPE_MAP.get(t, t)


def result_parts(ret_type):
    """('value type', 'error type') for 'T ! E', else None."""
    if ret_type and "!" in ret_type:
        val, _, err = ret_type.partition("!")
        return val.strip(), err.strip()
    return None


class UbeTranspiler:
    def __init__(self, source):
        self.source = source
        self.output = []
        self.indent = 0
        # v0.2 semantic tables
        self.structs = {}       # struct name -> [(field, ctype)]
        self.var_types = {}     # variable -> struct name or scalar type
        self.fn_rets = {}       # function -> return type text
        self.methods = {}       # (struct, method) -> return type text
        self.current_fn_return_type = None

    def emit(self, line):
        self.output.append("    " * self.indent + line)

    # --- type helpers -------------------------------------------------
    def c_ret(self, ret_type):
        parts = result_parts(ret_type)
        if parts:
            if ctype(parts[0]) != "int32_t":
                sys.stderr.write(
                    "[ubec]: warning: result value type '%s' has no runtime "
                    "helper yet; using UbeResult_i32\n" % parts[0])
            return "UbeResult_i32"
        return ctype(ret_type)

    def val_type(self, ret_type):
        parts = result_parts(ret_type)
        return ctype(parts[0]) if parts else ctype(ret_type)

    def param_list(self, params):
        out = []
        if params.strip():
            for p in params.split(","):
                pname, ptype = p.split(":")
                out.append("%s %s" % (ctype(ptype), pname.strip()))
        return out

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

    def compile(self):
        self.emit('#include "ube_runtime.h"')
        self.emit("")

        lines = [line.strip() for line in self.source.splitlines()
                 if line.strip() and not line.strip().startswith("//")]

        i = 0
        while i < len(lines):
            line = lines[i]

            # 1. Struct declaration
            if line.startswith("struct ") and line.endswith("{"):
                sname = line.split()[1]
                self.emit("typedef struct %s %s;" % (sname, sname))
                self.emit("struct %s {" % sname)
                self.indent += 1
                i += 1
                fields = []
                while i < len(lines) and lines[i] != "}":
                    parts = lines[i].rstrip(",").split(":")
                    fname, ftype = parts[0].strip(), ctype(parts[1])
                    self.emit("%s %s;" % (ftype, fname))
                    fields.append((fname, ftype))
                    i += 1
                self.indent -= 1
                self.emit("};")
                self.emit("")
                self.structs[sname] = fields
                i += 1
                continue

            # 2. Receiver function: fn (r Rect) area() -> i32 {
            m_recv = re.match(
                r"fn\s*\(\s*(\w+)\s+(\w+)\s*\)\s*(\w+)\s*\((.*?)\)\s*->\s*(.+?)\s*\{$",
                line)
            if m_recv:
                recv_var, recv_type, fn_name, params, ret_type = m_recv.groups()
                self.current_fn_return_type = ret_type
                self.methods[(recv_type, fn_name)] = ret_type
                args = ["%s %s" % (recv_type, recv_var)] + self.param_list(params)
                self.emit("%s %s_%s(%s) {" % (self.c_ret(ret_type), recv_type,
                                              fn_name, ", ".join(args)))
                self.indent += 1
                self.emit("size_t __watermark = ube_tuber_save();")
                i += 1
                continue

            # 3. Standard function: fn divide(a: i32, b: i32) -> i32 ! string {
            m_fn = re.match(r"fn\s+(\w+)\s*\((.*?)\)\s*->\s*(.+?)\s*\{$", line)
            if m_fn:
                fn_name, params, ret_type = m_fn.groups()
                self.current_fn_return_type = ret_type
                self.fn_rets[fn_name] = ret_type
                self.emit("%s %s(%s) {" % (self.c_ret(ret_type), fn_name,
                                           ", ".join(self.param_list(params))))
                self.indent += 1
                self.emit("size_t __watermark = ube_tuber_save();")
                i += 1
                continue

            # 4. Entry point
            if line.startswith("fn main()"):
                self.emit("int main() {")
                self.indent += 1
                self.emit("ube_init();")
                self.emit("size_t __watermark = ube_tuber_save();")
                self.current_fn_return_type = "main"
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

            # 8. Return statements (plain or Result)
            if line.startswith("return "):
                ret_val = line[7:].strip()
                if self.current_fn_return_type == "main":
                    self.emit("ube_tuber_restore(__watermark);")
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

            # 9. The `?` operator: let val = call(...)?
            m_try = re.match(r"let\s+(\w+)\s*=\s*(.+?)\?\s*$", line)
            if m_try:
                var_name, call_expr = m_try.group(1), m_try.group(2).strip()
                ret_text = self.call_ret(call_expr)
                if not ret_text or not result_parts(ret_text):
                    # Not a Result call: `?` is a no-op for now.
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

            # 10. Receiver method call: let a = r.area()
            m_call = re.match(r"let\s+(\w+)\s*=\s*(\w+)\.(\w+)\s*\((.*?)\)\s*$",
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

            # 11. for x in slice {
            m_for = re.match(r"for\s+(\w+)\s+in\s+(\w+)\s*\{$", line)
            if m_for:
                var, slice_var = m_for.groups()
                self.emit("for (size_t _i = 0; _i < %s.len; _i++) {"
                          % slice_var)
                self.indent += 1
                self.emit("int32_t %s = %s.data[_i];" % (var, slice_var))
                i += 1
                continue

            # 12. if <cond> {
            m_if = re.match(r"if\s+(.+?)\s*\{$", line)
            if m_if:
                self.emit("if (%s) {" % m_if.group(1).strip())
                self.indent += 1
                i += 1
                continue

            # 13. Declarations and statements
            if line.startswith("let mut ") and "[]i32{}" in line:
                var_name = line.split()[2]
                self.emit("UbeSlice_i32 %s = ube_slice_i32_new();" % var_name)
                self.var_types[var_name] = "[]i32"
            elif line.startswith("let ") and "{" in line and "}" in line:
                # Struct literal: let r = Rect{ width: 5, height: 8 }
                m_st = re.match(r"let\s+(\w+)\s*=\s*(\w+)\s*\{(.*)\}\s*$", line)
                if m_st:
                    var, sname, vals = m_st.groups()
                    inits = []
                    for part in vals.split(","):
                        if not part.strip():
                            continue
                        fname, _, fval = part.partition(":")
                        inits.append(".%s = %s" % (fname.strip(),
                                                   fval.strip()))
                    self.emit("%s %s = { %s };"
                              % (sname, var, ", ".join(inits)))
                    self.var_types[var] = sname
            elif line.startswith("let ") and "=" in line:
                var_name, _, expr = line[4:].partition("=")
                var_name, expr = var_name.strip(), expr.strip()
                if self.is_string_expr(expr):
                    self.emit("string %s = %s;" % (var_name,
                                                   self.gen_string_expr(expr)))
                    self.var_types[var_name] = "string"
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
                    # `string` is a (ptr, len) pair on the Tuber.
                    self.emit('printf("%%.*s\\n", (int)(%s).len, (%s).data);'
                              % (expr, expr))
                else:
                    self.emit('printf("%%s\\n", %s);' % expr)

            i += 1

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
