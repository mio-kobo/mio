package ast

import (
	"Mio/internal/token"
)

// 源码位置范围
type Span struct {
	Start token.Position
	End   token.Position
}

// AST 节点基础接口
type Node interface {
	Span() Span
}

// 表达式节点接口
type Expression interface {
	Node
	expressionNode()
}

// 语句节点接口
type Statement interface {
	Node
	statementNode()
}

// 整数字面量
type IntegerLiteral struct {
	Value int64
	Range Span
}

// 浮点字面量
type FloatLiteral struct {
	Value float64
	Range Span
}

// 表达式语句
type ExpressionStatement struct {
	Expr  Expression
	Range Span
}

// 程序根节点
type Program struct {
	Statements []Statement
	Range      Span
}

// 标识符
type Identifier struct {
	Name         string
	DeclaredType *TypeRef
	Range        Span
}

// 中缀表达式
type InfixExpression struct {
	Left     Expression
	Operator token.Type
	Right    Expression
	Range    Span
}

// 括号分组表达式
type GroupedExpression struct {
	Expr   Expression
	Ranges Span
}

// 一元前缀表达式
type PrefixExpression struct {
	Operator token.Type
	Right    Expression
	Range    Span
}

// hai / iie 布尔字面量
type BooleanLiteral struct {
	Value bool
	Range Span
}

// 字符串字面量
type StringLiteral struct {
	Value string
	Range Span
}

// mu 空值字面量
type NilLiteral struct {
	Range Span
}

// kaku 变量声明语句
type LetStatement struct {
	Name  *Identifier
	Value Expression
	Range Span
}

// 已有变量赋值语句
type AssignStatement struct {
	Name  *Identifier
	Value Expression
	Range Span
}

// IndexAssignStatement 表示数组元素赋值
type IndexAssignStatement struct {
	Target   *IndexExpression
	Value    Expression
	Nullable bool
	Range    Span
}

// 代码块语句
type BlockStatement struct {
	Statements []Statement
	Range      Span
}

// moshi / sore 条件判断语句
type IfStatement struct {
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
	Range       Span
}

// aida 循环语句
type WhileStatement struct {
	Condition Expression
	Body      *BlockStatement
	Range     Span
}

// nukeru 循环退出语句
type BreakStatement struct {
	Range Span
}

// tsuzuku 循环继续语句
type ContinueStatement struct {
	Range Span
}

// 函数调用表达式
type CallExpression struct {
	Function  Expression
	Arguments []Expression
	EnumType  string
	Variant   int
	Dynamic   bool
	MapCreate bool
	HasValue  bool
	Range     Span
}

// TypeApplyExpression 表示显式函数类型参数
type TypeApplyExpression struct {
	Function Expression
	Args     []*TypeRef
	Range    Span
}

// 数值转换表达式
type ConversionExpression struct {
	Target    string
	Arguments []Expression
	Range     Span
}

// ArrayLiteral 表示数组字面量
type ArrayLiteral struct {
	Elements []Expression
	Range    Span
}

// IndexExpression 表示数组索引读取
type IndexExpression struct {
	Left  Expression
	Index Expression
	Map   bool
	Range Span
}

// FieldExpression 表示字段读取
type FieldExpression struct {
	Left  Expression
	Name  string
	Index int
	Range Span
}

// StructLiteral 表示结构体构造
type StructLiteral struct {
	TypeName string
	TypeArgs []*TypeRef
	Fields   []FieldValue
	Range    Span
}

// FieldValue 保存结构体字段初始化
type FieldValue struct {
	Name  string
	Value Expression
	Range Span
}

// EnumLiteral 表示枚举变体构造
type EnumLiteral struct {
	TypeName string
	TypeArgs []*TypeRef
	Variant  string
	Values   []Expression
	Range    Span
}

// kaesu 返回语句
type ReturnStatement struct {
	Value Expression
	Range Span
}

// waza 函数定义语句
type FunctionStatement struct {
	Name       *Identifier
	TypeParams []string
	Parameters []*Identifier
	ReturnType *TypeRef
	Body       *BlockStatement
	Range      Span
}

// StructStatement 表示具名结构体声明
type StructStatement struct {
	Name       *Identifier
	TypeParams []string
	Fields     []*Identifier
	Range      Span
}

// EnumVariant 表示枚举变体声明
type EnumVariant struct {
	Name   string
	Fields []*TypeRef
	Range  Span
}

// EnumStatement 表示具名枚举声明
type EnumStatement struct {
	Name       *Identifier
	TypeParams []string
	Variants   []EnumVariant
	Range      Span
}

// MatchArm 表示枚举分支
type MatchArm struct {
	Variant  string
	Tag      int
	Bindings []*Identifier
	Body     *BlockStatement
	Range    Span
}

// MatchStatement 表示穷尽枚举分支
type MatchStatement struct {
	Value Expression
	Arms  []MatchArm
	Range Span
}

type TypeRef struct {
	Name    string
	Element *TypeRef
	Args    []*TypeRef
	Params  []*TypeRef
	Result  *TypeRef
	Range   Span
}

// 整数字面量
func (n *IntegerLiteral) expressionNode() {}

func (n *IntegerLiteral) Span() Span {
	return n.Range
}

func (n *FloatLiteral) expressionNode() {}

func (n *FloatLiteral) Span() Span {
	return n.Range
}

func (n *ArrayLiteral) expressionNode() {}

func (n *ArrayLiteral) Span() Span {
	return n.Range
}

func (n *IndexExpression) expressionNode() {}

func (n *IndexExpression) Span() Span {
	return n.Range
}

func (n *FieldExpression) expressionNode() {}

func (n *FieldExpression) Span() Span { return n.Range }

func (n *StructLiteral) expressionNode() {}

func (n *StructLiteral) Span() Span { return n.Range }

func (n *EnumLiteral) expressionNode() {}

func (n *EnumLiteral) Span() Span { return n.Range }

// 表达式语句
func (n *ExpressionStatement) statementNode() {}

func (n *ExpressionStatement) Span() Span {
	return n.Range
}

// 程序根节点
func (p *Program) Span() Span {
	return p.Range
}

// 标识符
func (n *Identifier) expressionNode() {}

func (n *Identifier) Span() Span {
	return n.Range
}

// 中缀表达式
func (n *InfixExpression) expressionNode() {}

func (n *InfixExpression) Span() Span {
	return n.Range
}

// 括号分组表达式
func (n *GroupedExpression) expressionNode() {}

func (n *GroupedExpression) Span() Span {
	return n.Ranges
}

// 一元前缀表达式
func (n *PrefixExpression) expressionNode() {}

func (n *PrefixExpression) Span() Span {
	return n.Range
}

// 布尔字面量
func (n *BooleanLiteral) expressionNode() {}

func (n *BooleanLiteral) Span() Span {
	return n.Range
}

// 字符串字面量
func (n *StringLiteral) expressionNode() {}

func (n *StringLiteral) Span() Span {
	return n.Range
}

// 空值字面量
func (n *NilLiteral) expressionNode() {}

func (n *NilLiteral) Span() Span {
	return n.Range
}

// 变量声明语句
func (n *LetStatement) statementNode() {}

func (n *LetStatement) Span() Span {
	return n.Range
}

// 变量赋值语句
func (n *AssignStatement) statementNode() {}

func (n *AssignStatement) Span() Span {
	return n.Range
}

func (n *IndexAssignStatement) statementNode() {}

func (n *IndexAssignStatement) Span() Span {
	return n.Range
}

// 代码块语句
func (n *BlockStatement) statementNode() {}

func (n *BlockStatement) Span() Span {
	return n.Range
}

// 条件判断语句
func (n *IfStatement) statementNode() {}

func (n *IfStatement) Span() Span {
	return n.Range
}

func (n *WhileStatement) statementNode() {}

func (n *WhileStatement) Span() Span {
	return n.Range
}

func (n *BreakStatement) statementNode() {}

func (n *BreakStatement) Span() Span {
	return n.Range
}

func (n *ContinueStatement) statementNode() {}

func (n *ContinueStatement) Span() Span {
	return n.Range
}

// 函数调用表达式
func (n *CallExpression) expressionNode() {}

func (n *CallExpression) Span() Span {
	return n.Range
}

func (n *ConversionExpression) expressionNode() {}

func (n *ConversionExpression) Span() Span {
	return n.Range
}

func (n *TypeApplyExpression) expressionNode() {}

func (n *TypeApplyExpression) Span() Span { return n.Range }

// 返回语句
func (n *ReturnStatement) statementNode() {}

func (n *ReturnStatement) Span() Span {
	return n.Range
}

// 函数定义语句
func (n *FunctionStatement) statementNode() {}

func (n *FunctionStatement) Span() Span {
	return n.Range
}

func (n *StructStatement) statementNode() {}

func (n *StructStatement) Span() Span { return n.Range }

func (n *EnumStatement) statementNode() {}

func (n *EnumStatement) Span() Span { return n.Range }

func (n *MatchStatement) statementNode() {}

func (n *MatchStatement) Span() Span { return n.Range }

func (t *TypeRef) Span() Span {
	return t.Range
}
