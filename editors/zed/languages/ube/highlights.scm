; Comments
(comment) @comment

; Visibility and Modifiers
(visibility) @keyword
(mutability) @keyword

; Keywords
[
  "import"
  "struct"
  "enum"
  "trait"
  "extern"
  "arena"
  "unsafe"
  "defer"
  "spawn"
] @keyword

[
  "fn"
] @keyword.function

[
  "let"
] @keyword

[
  "return"
] @keyword.return

[
  "if"
  "else"
  "match"
  "select"
  "case"
  "default"
  "break"
  "continue"
] @keyword.control

[
  "for"
  "while"
  "in"
] @keyword.repeat

"or_else" @keyword.operator

; Literals
(boolean_literal) @boolean
(null_literal) @constant.builtin
(number_literal) @number
(string_literal) @string
(format_string_literal) @string
(escape_sequence) @string.escape

; Built-in and Custom Types
(primitive_type) @type.builtin
(type_identifier) @type

; Function and Method Declarations
(function_declaration
  name: (identifier) @function)

(trait_method_declaration
  name: (identifier) @function)

(extern_function_declaration
  name: (identifier) @function)

; Function & Method Calls
(call_expression
  function: (identifier) @function)

(call_expression
  function: (member_expression
    member: (identifier) @function.method @function))

; Parameters
(parameter
  name: (identifier) @variable.parameter)

(receiver
  name: (identifier) @variable.parameter)

; Field Access and Initializers
(field_declaration
  name: (identifier) @property)

(field_initializer
  name: (identifier) @property)

(member_expression
  member: (identifier) @property)

; Enum Variants
(variant_declaration
  name: (identifier) @variant)

(match_pattern
  name: (identifier) @variant)

; Operators
[
  "->"
  "=>"
  "?"
  "!"
  "="
  "+="
  "-="
  "*="
  "/="
  "%="
  "&="
  "|="
  "^="
  "<<="
  ">>="
  "=="
  "!="
  "<"
  "<="
  ">"
  ">="
  "<<"
  ">>"
  "&&"
  "||"
  "+"
  "-"
  "*"
  "/"
  "%"
  "&"
  "|"
  "^"
  "~"
  "<-"
  ".."
] @operator

; Delimiters & Brackets
[
  "("
  ")"
  "["
  "]"
  "{"
  "}"
] @punctuation.bracket

[
  ","
  ":"
  "."
] @punctuation.delimiter
