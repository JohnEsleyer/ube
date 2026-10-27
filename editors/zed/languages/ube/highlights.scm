; Keywords
[
  "let" "mut" "fn" "struct" "enum" "extern" "import"
] @keyword

[
  "if" "else" "while" "for" "in" "return" "break" "continue"
  "match" "select" "spawn" "scope" "defer" "or_else"
] @keyword.control

; Types
[
  "i32" "u32" "i64" "u64" "u8" "f32" "f64" "bool" "string" "void"
  "Chan" "Pool" "UbeHandle"
] @type.builtin
(type_identifier) @type

; Functions
(call_expression function: (identifier) @function.call)
(function_declaration name: (identifier) @function)

; Literals
(number) @number
(string_literal) @string
["true" "false" "null"] @constant.builtin
(comment) @comment

; Operators
["=" "==" "!=" "<" "<=" ">" ">=" "&&" "||" "+" "-" "*" "/" "%" "->" "=>" "?" "!" "&"] @operator
