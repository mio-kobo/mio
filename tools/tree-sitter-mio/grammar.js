/**
 * Tree-sitter grammar for Mio (language version 0.5 keywords, as shipped in Mio 2.1.x).
 *
 * Source of truth: internal/lexer, internal/token and internal/parser in this
 * repository. The grammar describes only syntax the official parser accepts.
 *
 * One construct needs a lexical trick. The official parser decides between an
 * index expression `f[x]` and explicit type arguments `f[T](...)` by looking at the
 * first token after `[`: a builtin type name, an identifier that starts with an
 * upper-case letter, `[` or `(` means type arguments (looksLikeTypeName). The same
 * rule is reproduced here with token priority: builtin type names are string
 * tokens and `type_identifier` is an upper-case-initial token declared before
 * `identifier`, so they win exactly where the heuristic says they do.
 */

const PREC = {
  or: 1,
  and: 2,
  equals: 3,
  compare: 4,
  sum: 5,
  product: 6,
  prefix: 7,
  call: 8,
  index: 9,
  field: 10,
};

const BUILTIN_TYPES = [
  'int8', 'int16', 'int32', 'int64', 'rune', 'uint8', 'uint16', 'uint32',
  'float32', 'float64', 'bool', 'string', 'unit', 'resource', 'Map',
];

function commaSep(rule) {
  return optional(commaSep1(rule));
}

function commaSep1(rule) {
  return seq(rule, repeat(seq(',', rule)));
}

module.exports = grammar({
  name: 'mio',

  extras: ($) => [/\s/, $.line_comment, $.block_comment],

  word: ($) => $.identifier,

  supertypes: ($) => [$._statement, $._expression, $._type],

  conflicts: ($) => [
    [$._expression, $.type_reference],
    [$.array_type, $.array_literal],
    [$._expression, $._strict_type_reference],
  ],

  rules: {
    source_file: ($) => repeat($._statement),

    _statement: ($) => choice(
      $.import_statement,
      $.const_declaration,
      $.function_declaration,
      $.struct_declaration,
      $.enum_declaration,
      $.let_statement,
      $.assignment_statement,
      $.index_assignment_statement,
      $.if_statement,
      $.while_statement,
      $.return_statement,
      $.break_statement,
      $.continue_statement,
      $.match_statement,
      $.expression_statement,
    ),

    // ---- declarations ----

    import_statement: ($) => seq('yobu', field('path', $.string_literal), ';'),

    const_declaration: ($) => seq(
      optional('koukai'),
      'teisu',
      field('name', $.identifier),
      optional(seq(':', field('type', $._type))),
      '=',
      field('value', $._expression),
      ';',
    ),

    function_declaration: ($) => seq(
      optional('koukai'),
      'waza',
      field('name', $.identifier),
      optional(field('type_parameters', $.type_parameters)),
      field('parameters', $.parameters),
      optional(seq('->', field('return_type', $._type))),
      field('body', $.block),
    ),

    type_parameters: ($) => seq('<', commaSep1($.type_parameter), '>'),

    type_parameter: ($) => seq(
      field('name', $.identifier),
      optional(seq(':', field('constraint', $.identifier))),
    ),

    parameters: ($) => seq('(', commaSep($.parameter), ')'),

    parameter: ($) => seq(
      field('name', $.identifier),
      optional(seq(':', field('type', $._type))),
    ),

    struct_declaration: ($) => seq(
      optional('koukai'),
      'kata',
      field('name', $.identifier),
      optional(field('type_parameters', $.type_parameters)),
      '{',
      repeat($.field_declaration),
      '}',
    ),

    field_declaration: ($) => seq(
      field('name', $.identifier),
      ':',
      field('type', $._type),
      choice(',', ';'),
    ),

    enum_declaration: ($) => seq(
      optional('koukai'),
      'sentaku',
      field('name', $.identifier),
      optional(field('type_parameters', $.type_parameters)),
      '{',
      repeat($.enum_variant),
      '}',
    ),

    enum_variant: ($) => seq(
      field('name', $.identifier),
      optional(seq('(', commaSep($._type), ')')),
      choice(',', ';'),
    ),

    // ---- statements ----

    let_statement: ($) => seq(
      'kaku',
      field('name', $.identifier),
      optional(seq(':', field('type', $._type))),
      '=',
      field('value', $._expression),
      ';',
    ),

    assignment_statement: ($) => prec(1, seq(
      field('name', $.identifier),
      '=',
      field('value', $._expression),
      ';',
    )),

    index_assignment_statement: ($) => seq(
      field('target', $.index_expression),
      '=',
      field('value', $._expression),
      ';',
    ),

    if_statement: ($) => seq(
      'moshi',
      '(',
      field('condition', $._expression),
      ')',
      field('consequence', $.block),
      optional(seq('sore', field('alternative', $.block))),
    ),

    while_statement: ($) => seq(
      'aida',
      '(',
      field('condition', $._expression),
      ')',
      field('body', $.block),
    ),

    return_statement: ($) => seq('kaesu', optional(field('value', $._expression)), ';'),

    break_statement: (_$) => seq('nukeru', ';'),

    continue_statement: (_$) => seq('tsuzuku', ';'),

    match_statement: ($) => seq(
      'awase',
      '(',
      field('value', $._expression),
      ')',
      '{',
      repeat($.match_arm),
      '}',
    ),

    match_arm: ($) => seq(
      'baai',
      field('variant', $.identifier),
      optional(seq('(', commaSep($.identifier), ')')),
      field('body', $.block),
    ),

    expression_statement: ($) => seq($._expression, ';'),

    block: ($) => seq('{', repeat($._statement), '}'),

    // ---- types ----

    _type: ($) => seq(
      choice(
        $.type_reference,
        $.array_type,
        $.function_type,
        $.parenthesized_type,
      ),
    ),

    type_reference: ($) => seq(
      field('name', choice($.builtin_type, $.type_identifier, $.identifier)),
      optional(seq('.', field('member', choice($.type_identifier, $.identifier)))),
      optional(field('arguments', $.type_arguments)),
      repeat('?'),
    ),

    type_arguments: ($) => seq('<', commaSep1($._type), '>'),

    array_type: ($) => prec.right(seq('[', ']', field('element', $._type))),

    function_type: ($) => prec.right(seq(
      '(', commaSep($._type), ')', '->', field('result', $._type),
    )),

    parenthesized_type: ($) => seq('(', $._type, ')', repeat('?')),

    // ---- expressions ----

    _expression: ($) => choice(
      $.identifier,
      $.type_identifier,
      $.integer_literal,
      $.float_literal,
      $.string_literal,
      $.rune_literal,
      $.boolean_literal,
      $.nil_literal,
      $.array_literal,
      $.struct_literal,
      $.function_literal,
      $.parenthesized_expression,
      $.unary_expression,
      $.binary_expression,
      $.call_expression,
      $.index_expression,
      $.field_expression,
      $.type_application,
    ),

    parenthesized_expression: ($) => seq('(', $._expression, ')'),

    unary_expression: ($) => prec(PREC.prefix, seq(
      field('operator', choice('-', '!')),
      field('operand', $._expression),
    )),

    binary_expression: ($) => choice(
      ...[
        ['||', PREC.or],
        ['&&', PREC.and],
        ['==', PREC.equals],
        ['!=', PREC.equals],
        ['<', PREC.compare],
        ['<=', PREC.compare],
        ['>', PREC.compare],
        ['>=', PREC.compare],
        ['+', PREC.sum],
        ['-', PREC.sum],
        ['*', PREC.product],
        ['/', PREC.product],
      ].map(([operator, precedence]) => prec.left(precedence, seq(
        field('left', $._expression),
        field('operator', operator),
        field('right', $._expression),
      ))),
    ),

    call_expression: ($) => prec(PREC.call, seq(
      field('function', $._expression),
      field('arguments', $.arguments),
    )),

    arguments: ($) => seq('(', commaSep($._expression), ')'),

    index_expression: ($) => prec(PREC.index, seq(
      field('value', $._expression),
      '[',
      field('index', $._expression),
      ']',
    )),

    // Explicit type arguments: `first[int64](xs)`, `Pair[string] { ... }`, `Shape[int64].Empty()`.
    // The first argument must look like a type (builtin name, upper-case name, `[` or
    // `(`), exactly like the official parser's looksLikeTypeName check.
    type_application: ($) => prec.dynamic(2, prec(PREC.index, seq(
      field('function', $._expression),
      '[',
      $._type_application_first,
      repeat(seq(',', $._type)),
      ']',
    ))),

    _type_application_first: ($) => choice(
      alias($._strict_type_reference, $.type_reference),
      $.array_type,
      $.function_type,
      $.parenthesized_type,
    ),

    _strict_type_reference: ($) => seq(
      field('name', choice($.builtin_type, $.type_identifier)),
      optional(seq('.', field('member', choice($.type_identifier, $.identifier)))),
      optional(field('arguments', $.type_arguments)),
      repeat('?'),
    ),

    field_expression: ($) => prec(PREC.field, seq(
      field('value', $._expression),
      '.',
      field('field', $.identifier),
    )),

    array_literal: ($) => seq('[', commaSep($._expression), ']'),

    struct_literal: ($) => seq(
      field('type', choice($.identifier, $.type_identifier, $.field_expression, $.type_application)),
      '{',
      optional(seq(commaSep1($.field_initializer), optional(','))),
      '}',
    ),

    field_initializer: ($) => seq(
      field('name', $.identifier),
      ':',
      field('value', $._expression),
    ),

    function_literal: ($) => seq(
      'waza',
      field('parameters', $.parameters),
      '->',
      field('return_type', $._type),
      field('body', $.block),
    ),

    // ---- lexical ----

    builtin_type: (_$) => choice(...BUILTIN_TYPES),

    type_identifier: (_$) => /[A-Z][A-Za-z0-9_]*/,

    identifier: (_$) => /[A-Za-z_][A-Za-z0-9_]*/,

    integer_literal: (_$) => /[0-9]+/,

    float_literal: (_$) => token(choice(
      /[0-9]+\.[0-9]+([eE][+-]?[0-9]+)?/,
      /[0-9]+[eE][+-]?[0-9]+/,
    )),

    string_literal: (_$) => token(seq('"', repeat(choice(/[^"\\\n\r]/, /\\./)), '"')),

    rune_literal: (_$) => token(seq(
      "'",
      choice(
        /[^'\\\n\r]/,
        /\\[abfnrtv\\']/,
        /\\x[0-9a-fA-F]{2}/,
        /\\[0-7]{3}/,
        /\\u[0-9a-fA-F]{4}/,
        /\\U[0-9a-fA-F]{8}/,
      ),
      "'",
    )),

    boolean_literal: (_$) => choice('hai', 'iie'),

    nil_literal: (_$) => 'mu',

    line_comment: (_$) => token(seq('//', /[^\r\n]*/)),

    block_comment: (_$) => token(seq('/*', /[^*]*\*+([^/*][^*]*\*+)*/, '/')),
  },
});
