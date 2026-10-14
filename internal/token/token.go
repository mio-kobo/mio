package token

type Type string

const ( // 定义特殊 Token
	Illegal Type = "ILLEGAL" // 无法识别
	EOF     Type = "EOF"     // 代码结束

	Ident   Type = "IDENT"   // 普通标识符
	Integer Type = "INTEGER" // 整数字面量
	Float   Type = "FLOAT"   // 浮点字面量
	String  Type = "STRING"  // 字符串字面量
)

const ( // 定义运算符 Token
	Assign Type = "ASSIGN"

	Plus     Type = "PLUS"
	Minus    Type = "MINUS"
	Asterisk Type = "ASTERISK"
	Slash    Type = "SLASH"

	Bang     Type = "BANG"
	Question Type = "QUESTION"

	Equal    Type = "EQUAL"
	NotEqual Type = "NOT_EQUAL"

	Less         Type = "LESS"
	LessEqual    Type = "LESS_EQUAL"
	Greater      Type = "GREATER"
	GreaterEqual Type = "GREATER_EQUAL"

	And Type = "AND"
	Or  Type = "OR"
)

const ( // 定义分隔符 Token
	Comma     Type = "COMMA"
	Semicolon Type = "SEMICOLON"
	LParen    Type = "LPAREN"
	RParen    Type = "RPAREN"
	LBrace    Type = "LBRACE"
	RBrace    Type = "RBRACE"
	LBracket  Type = "LBRACKET"
	RBracket  Type = "RBRACKET"
	Dot       Type = "DOT"
)

const ( // 定义关键字 Token
	Let      Type = "LET"
	Function Type = "FUNCTION"
	If       Type = "IF"
	Else     Type = "ELSE"
	Return   Type = "RETURN"
	True     Type = "TRUE"
	False    Type = "FALSE"
	Nil      Type = "NIL"
)

const ( // 未来启用关键字 Token
	While    Type = "WHILE"
	Break    Type = "BREAK"
	Continue Type = "CONTINUE"
	Struct   Type = "STRUCT"
	Enum     Type = "ENUM"
	Match    Type = "MATCH"
	Case     Type = "CASE"

	Import Type = "IMPORT"
)

const (
	Colon Type = ":"
	Arrow Type = "->"
)

type Position struct { // 定义源码位置
	Offset int
	Line   int
	Column int
}

type Token struct { // 定义 Token 结构体
	Type    Type
	Literal string

	Start Position
	End   Position
}

type LanguageVersion uint8 // 定义版本号

const (
	V01 LanguageVersion = iota
	V02
	V03
	V04
)

// 定义关键字表
var keywordsV01 = map[string]Type{
	"kaku":  Let,
	"waza":  Function,
	"moshi": If,
	"sore":  Else,

	"kaesu": Return,

	"hai": True,
	"iie": False,
	"mu":  Nil,
}

var keywordsV02 = map[string]Type{
	"aida":    While,
	"nukeru":  Break,
	"tsuzuku": Continue,
}

var keywordsV03 = map[string]Type{
	"yobu": Import,
}

var keywordsV04 = map[string]Type{
	"kata":    Struct,
	"sentaku": Enum,
	"awase":   Match,
	"baai":    Case,
}

func LookupIdent(
	literal string,
	version LanguageVersion,
) Type {

	if tok, ok := keywordsV01[literal]; ok {
		return tok
	}

	if version >= V02 {
		if tok, ok := keywordsV02[literal]; ok {
			return tok
		}
	}

	if version >= V03 {
		if tok, ok := keywordsV03[literal]; ok {
			return tok
		}
	}
	if version >= V04 {
		if tok, ok := keywordsV04[literal]; ok {
			return tok
		}
	}

	return Ident

}
