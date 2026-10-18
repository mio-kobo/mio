// Package bootstrapaudit 统计 Mochi 程序距离当前 Native AOT 的静态能力缺口。
// 它只读字节码，不调用 AOT，不修改编译器或 Mochi 文件格式。
package bootstrapaudit

import (
	"fmt"
	"sort"
	"strings"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// Status 描述当前 Native AOT 的实现边界，而非对任意程序可编译性的保证。
type Status string

const (
	StatusLowered Status = "lowered"
	StatusLimited Status = "limited"
	StatusMissing Status = "missing"
)

type Site struct {
	Block      string `json:"block"`
	FunctionID int    `json:"function_id"` // 主程序为 -1
	Offset     int    `json:"offset"`
	Operands   []int  `json:"operands,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type ConstantKind struct {
	Kind       string `json:"kind"`
	Count      int    `json:"count"`
	Referenced int    `json:"referenced"`
}

type OpcodeStat struct {
	Opcode         int    `json:"opcode"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	Status         Status `json:"status"`
	Count          int    `json:"count"`
	Lowered        int    `json:"lowered"`
	Limited        int    `json:"limited"`
	Missing        int    `json:"missing"`
	AffectedBlocks int    `json:"affected_blocks"`
	ExampleSites   []Site `json:"example_sites,omitempty"`
	Notes          string `json:"notes,omitempty"`
}

type FunctionStat struct {
	FunctionID     int    `json:"function_id"`
	Name           string `json:"name"`
	Instructions   int    `json:"instructions"`
	ByteLength     int    `json:"byte_length"`
	ParameterCount int    `json:"parameter_count"`
	LocalCount     int    `json:"local_count"`
	Captures       int    `json:"captures"`
	ReturnsValue   bool   `json:"returns_value"`
	Missing        int    `json:"missing"`
	Limited        int    `json:"limited"`
	EmptyBody      bool   `json:"empty_body,omitempty"`
}

type NativeCandidate struct {
	FunctionID int    `json:"function_id"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
}

type StructuralIssue struct {
	Block  string `json:"block"`
	Kind   string `json:"kind"`
	Value  int    `json:"value"`
	Limit  int    `json:"limit"`
	Reason string `json:"reason"`
}

func nativeFSSupported(name string) bool {
	const prefix = "$mma::mio/fs/runtime::"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := strings.TrimPrefix(name, prefix)
	op, _, ok := strings.Cut(rest, "::")
	if !ok {
		return false
	}
	switch op {
	case "open", "read", "write", "close", "file_status", "open_dir",
		"dir_next", "dir_name", "dir_is_dir", "dir_is_file", "dir_status", "dir_close":
		return true
	default:
		return false
	}
}

type Report struct {
	SchemaVersion         int               `json:"schema_version"`
	CompilerBaseline      string            `json:"compiler_baseline"`
	ProgramSizeBytes      int               `json:"program_size_bytes,omitempty"`
	SourceSHA256          string            `json:"source_sha256,omitempty"`
	MochiVersion          int               `json:"mochi_version,omitempty"`
	FunctionCount         int               `json:"function_count"`
	FunctionsWithCaptures int               `json:"functions_with_captures"`
	FunctionsWithoutBody  int               `json:"functions_without_body"`
	GlobalsCount          int               `json:"globals_count"`
	MaxLocalSlots         int               `json:"max_local_slots"`
	TotalInstructions     int               `json:"total_instructions"`
	DistinctOpcodes       int               `json:"distinct_opcodes"`
	LoweredInstructions   int               `json:"lowered_instructions"`
	LimitedInstructions   int               `json:"limited_instructions"`
	MissingInstructions   int               `json:"missing_instructions"`
	ConstantCount         int               `json:"constant_count"`
	Constants             []ConstantKind    `json:"constants"`
	Opcodes               []OpcodeStat      `json:"opcodes"`
	Functions             []FunctionStat    `json:"functions"`
	NativeCandidates      []NativeCandidate `json:"native_candidates,omitempty"`
	StructuralIssues      []StructuralIssue `json:"structural_issues,omitempty"`
	Notices               []string          `json:"notices"`
}

type accumulator struct {
	item     OpcodeStat
	blockIDs map[int]bool
}

// Analyze 检查主程序和所有函数（包括从未调用的函数）。
// 此处不做控制流可达性分析，出现一次便计入一次，以免遗漏潜在缺口。
func Analyze(program *compiler.Bytecode) (*Report, error) {
	if program == nil {
		return nil, fmt.Errorf("无法分析 nil Mochi 程序")
	}
	result := &Report{
		SchemaVersion:    1,
		CompilerBaseline: "M11c + M12 + M13a/M13b + M15a/M15b macOS ARM64（静态降级能力矩阵）",
		FunctionCount:    len(program.Functions),
		GlobalsCount:     program.GlobalsCount,
		ConstantCount:    len(program.Constants),
		Notices: []string{
			"这是静态 Opcode/常量清单，不是完整的 Native AOT 编译结果；'limited' 表示仅特定类型、调用方式或规模有实现。",
			"统计包含不可达字节码和未使用函数；不能把降级条数占比当成 Native 自举完成百分比。",
			"M12 Struct/Array 已接入 M11b 精确 Root 与自动 Mark-Sweep；普通程序默认 1 MiB，NativeFS 自举档受 1 GiB 硬上限约束。",
			"M13b Enum 使用独立 TypeID、自描述 payload 引用位图，并由 M11 对象扫描器精确追踪。",
			"M14 已识别的 NativeFS 无函数体条目走 Darwin ARM64 MMA bridge；其他条目仍列为潜在外部绑定。",
		},
	}
	if program.GlobalsCount > darwinarm64.MaxGlobalSlots {
		result.StructuralIssues = append(result.StructuralIssues, StructuralIssue{"<main>", "global_slots", program.GlobalsCount, darwinarm64.MaxGlobalSlots, "全局槽位数量超过当前 Mach-O __DATA 限制"})
	}
	kinds := make(map[string]*ConstantKind)
	for _, v := range program.Constants {
		kind := v.Kind().String()
		if kinds[kind] == nil {
			kinds[kind] = &ConstantKind{Kind: kind}
		}
		kinds[kind].Count++
	}
	stats := make(map[code.Opcode]*accumulator)
	referencedKinds := make([]int, len(program.Constants))
	blocks := []struct {
		id     int
		name   string
		source code.Instructions
		fn     *compiler.CompiledFunction
	}{{-1, "<main>", program.Instructions, nil}}
	for i := range program.Functions {
		fn := &program.Functions[i]
		name := fn.Name
		if name == "" {
			name = fmt.Sprintf("<function:%d>", i)
		}
		blocks = append(blocks, struct {
			id     int
			name   string
			source code.Instructions
			fn     *compiler.CompiledFunction
		}{i, name, fn.Instructions, fn})
		if fn.LocalCount > result.MaxLocalSlots {
			result.MaxLocalSlots = fn.LocalCount
		}
		if len(fn.Captures) > 0 {
			result.FunctionsWithCaptures++
		}
		if fn.ParameterCount > darwinarm64.MaxM15Parameters {
			result.StructuralIssues = append(result.StructuralIssues, StructuralIssue{name, "parameter_count", fn.ParameterCount, darwinarm64.MaxM15Parameters, "函数参数数量超出当前原生 ABI"})
		}
		if fn.LocalCount > darwinarm64.MaxM4Locals {
			result.StructuralIssues = append(result.StructuralIssues, StructuralIssue{name, "local_count", fn.LocalCount, darwinarm64.MaxM4Locals, "函数局部槽位数量超出当前原生栈帧限制"})
		}
		if len(fn.Instructions) == 0 {
			result.FunctionsWithoutBody++
		}
		if (len(fn.Instructions) == 0 || strings.HasPrefix(fn.Name, "$mma::")) && !nativeFSSupported(fn.Name) {
			reason := "缺少函数体，可能需要 Runtime/MMA 桥接"
			if strings.HasPrefix(fn.Name, "$mma::") {
				reason = "MMA 原生函数绑定，需要单独的运行时桥接"
			}
			result.NativeCandidates = append(result.NativeCandidates, NativeCandidate{i, name, reason})
		}
	}
	for _, block := range blocks {
		fs := FunctionStat{FunctionID: block.id, Name: block.name, ByteLength: len(block.source)}
		if block.fn != nil {
			fs.ParameterCount = block.fn.ParameterCount
			fs.LocalCount = block.fn.LocalCount
			fs.Captures = len(block.fn.Captures)
			fs.ReturnsValue = block.fn.ReturnsValue
			fs.EmptyBody = len(block.source) == 0
		}
		boundaries := make(map[int]bool)
		jumpTargets := make([]int, 0)
		for offset := 0; offset < len(block.source); {
			boundaries[offset] = true
			op := code.Opcode(block.source[offset])
			def, err := code.Lookup(op)
			if err != nil {
				return nil, fmt.Errorf("%s 偏移 %d: %w", block.name, offset, err)
			}
			operands, width, err := code.ReadOperands(def, block.source[offset+1:])
			if err != nil {
				return nil, fmt.Errorf("%s 偏移 %d: %w", block.name, offset, err)
			}
			if width < 0 || offset+1+width > len(block.source) {
				return nil, fmt.Errorf("%s 偏移 %d: 指令操作数越界", block.name, offset)
			}
			if (def.Name == "OpJump" || def.Name == "OpJumpIfFalse") && len(operands) > 0 {
				jumpTargets = append(jumpTargets, operands[0])
			}
			if def.Name == "OpConstant" && len(operands) > 0 {
				if operands[0] < 0 || operands[0] >= len(program.Constants) {
					return nil, fmt.Errorf("%s 偏移 %d: 常量 %d 越界", block.name, offset, operands[0])
				}
				referencedKinds[operands[0]]++
			}
			status, category, notes, reason := classify(def.Name, operands, block.id, block.fn, program)
			// 使用每个实际位置的状态，而不是按 Opcode 给整个程序贴一个标签。
			a := stats[op]
			if a == nil {
				a = &accumulator{item: OpcodeStat{Opcode: int(op), Name: def.Name, Category: category, Status: status, Notes: notes}, blockIDs: make(map[int]bool)}
				stats[op] = a
			}
			a.item.Count++
			a.blockIDs[block.id] = true
			if status == StatusMissing {
				a.item.Missing++
				result.MissingInstructions++
				fs.Missing++
			}
			if status == StatusLimited {
				a.item.Limited++
				result.LimitedInstructions++
				fs.Limited++
			}
			if status == StatusLowered {
				a.item.Lowered++
				result.LoweredInstructions++
			}
			// 缺口样例优先，受限调用点不应覆盖后续发现的真实失败位置。
			if status == StatusMissing && a.item.Missing == 1 {
				a.item.ExampleSites = nil
			}
			if len(a.item.ExampleSites) < 3 && (status == StatusMissing || (status == StatusLimited && a.item.Missing == 0)) {
				site := Site{Block: block.name, FunctionID: block.id, Offset: offset, Operands: append([]int(nil), operands...), Reason: reason}
				a.item.ExampleSites = append(a.item.ExampleSites, site)
			}
			// 同一个 Opcode 可能同时存在有限实现和明确缺失的调用点。
			if a.item.Missing > 0 {
				a.item.Status = StatusMissing
			} else if a.item.Limited > 0 {
				a.item.Status = StatusLimited
			}
			fs.Instructions++
			result.TotalInstructions++
			offset += 1 + width
		}
		boundaries[len(block.source)] = true
		for _, target := range jumpTargets {
			if !boundaries[target] {
				return nil, fmt.Errorf("%s 中的跳转目标 %d 不是合法指令边界", block.name, target)
			}
		}
		result.Functions = append(result.Functions, fs)
	}
	for i, v := range program.Constants {
		if referencedKinds[i] > 0 {
			kinds[v.Kind().String()].Referenced++
		}
	}
	for _, v := range kinds {
		result.Constants = append(result.Constants, *v)
	}
	sort.Slice(result.Constants, func(i, j int) bool { return result.Constants[i].Kind < result.Constants[j].Kind })
	for _, st := range stats {
		st.item.AffectedBlocks = len(st.blockIDs)
		result.Opcodes = append(result.Opcodes, st.item)
	}
	sort.Slice(result.Opcodes, func(i, j int) bool { return result.Opcodes[i].Opcode < result.Opcodes[j].Opcode })
	result.DistinctOpcodes = len(result.Opcodes)
	sort.Slice(result.Functions, func(i, j int) bool { return result.Functions[i].FunctionID < result.Functions[j].FunctionID })
	sort.Slice(result.NativeCandidates, func(i, j int) bool {
		return result.NativeCandidates[i].FunctionID < result.NativeCandidates[j].FunctionID
	})
	return result, nil
}

// classify 只描述当前已有的编译路径，不把“有 Op 处理”误报为完整语义支持。
func classify(name string, operands []int, blockID int, fn *compiler.CompiledFunction, p *compiler.Bytecode) (Status, string, string, string) {
	reason := "需要核对操作数类型、控制流和 AOT 资源上限"
	switch name {
	case "OpTrue", "OpFalse", "OpNot", "OpJump", "OpJumpIfFalse":
		return StatusLowered, "control", "M3 已实现布尔与控制流（仍需全程序验证）", ""
	case "OpReturn", "OpReturnValue":
		return StatusLowered, "function", "M4 已实现原生函数返回（仍需合法 Stack Frame）", ""
	case "OpConstant":
		if len(operands) > 0 && operands[0] >= 0 && operands[0] < len(p.Constants) {
			k := p.Constants[operands[0]].Kind()
			if k != value.KindInt && k != value.KindInt8 && k != value.KindInt16 && k != value.KindInt32 &&
				k != value.KindUint8 && k != value.KindUint16 && k != value.KindUint32 &&
				k != value.KindBool && k != value.KindFloat && k != value.KindFloat32 && k != value.KindString {
				return StatusMissing, "constant", "当前只处理整数、bool、float32、float64 和静态字符串", "常量类型 " + k.String() + " 尚未实现"
			}
		}
		return StatusLimited, "constant", "静态字符串/数字常量及 M13 可空装箱已实现；复杂常量对象尚未实现", reason
	case "OpGetGlobal", "OpSetGlobal":
		if blockID >= 0 {
			return StatusMissing, "slot", "M11a 的函数体内全局槽读写仍被 AOT 拒绝", "函数体内 Global 访问不支持"
		}
		return StatusLimited, "slot", "主程序 Global 槽已实现，类型/初始化有约束", reason
	case "OpGetLocal", "OpSetLocal", "OpDefineLocal":
		return StatusLimited, "slot", "函数 Local 槽已有实现；仅支持当前 AOT 已有值类型", reason
	case "OpAdd", "OpSub", "OpMul", "OpDiv", "OpNeg", "OpEqual", "OpNotEqual", "OpLess", "OpLessEqual", "OpGreater", "OpGreaterEqual":
		return StatusLimited, "numeric", "基础 int64、float32、float64 和受限比较；复合类型不支持", reason
	case "OpChant", "OpPop":
		return StatusLimited, "output", "M7 的静态字符串及 M6b 浮点输出可用；动态对象不支持", reason
	case "OpCall":
		if len(operands) == 0 || operands[0] < 0 || operands[0] >= len(p.Functions) {
			return StatusMissing, "function", "静态 Call 已实现但目标须是可编译的普通函数", "函数索引无效"
		}
		target := p.Functions[operands[0]]
		if nativeFSSupported(target.Name) {
			return StatusLowered, "ffi", "M14 Darwin ARM64 NativeFS MMA bridge", ""
		}
		if len(target.Captures) > 0 {
			return StatusMissing, "function", "不支持闭包捕获环境", "调用了带 Captures 的函数"
		}
		if len(target.Instructions) == 0 || strings.HasPrefix(target.Name, "$mma::") {
			return StatusMissing, "ffi", "需要额外的文件系统/MMA Runtime", "调用了无原生函数体的目标：" + target.Name
		}
		return StatusLimited, "function", "无捕获的静态 OpCall 已实现；动态调用/外部绑定不支持", reason
	case "OpIntToFloat", "OpFloatToInt", "OpIntToFloat32", "OpFloat32ToInt", "OpFloat32ToFloat64", "OpFloat64ToFloat32":
		return StatusLimited, "numeric", "M6a 已实现浮点转换，包含范围和特殊值约束", reason
	case "OpArrayTyped":
		if len(operands) > 1 && !m12AuditDescriptorSupported(operands[1]) {
			return StatusMissing, "array", "M13a 支持当前 M6 标量与 nullable 的 string、array、struct 元素", "数组元素 descriptor 尚未支持"
		}
		return StatusLimited, "array", "M12.1 descriptor/storage 已接入 M11b 精确扫描与自动 GC", reason
	case "OpArrayMake", "OpArrayWithCapacity":
		if len(operands) > 0 && !m12AuditDescriptorSupported(operands[0]) {
			return StatusMissing, "array", "M13a 支持当前 M6 标量与 nullable 的 string、array、struct 元素", "数组元素 descriptor 尚未支持"
		}
		return StatusLimited, "array", "M12 事务式分配已接入 M11c 自动 GC；Heap 上限仍为 1 MiB", reason
	case "OpArray", "OpArrayPush", "OpArrayCapacity", "OpArrayReserve", "OpArrayClone", "OpArrayTruncate", "OpArrayAppendAll", "OpIndex", "OpSetIndex", "OpLen":
		return StatusLimited, "array", "M12 共享数组与浅复制操作已接入精确自动 GC；Heap 上限仍为 1 MiB", reason
	case "OpSetNullableIndex":
		return StatusLimited, "array", "M13a 可空数组写入已接入精确 GC；仍受当前元素类型集合限制", reason
	case "OpStruct", "OpGetField":
		return StatusLimited, "struct", "M12.1 结构体字段位图已接入 M11b 精确扫描与自动 GC", reason
	case "OpEnum", "OpEnumTag", "OpGetEnumValue":
		return StatusLimited, "enum", "M13b 原生 Enum 已接入变体标签、关联值位图与 M11 精确 GC；仍受现有静态类型推断边界限制", reason
	case "OpMap", "OpMapGet", "OpMapSet":
		return StatusMissing, "map", "需要哈希表、动态内存和精确 GC", "M11a 无 Map 降级路径"
	case "OpFunction", "OpCallValue", "OpClosure", "OpGetUpvalue", "OpSetUpvalue":
		return StatusLimited, "closure", "M15b Closure/Environment/Cell 已接入 M11 精确 GC；仍受现有 AOT 值类型与函数签名推断约束", reason
	case "OpNil", "OpIsNil":
		return StatusLimited, "nullable", "M13a 已实现可空标量装箱、空引用和精确 GC", reason
	case "OpMath":
		return StatusLowered, "math", "M13c 已实现 Compiler A 使用的 is_inf 与非返回 error 原语", ""
	case "OpIntegerConvert":
		return StatusLowered, "narrow-int", "M13c 已实现有界转换、窄位回绕和符号/零扩展", ""
	case "OpDataConvert":
		return StatusLowered, "data", "M13c 已实现 string/[]uint8 独立复制与 UTF-8 校验", ""
	default:
		return StatusMissing, "other", "已由 Mochi 识别，但当前 Native AOT 未实现", "缺少 Native AOT Opcode 实现"
	}
}

func m12AuditDescriptorSupported(descriptor int) bool {
	switch value.Kind(descriptor & 0x7f) {
	case value.KindInvalid, value.KindInt, value.KindInt8, value.KindInt16, value.KindInt32,
		value.KindUint8, value.KindUint16, value.KindUint32,
		value.KindFloat, value.KindFloat32, value.KindBool, value.KindString,
		value.KindArray, value.KindStruct, value.KindEnum:
		return true
	default:
		return false
	}
}
