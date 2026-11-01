(fn_declaration
  name: (identifier) @run
  (#eq? @run "main")) @runnable

(fn_declaration
  name: (identifier) @run
  (#match? @run "^test_")) @runnable

(function_declaration
  name: (identifier) @run
  (#eq? @run "main")) @runnable

(function_declaration
  name: (identifier) @run
  (#match? @run "^test_")) @runnable
