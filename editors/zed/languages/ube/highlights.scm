; ==============================================================================
; Tree-sitter Highlighting Queries for Ube
; ==============================================================================

; Keywords
"fn" @keyword.function
"return" @keyword.return

[
  "let"
  "mut"
  "pub"
] @keyword.modifier

[
  "if"
  "else"
  "while"
  "for"
  "in"
  "match"
  "select"
  "spawn"
  "arena"
  "promote"
  "defer"
  "or_else"
  "break"
  "continue"
] @keyword.control

[
  "struct"
  "enum"
  "trait"
  "extern"
  "import"
] @keyword

; Constants & Literals
[
  "true"
  "false"
] @boolean

"null" @constant.builtin

[
  (integer_literal)
  (int_literal)
  (number)
] @number

[
  (float_literal)
] @number.float

[
  (string_literal)
  (string)
] @string

[
  (format_string)
  (interpolated_string)
] @string.special

[
  (escape_sequence)
] @string.escape

; Comments
[
  (line_comment)
  (block_comment)
  (comment)
] @comment

; Operators
[
  "->"
  "=>"
  "?"
  "!"
  ".."
  "+"
  "-"
  "*"
  "/"
  "%"
  "=="
  "!="
  "<"
  "<="
  ">"
  ">="
  "="
  "+="
  "-="
  "*="
  "/="
  "%="
  "&&"
  "||"
  "&"
  "|"
  "^"
  "~"
  "<<"
  ">>"
  "&="
  "|="
  "^="
  "<<="
  ">>="
] @operator

; Delimiters
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
  "."
  ":"
  ";"
] @punctuation.delimiter

; Built-in types
((identifier) @type.builtin
  (#match? @type.builtin "^(i8|i16|i32|i64|u8|u16|u32|u64|f32|f64|bool|string|void|Chan|Pool|UbeHandle|StringBuilder)$"))

; Capitalized identifiers as user types
((identifier) @type
  (#match? @type "^[A-Z][a-zA-Z0-9_]*$"))

(primitive_type) @type.builtin
(custom_type) @type
(type_identifier) @type

; Built-in functions
((identifier) @function.builtin
  (#match? @function.builtin "^(print|println|println_str|format|time_now_ms|sleep_ms|yield|exit_process|int_to_str|str_to_int|read_file|write_file|file_exists|file_remove|path_join|dir_list|dir_create|env_get|env_set|cmd_run|str_contains|str_starts_with|str_ends_with|str_trim|str_index_of|str_split|str_replace|os_args|abs|min|max|sqrt|pow|floor|ceil)$"))

; Function and Method Declarations
(fn_declaration
  name: (identifier) @function)

(function_declaration
  name: (identifier) @function)

(extern_fn_declaration
  name: (identifier) @function)

; Method Calls and Direct Calls
(call_expression
  function: (identifier) @function.call)

(call_expression
  function: (member_expression
    member: (identifier) @function.method))

; Parameters & Members
(parameter
  name: (identifier) @variable.parameter)

(field_declaration
  name: (identifier) @variable.member)

(member_expression
  member: (identifier) @variable.member)

(field_initializer
  name: (identifier) @variable.member)
