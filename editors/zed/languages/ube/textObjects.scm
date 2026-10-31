(function_declaration
  body: (block
    "{"
    (_)* @function.inside
    "}")) @function.around

(struct_declaration
  body: (struct_body
    "{"
    (_)* @class.inside
    "}")) @class.around

(enum_declaration
  body: (enum_body
    "{"
    (_)* @class.inside
    "}")) @class.around

(trait_declaration
  body: (trait_body
    "{"
    (_)* @class.inside
    "}")) @class.around

(comment)+ @comment.around
(comment) @comment.inside
