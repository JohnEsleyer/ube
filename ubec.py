import sys
import re

class UbeCompiler:
    def __init__(self, source):
        self.source = source
        self.output = []
        self.indent = 0

    def emit(self, line):
        self.output.append("    " * self.indent + line)

    def compile(self):
        self.emit('#include "ube_runtime.h"')
        self.emit("")

        lines = [line.strip() for line in self.source.splitlines() if line.strip() and not line.strip().startswith("//")]

        for line in lines:
            if line.startswith("fn main()"):
                self.emit("int main() {")
                self.indent += 1
                self.emit("ube_init();")
                self.emit("size_t __watermark_main = ube_tuber_save();")
            elif line == "}":
                if self.indent == 1: # exiting main
                    self.emit("ube_tuber_restore(__watermark_main);")
                    self.emit("return 0;")
                self.indent -= 1
                self.emit("}")
            elif line.startswith("let mut ") and "[]i32{}" in line:
                # Example: let mut numbers = []i32{}
                var_name = line.split()[2]
                self.emit(f"UbeSlice_i32 {var_name} = ube_slice_i32_new();")
            elif line.startswith("let ") and "=" in line:
                # Example: let x = 10
                parts = line[4:].split("=")
                var_name = parts[0].strip()
                val = parts[1].strip()
                self.emit(f"int32_t {var_name} = {val};")
            elif ".push(" in line:
                # Example: numbers.push(42)
                m = re.match(r"(\w+)\.push\((.*)\)", line)
                if m:
                    var, val = m.group(1), m.group(2)
                    self.emit(f"ube_slice_i32_push(&{var}, {val});")
            elif line.startswith("println(") and line.endswith(")"):
                # Example: println(x)
                expr = line[8:-1]
                self.emit(f'printf("%d\\n", (int)({expr}));')
            elif line.startswith("println_str(") and line.endswith(")"):
                # Example: println_str("hello")
                expr = line[12:-1]
                self.emit(f'printf("%s\\n", {expr});')
            elif line == "tuber {":
                # Scoped reset block!
                self.emit("{")
                self.indent += 1
                self.emit("size_t __watermark_scope = ube_tuber_save();")
            elif line == "} // tuber":
                self.emit("ube_tuber_restore(__watermark_scope);")
                self.indent -= 1
                self.emit("}")
            elif line.startswith("for ") and " in " in line:
                # Example: for item in numbers {
                m = re.match(r"for (\w+) in (\w+)", line)
                var, slice_var = m.group(1), m.group(2)
                self.emit(f"for (size_t _i = 0; _i < {slice_var}.len; _i++) {{")
                self.indent += 1
                self.emit(f"int32_t {var} = {slice_var}.data[_i];")

        return "\n".join(self.output)

if __name__ == "__main__":
    if len(sys.argv) < 2:
        print("Usage: python3 ubec.py <file.ube>")
        sys.exit(1)

    with open(sys.argv[1], "r") as f:
        src = f.read()

    compiler = UbeCompiler(src)
    c_code = compiler.compile()

    with open("output.c", "w") as f:
        f.write(c_code)
    print("Compiled successfully -> output.c")
