(fn_declaration) @function.around
(fn_declaration
  body: (block) @function.inside)

(function_declaration) @function.around
(function_declaration
  body: (block) @function.inside)

(struct_declaration) @class.around
(struct_declaration
  body: (_)? @class.inside)

(enum_declaration) @class.around
(enum_declaration
  body: (_)? @class.inside)

(trait_declaration) @class.around
(trait_declaration
  body: (_)? @class.inside)

(comment) @comment.around
