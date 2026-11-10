; Highlight queries for Mio. Keywords are exactly those of internal/token (version 0.5).

[
  "kaku"
  "waza"
  "teisu"
  "kata"
  "sentaku"
  "koukai"
] @keyword

[
  "moshi"
  "sore"
  "aida"
  "awase"
  "baai"
  "kaesu"
  "nukeru"
  "tsuzuku"
] @keyword.control

"yobu" @keyword.import

(boolean_literal) @constant.builtin
(nil_literal) @constant.builtin

(integer_literal) @number
(float_literal) @number
(string_literal) @string
(rune_literal) @string.special

(line_comment) @comment
(block_comment) @comment

(builtin_type) @type.builtin
(type_identifier) @type

[
  "+" "-" "*" "/" "==" "!=" "<" "<=" ">" ">=" "&&" "||" "!" "=" "->"
] @operator

[ "(" ")" "[" "]" "{" "}" ] @punctuation.bracket
[ "," ";" ":" "." ] @punctuation.delimiter

(function_declaration name: (identifier) @function)
(call_expression function: (identifier) @function.call)
(call_expression
  function: (identifier) @function.builtin
  (#any-of? @function.builtin "chant" "len"))
(call_expression function: (field_expression field: (identifier) @function.call))

(parameter name: (identifier) @variable.parameter)
(type_parameter name: (identifier) @type)
(field_declaration name: (identifier) @property)
(field_initializer name: (identifier) @property)
(field_expression field: (identifier) @property)
(enum_variant name: (identifier) @constructor)
(match_arm variant: (identifier) @constructor)
(const_declaration name: (identifier) @constant)
(import_statement path: (string_literal) @string.special.path)
