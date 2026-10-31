(
  (function_declaration
    name: (identifier) @_name
    (#eq? @_name "main")
  ) @run
  (#set! tag ube-main)
)

(
  (function_declaration
    name: (identifier) @_name
    (#match? @_name "^test_")
  ) @run
  (#set! tag ube-test)
)
