package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"Mio/internal/mochi"
	"Mio/internal/value"
	"Mio/internal/vm"
)

func runConvertSource(t testing.TB, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

func runSourceVM(t testing.TB, source string, instructionLimit uint64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	machine := vm.NewWithOutput(bytecode, &output)
	if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: instructionLimit}); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func runConvertValue(t testing.TB, source string, instructionLimit uint64) value.Value {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		t.Fatal(err)
	}
	machine := vm.NewWithOutput(bytecode, io.Discard)
	if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: instructionLimit}); err != nil {
		t.Fatal(err)
	}
	result, ok := machine.LastPoppedValue()
	if !ok {
		t.Fatal("missing VM result")
	}
	return result
}

func mioStringLiteral(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + replacer.Replace(value) + `"`
}

func mioFloatLiteral(value string) string {
	if !strings.ContainsAny(value, ".eE") {
		return value + ".0"
	}
	return value
}

func exactBinaryFraction(numerator int64, exponent int64) string {
	coefficient := big.NewInt(numerator)
	power := new(big.Int).Exp(big.NewInt(5), big.NewInt(-exponent), nil)
	coefficient.Mul(coefficient, power)
	digits := coefficient.String()
	places := int(-exponent)
	if len(digits) <= places {
		return "0." + strings.Repeat("0", places-len(digits)) + digits
	}
	point := len(digits) - places
	return digits[:point] + "." + digits[point:]
}

// 测试 int64 严格解析的边界、符号和非法输入
func TestCLIKisoConvertParseInt(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0", "0"}, {"-0", "0"}, {"+0", "0"}, {"+42", "42"}, {"-42", "-42"}, {"00042", "42"},
		{"9223372036854775807", "9223372036854775807"}, {"-9223372036854775808", "-9223372036854775808"},
		{"9223372036854775808", "mu"}, {"-9223372036854775809", "mu"}, {"", "mu"}, {"-", "mu"}, {"+", "mu"},
		{"12a3", "mu"}, {" 42", "mu"}, {"42 ", "mu"}, {"42\n", "mu"}, {"1_0", "mu"}, {"1,0", "mu"},
		{"1.0", "mu"}, {"１２", "mu"}, {"42中", "mu"}, {"42日", "mu"}, {"42🍡", "mu"}, {"4\x002", "mu"},
		{strings.Repeat("9", 4096), "mu"}, {"000000000000000000009223372036854775807", "9223372036854775807"},
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	for index, test := range tests {
		fmt.Fprintf(&source, "kaku result%d = convert.parse_int(%s); moshi (result%d == mu) { chant(\"mu\"); } sore { chant(result%d); }\n", index, mioStringLiteral(test.input), index, index)
	}
	stdout, stderr, exit := runConvertSource(t, source.String())
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := make([]string, len(tests))
	for index, test := range tests {
		want[index] = test.want
	}
	if got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("output=%q, want %q", got, want)
	}
}

// 测试 uint32 严格解析的边界和非法符号
func TestCLIKisoConvertParseUint(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"0", "0"}, {"+0", "0"}, {"00042", "42"}, {"4294967295", "4294967295"},
		{"4294967296", "mu"}, {"-1", "mu"}, {"", "mu"}, {"+", "mu"}, {"1a", "mu"},
		{" 1", "mu"}, {"1\n", "mu"}, {"1中", "mu"}, {strings.Repeat("7", 4096), "mu"},
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	for index, test := range tests {
		fmt.Fprintf(&source, "kaku result%d = convert.parse_uint(%s); moshi (result%d == mu) { chant(\"mu\"); } sore { chant(result%d); }\n", index, mioStringLiteral(test.input), index, index)
	}
	stdout, stderr, exit := runConvertSource(t, source.String())
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := make([]string, len(tests))
	for index, test := range tests {
		want[index] = test.want
	}
	if got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("output=%q, want %q", got, want)
	}
}

// 测试整数解析与 Go strconv 的独立参考结果一致
func TestCLIKisoConvertMatchesStrconv(t *testing.T) {
	intInputs := []string{"1", "+17", "-17", "000001", "2147483647", "-2147483648", "9223372036854775807", "-9223372036854775808", "9223372036854775808", "--1", "1e2", "日本語"}
	uintInputs := []string{"1", "+17", "000001", "2147483648", "4294967295", "4294967296", "-0", "1e2", "🍡"}
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	var want []string
	for index, input := range intInputs {
		fmt.Fprintf(&source, "kaku signed%d = convert.parse_int(%s); moshi (signed%d == mu) { chant(\"mu\"); } sore { chant(signed%d); }\n", index, mioStringLiteral(input), index, index)
		parsed, err := strconv.ParseInt(input, 10, 64)
		if err != nil {
			want = append(want, "mu")
		} else {
			want = append(want, strconv.FormatInt(parsed, 10))
		}
	}
	for index, input := range uintInputs {
		fmt.Fprintf(&source, "kaku unsigned%d = convert.parse_uint(%s); moshi (unsigned%d == mu) { chant(\"mu\"); } sore { chant(unsigned%d); }\n", index, mioStringLiteral(input), index, index)
		reference := input
		if strings.HasPrefix(reference, "+") {
			reference = strings.TrimPrefix(reference, "+")
		}
		parsed, err := strconv.ParseUint(reference, 10, 32)
		if err != nil {
			want = append(want, "mu")
		} else {
			want = append(want, strconv.FormatUint(parsed, 10))
		}
	}
	stdout, stderr, exit := runConvertSource(t, source.String())
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if got := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n"); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("output=%q, want %q", got, want)
	}
}

// 测试整数格式化边界和独立预期结果
func TestCLIKisoConvertFormatIntegers(t *testing.T) {
	stdout, stderr, exit := runConvertSource(t, `
yobu "kiso/convert";
chant(convert.format_int(0));
chant(convert.format_int(42));
chant(convert.format_int(-42));
chant(convert.format_int(9223372036854775807));
chant(convert.format_int(-9223372036854775807 - 1));
chant(convert.format_uint(uint32(0)));
chant(convert.format_uint(uint32(42)));
chant(convert.format_uint(uint32(4294967295)));
`)
	want := "0\n42\n-42\n9223372036854775807\n-9223372036854775808\n0\n42\n4294967295\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want)
	}
}

// 测试整数格式化与解析往返
func TestCLIKisoConvertIntegerRoundTrip(t *testing.T) {
	stdout, stderr, exit := runConvertSource(t, `
yobu "kiso/convert";
kaku signed: []int64 = [0, 1, -1, 42, -42, 9223372036854775807, -9223372036854775807 - 1];
kaku signed_index = 0;
aida (signed_index < len(signed)) {
    kaku text = convert.format_int(signed[signed_index]);
    kaku parsed = convert.parse_int(text);
    moshi (parsed != mu) { chant(parsed == signed[signed_index]); } sore { chant(iie); }
    signed_index = signed_index + 1;
}
kaku unsigned: []uint32 = [0, 1, 42, 4294967295];
kaku unsigned_index = 0;
aida (unsigned_index < len(unsigned)) {
    kaku text = convert.format_uint(unsigned[unsigned_index]);
    kaku parsed = convert.parse_uint(text);
    moshi (parsed != mu) { chant(parsed == unsigned[unsigned_index]); } sore { chant(iie); }
    unsigned_index = unsigned_index + 1;
}
`)
	want := strings.Repeat("hai\n", 11)
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want)
	}
}

// 测试布尔文本只接受 Mio 关键字
func TestCLIKisoConvertBool(t *testing.T) {
	stdout, stderr, exit := runConvertSource(t, `
yobu "kiso/convert";
kaku yes = convert.parse_bool("hai");
kaku no = convert.parse_bool("iie");
kaku english_true = convert.parse_bool("true");
kaku english_false = convert.parse_bool("false");
kaku empty = convert.parse_bool("");
moshi (yes != mu) { chant(yes == hai); } sore { chant(iie); }
moshi (no != mu) { chant(no == iie); } sore { chant(iie); }
chant(english_true == mu); chant(english_false == mu); chant(empty == mu);
chant(convert.format_bool(hai)); chant(convert.format_bool(iie));
`)
	want := "hai\nhai\nhai\nhai\nhai\nhai\niie\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want)
	}
}

// 测试 float64 解析与 Go 参考舍入的位模式一致
func TestCLIKisoConvertParseFloat64(t *testing.T) {
	overflowMidpoint := new(big.Int).Lsh(big.NewInt(1), 1024)
	overflowMidpoint.Sub(overflowMidpoint, new(big.Int).Lsh(big.NewInt(1), 970))
	inputs := []string{
		"0", "-0", "+0", "1", "-1.5", ".5", "5.", "1e3", "1E-3",
		"5e-324", "2.4703282292062327e-324", "2.4703282292062328e-324",
		exactBinaryFraction(1, -1075), exactBinaryFraction(3, -1075),
		"2.2250738585072014e-308", "1.7976931348623157e308", "1.7976931348623159e308",
		overflowMidpoint.String(),
		"9007199254740993", "1.00000000000000011102230246251565404236316680908203125",
		"NaN", "Infinity", "+Infinity", "-Infinity",
		"", ".", "1e", "1e+", " 1", "1 ", "1_0", "１２", "nan", "inf",
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	for index, input := range inputs {
		fmt.Fprintf(&source, "kaku value%d = convert.parse_float64(%s); moshi (value%d == mu) { chant(\"mu\"); } sore { chant(convert.format_float64(value%d)); }\n", index, mioStringLiteral(input), index, index)
	}
	lines := strings.Split(strings.TrimSuffix(runSourceVM(t, source.String(), 200_000_000), "\n"), "\n")
	if len(lines) != len(inputs) {
		t.Fatalf("got %d lines, want %d", len(lines), len(inputs))
	}
	for index, input := range inputs {
		if input == "1_0" || input == "nan" || input == "inf" {
			if lines[index] != "mu" {
				t.Fatalf("parse %q = %q, want mu", input, lines[index])
			}
			continue
		}
		if input == "NaN" {
			if lines[index] != "NaN" {
				t.Fatalf("parse %q = %q", input, lines[index])
			}
			continue
		}
		if input == "Infinity" || input == "+Infinity" || input == "-Infinity" {
			want := strings.TrimPrefix(input, "+")
			if lines[index] != want {
				t.Fatalf("parse %q = %q, want %q", input, lines[index], want)
			}
			continue
		}
		want, err := strconv.ParseFloat(input, 64)
		if err != nil && !errorsIsRange(err) {
			if lines[index] != "mu" {
				t.Fatalf("parse %q = %q, want mu", input, lines[index])
			}
			continue
		}
		if math.IsInf(want, 0) {
			if lines[index] != "mu" {
				t.Fatalf("overflow %q = %q, want mu", input, lines[index])
			}
			continue
		}
		got, err := strconv.ParseFloat(lines[index], 64)
		if err != nil || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("parse %q formatted %q bits=%016x, want %016x", input, lines[index], math.Float64bits(got), math.Float64bits(want))
		}
	}
}

// 测试 float32 解析直接舍入到 binary32
func TestCLIKisoConvertParseFloat32(t *testing.T) {
	direct, _ := strconv.ParseFloat("1.0000000596046448", 32)
	via64, _ := strconv.ParseFloat("1.0000000596046448", 64)
	if math.Float32bits(float32(direct)) == math.Float32bits(float32(via64)) {
		t.Fatal("double-rounding regression input no longer distinguishes direct binary32 rounding")
	}
	inputs := []string{
		"0", "-0", "0.1", "1.401298464324817e-45", "7.006492321624085e-46",
		"1.1754943508222875e-38", "3.4028234663852886e38", "3.4028235677973366e38",
		"16777217", "1.0000000596046448", "1.0000000596046447753906250000000000001",
		"NaN", "Infinity", "-Infinity", "1e", " 1", "1,0",
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	for index, input := range inputs {
		fmt.Fprintf(&source, "kaku value%d = convert.parse_float32(%s); moshi (value%d == mu) { chant(\"mu\"); } sore { chant(convert.format_float32(value%d)); }\n", index, mioStringLiteral(input), index, index)
	}
	lines := strings.Split(strings.TrimSuffix(runSourceVM(t, source.String(), 150_000_000), "\n"), "\n")
	for index, input := range inputs {
		if input == "NaN" || input == "Infinity" || input == "-Infinity" {
			if lines[index] != input {
				t.Fatalf("parse %q = %q", input, lines[index])
			}
			continue
		}
		want64, err := strconv.ParseFloat(input, 32)
		if err != nil && !errorsIsRange(err) {
			if lines[index] != "mu" {
				t.Fatalf("parse %q = %q, want mu", input, lines[index])
			}
			continue
		}
		want := float32(want64)
		if math.IsInf(float64(want), 0) {
			if lines[index] != "mu" {
				t.Fatalf("overflow %q = %q, want mu", input, lines[index])
			}
			continue
		}
		got64, err := strconv.ParseFloat(lines[index], 32)
		got := float32(got64)
		if err != nil || math.Float32bits(got) != math.Float32bits(want) {
			t.Fatalf("parse %q formatted %q bits=%08x, want %08x", input, lines[index], math.Float32bits(got), math.Float32bits(want))
		}
	}
}

// 测试浮点解析结果的 IEEE 754 位模式
func TestCLIKisoConvertParseFloatBits(t *testing.T) {
	tests64 := []string{
		"0", "-0", "5e-324", "2.225073858507201e-308", "2.2250738585072014e-308",
		"1.7976931348623157e308", "9007199254740993",
		"1.00000000000000011102230246251565404236316680908203125",
		"1.00000000000000033306690738754696212708950042724609375",
	}
	for _, input := range tests64 {
		input := input
		t.Run("float64/"+input, func(t *testing.T) {
			result := runConvertValue(t, fmt.Sprintf(`yobu "kiso/convert"; convert.parse_float64(%s);`, mioStringLiteral(input)), 50_000_000)
			got, ok := result.Float64()
			if !ok {
				t.Fatalf("parse %q returned %s", input, result.Kind())
			}
			want, err := strconv.ParseFloat(input, 64)
			if err != nil {
				t.Fatal(err)
			}
			if math.Float64bits(got) != math.Float64bits(want) {
				t.Fatalf("parse %q bits=%016x, want %016x", input, math.Float64bits(got), math.Float64bits(want))
			}
		})
	}

	tests32 := []string{
		"0", "-0", "1.401298464324817e-45", "1.1754942106924411e-38", "1.1754943508222875e-38",
		"3.4028234663852886e38", "16777217", "1.0000000596046448",
		"1.0000000596046447753906250000000000001",
	}
	for _, input := range tests32 {
		input := input
		t.Run("float32/"+input, func(t *testing.T) {
			result := runConvertValue(t, fmt.Sprintf(`yobu "kiso/convert"; convert.parse_float32(%s);`, mioStringLiteral(input)), 20_000_000)
			got, ok := result.Float32()
			if !ok {
				t.Fatalf("parse %q returned %s", input, result.Kind())
			}
			want64, err := strconv.ParseFloat(input, 32)
			if err != nil {
				t.Fatal(err)
			}
			want := float32(want64)
			if math.Float32bits(got) != math.Float32bits(want) {
				t.Fatalf("parse %q bits=%08x, want %08x", input, math.Float32bits(got), math.Float32bits(want))
			}
		})
	}
}

func errorsIsRange(err error) bool {
	numberError, ok := err.(*strconv.NumError)
	return ok && numberError.Err == strconv.ErrRange
}

func decimalSignificantDigits(text string) int {
	if strings.HasPrefix(text, "-") || strings.HasPrefix(text, "+") {
		text = text[1:]
	}
	if exponent := strings.IndexAny(text, "eE"); exponent >= 0 {
		text = text[:exponent]
	}
	text = strings.ReplaceAll(text, ".", "")
	text = strings.TrimLeft(text, "0")
	text = strings.TrimRight(text, "0")
	if text == "" {
		return 1
	}
	return len(text)
}

// 测试已知边界的最短浮点格式化
func TestCLIKisoConvertFloatFormattingRegressions(t *testing.T) {
	values64, values32 := floatFormattingRegressionValues()
	checkFloatFormatting(t, values64, values32, 100_000_000)
}

// floatFormattingRegressionValues 返回已知格式化边界
func floatFormattingRegressionValues() ([]float64, []float32) {
	values64 := []float64{
		math.SmallestNonzeroFloat64,
		math.Float64frombits(2),
		math.Float64frombits(0x000fffffffffffff),
		math.Float64frombits(0x0010000000000000),
		math.Float64frombits(0x3fefffffffffffff),
		1,
		math.Float64frombits(0x3ff0000000000001),
		math.MaxFloat64,
	}
	values32 := []float32{
		math.SmallestNonzeroFloat32,
		math.Float32frombits(2),
		math.Float32frombits(0x007fffff),
		math.Float32frombits(0x00800000),
		math.Float32frombits(0x3f7fffff),
		1,
		math.Float32frombits(0x3f800001),
		math.MaxFloat32,
	}
	return values64, values32
}

// 测试随机位模式的最短往返格式化
func TestCLIKisoConvertRandomFloatFormatting(t *testing.T) {
	skipInShortMode(t, "跳过随机浮点格式化验证")
	random := rand.New(rand.NewSource(42))
	regressions64, regressions32 := floatFormattingRegressionValues()
	values64 := make([]float64, 0, 504)
	values32 := make([]float32, 0, 504)
	for exponent := -1074; exponent <= 1023; exponent += 64 {
		value := math.Ldexp(1, exponent)
		if !math.IsInf(value, 0) && value != 0 {
			values64 = append(values64, value)
		}
	}
	for exponent := -149; exponent <= 127; exponent += 8 {
		value := float32(math.Ldexp(1, exponent))
		if !math.IsInf(float64(value), 0) && value != 0 {
			values32 = append(values32, value)
		}
	}
	sampleCount := 192
	if stressTestEnabled() {
		sampleCount = 512
	}
	for len(values64) < sampleCount-len(regressions64) {
		value := math.Float64frombits(random.Uint64())
		if math.IsNaN(value) || math.IsInf(value, 0) || value == 0 {
			continue
		}
		values64 = append(values64, value)
	}
	for len(values32) < sampleCount-len(regressions32) {
		value := math.Float32frombits(random.Uint32())
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) || value == 0 {
			continue
		}
		values32 = append(values32, value)
	}
	checkFloatFormatting(t, values64, values32, 2_500_000_000)
}

// checkFloatFormatting 批量验证最短往返格式化
func checkFloatFormatting(t *testing.T, values64 []float64, values32 []float32, instructionLimit uint64) {
	t.Helper()
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	for _, value := range values64 {
		fmt.Fprintf(&source, "chant(convert.format_float64(%s));\n", mioFloatLiteral(strconv.FormatFloat(value, 'g', -1, 64)))
	}
	for index, value := range values32 {
		fmt.Fprintf(&source, "kaku value32_%d: float32 = %s; chant(convert.format_float32(value32_%d));\n", index, mioFloatLiteral(strconv.FormatFloat(float64(value), 'g', -1, 32)), index)
	}
	lines := strings.Split(strings.TrimSuffix(runSourceVM(t, source.String(), instructionLimit), "\n"), "\n")
	for index, want := range values64 {
		got, err := strconv.ParseFloat(lines[index], 64)
		if err != nil || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("float64 bits=%016x formatted %q", math.Float64bits(want), lines[index])
		}
		reference := strconv.FormatFloat(want, 'g', -1, 64)
		if decimalSignificantDigits(lines[index]) != decimalSignificantDigits(reference) {
			t.Fatalf("float64 %x digits=%d, reference %q digits=%d", math.Float64bits(want), decimalSignificantDigits(lines[index]), reference, decimalSignificantDigits(reference))
		}
	}
	for index, want := range values32 {
		line := lines[len(values64)+index]
		got64, err := strconv.ParseFloat(line, 32)
		got := float32(got64)
		if err != nil || math.Float32bits(got) != math.Float32bits(want) {
			t.Fatalf("float32 bits=%08x formatted %q", math.Float32bits(want), line)
		}
		reference := strconv.FormatFloat(float64(want), 'g', -1, 32)
		if decimalSignificantDigits(line) != decimalSignificantDigits(reference) {
			t.Fatalf("float32 %x digits=%d, reference %q digits=%d", math.Float32bits(want), decimalSignificantDigits(line), reference, decimalSignificantDigits(reference))
		}
	}
}

// 测试随机十进制输入与 Go 参考解析一致
func TestCLIKisoConvertRandomFloatParsing(t *testing.T) {
	skipInShortMode(t, "跳过随机浮点解析验证")
	random := rand.New(rand.NewSource(84))
	sampleCount := 128
	if stressTestEnabled() {
		sampleCount = 384
	}
	inputs64 := make([]string, sampleCount)
	inputs32 := make([]string, sampleCount)
	for index := range inputs64 {
		coefficient := random.Uint64()%9_000_000_000_000_000_000 + 1
		exponent := random.Intn(701) - 350
		inputs64[index] = fmt.Sprintf("%de%+d", coefficient, exponent)
	}
	for index := range inputs32 {
		coefficient := random.Uint64()%9_000_000_000_000_000_000 + 1
		exponent := random.Intn(121) - 60
		inputs32[index] = fmt.Sprintf("%de%+d", coefficient, exponent)
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/convert\";\n")
	for index, input := range inputs64 {
		fmt.Fprintf(&source, "kaku parsed64_%d = convert.parse_float64(%s); moshi (parsed64_%d == mu) { chant(\"mu\"); } sore { chant(convert.format_float64(parsed64_%d)); }\n", index, mioStringLiteral(input), index, index)
	}
	for index, input := range inputs32 {
		fmt.Fprintf(&source, "kaku parsed32_%d = convert.parse_float32(%s); moshi (parsed32_%d == mu) { chant(\"mu\"); } sore { chant(convert.format_float32(parsed32_%d)); }\n", index, mioStringLiteral(input), index, index)
	}
	instructionLimit := uint64(1_500_000_000)
	if stressTestEnabled() {
		instructionLimit = 5_000_000_000
	}
	lines := strings.Split(strings.TrimSuffix(runSourceVM(t, source.String(), instructionLimit), "\n"), "\n")
	for index, input := range inputs64 {
		want, _ := strconv.ParseFloat(input, 64)
		if math.IsInf(want, 0) {
			if lines[index] != "mu" {
				t.Fatalf("float64 overflow %q = %q", input, lines[index])
			}
			continue
		}
		got, err := strconv.ParseFloat(lines[index], 64)
		if err != nil || math.Float64bits(got) != math.Float64bits(want) {
			t.Fatalf("float64 parse %q formatted %q bits=%016x, want %016x", input, lines[index], math.Float64bits(got), math.Float64bits(want))
		}
	}
	for index, input := range inputs32 {
		want64, _ := strconv.ParseFloat(input, 32)
		want := float32(want64)
		line := lines[len(inputs64)+index]
		if math.IsInf(float64(want), 0) {
			if line != "mu" {
				t.Fatalf("float32 overflow %q = %q", input, line)
			}
			continue
		}
		got64, err := strconv.ParseFloat(line, 32)
		got := float32(got64)
		if err != nil || math.Float32bits(got) != math.Float32bits(want) {
			t.Fatalf("float32 parse %q formatted %q bits=%08x, want %08x", input, line, math.Float32bits(got), math.Float32bits(want))
		}
	}
}

// 测试超长有效十进制输入和资源边界
func TestCLIKisoConvertLongFloatInput(t *testing.T) {
	skipInShortMode(t, "跳过超长浮点输入验证")
	nearOne := "1." + strings.Repeat("0", 4096) + "1"
	cancelledExponent := "1" + strings.Repeat("0", 9000) + "e-9000"
	tooLong := "1." + strings.Repeat("0", 10000) + "1"
	source := fmt.Sprintf(`
yobu "kiso/convert";
kaku near = convert.parse_float64(%s);
kaku cancelled = convert.parse_float64(%s);
kaku limited = convert.parse_float64(%s);
moshi (near != mu) { chant(convert.format_float64(near)); } sore { chant("mu"); }
moshi (cancelled != mu) { chant(convert.format_float64(cancelled)); } sore { chant("mu"); }
chant(limited == mu);
`, mioStringLiteral(nearOne), mioStringLiteral(cancelledExponent), mioStringLiteral(tooLong))
	got := runSourceVM(t, source, 1_500_000_000)
	if got != "1\n1\nhai\n" {
		t.Fatalf("output=%q", got)
	}
}

// 测试 convert 私有辅助函数不能跨包访问
func TestCLIKisoConvertAccessControl(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, `yobu "kiso/convert"; convert.parse_negative([49], 0, -9);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"check", path}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), "does not export") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
}

// 测试 convert 的 run、build、exec、disasm 和按需链接
func TestCLIKisoConvertMochiCommands(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/convert"; kaku value = convert.parse_int("-42"); moshi (value != mu) { chant(convert.format_int(value)); } chant(convert.format_bool(hai)); kaku floating = convert.parse_float32("0.1"); moshi (floating != mu) { chant(convert.format_float32(floating)); }`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &stdout, &stderr); exit != 0 || stdout.String() != "-42\nhai\n0.1\n" {
		t.Fatalf("run exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	artifact, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(artifact[5:7]); version != mochi.VersionV14 {
		t.Fatalf("Mochi version=%d", version)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "-42\nhai\n0.1\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 ||
		!strings.Contains(stdout.String(), "kiso/convert::parse_int") ||
		!strings.Contains(stdout.String(), "kiso/convert::format_int") ||
		!strings.Contains(stdout.String(), "kiso/convert::format_bool") ||
		!strings.Contains(stdout.String(), "kiso/convert::parse_float32") ||
		!strings.Contains(stdout.String(), "kiso/convert::format_float32") ||
		strings.Contains(stdout.String(), "kiso/convert::parse_uint") ||
		strings.Contains(stdout.String(), "kiso/convert::format_uint") ||
		strings.Contains(stdout.String(), "kiso/convert::parse_bool") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测试未使用 convert 不增加 Mochi 体积
func TestCLIKisoConvertUnusedPackageHasNoMochiCost(t *testing.T) {
	root := t.TempDir()
	plainPath := filepath.Join(root, "plain.mio")
	importedPath := filepath.Join(root, "imported.mio")
	plainMochi := filepath.Join(root, "plain.mochi")
	importedMochi := filepath.Join(root, "imported.mochi")
	writeInfrastructureSource(t, plainPath, `chant(42);`)
	writeInfrastructureSource(t, importedPath, `yobu "kiso/convert"; chant(42);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	for _, pair := range [][2]string{{plainPath, plainMochi}, {importedPath, importedMochi}} {
		var stdout, stderr bytes.Buffer
		if exit := runCLI([]string{"build", "-o", pair[1], pair[0]}, &stdout, &stderr); exit != 0 {
			t.Fatalf("build %s exit=%d stderr=%s", pair[0], exit, stderr.String())
		}
	}
	plain, err := os.ReadFile(plainMochi)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := os.ReadFile(importedMochi)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, imported) {
		t.Fatalf("plain=%d imported-unused=%d", len(plain), len(imported))
	}
	t.Logf("plain=%d bytes imported-unused=%d bytes", len(plain), len(imported))
}

func benchmarkConvertProgram(b *testing.B, source string) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, source)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		b.Fatal(err)
	}
	machine := vm.NewWithOutput(bytecode, io.Discard)
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, bytecode); err != nil {
		b.Fatal(err)
	}
	mochiBytes := encoded.Len()
	bytecodeBytes := instructionBytes(bytecode)
	instructions := measureUTF8Instructions(b, bytecode, 100_000_000)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: 100_000_000}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(mochiBytes), "mochi-B")
	b.ReportMetric(float64(bytecodeBytes), "bytecode-B")
	b.ReportMetric(float64(instructions), "instructions/op")
}

// BenchmarkKisoConvertExecute 测量转换算法的完整 VM 执行成本
func BenchmarkKisoConvertExecute(b *testing.B) {
	tests := []struct {
		name   string
		source string
	}{
		{"parse-positive", `yobu "kiso/convert"; convert.parse_int("123456789");`},
		{"parse-negative", `yobu "kiso/convert"; convert.parse_int("-123456789");`},
		{"parse-max-int64", `yobu "kiso/convert"; convert.parse_int("9223372036854775807");`},
		{"parse-min-int64", `yobu "kiso/convert"; convert.parse_int("-9223372036854775808");`},
		{"parse-max-uint32", `yobu "kiso/convert"; convert.parse_uint("4294967295");`},
		{"parse-failure", `yobu "kiso/convert"; convert.parse_int("9223372036854775808");`},
		{"format-int", `yobu "kiso/convert"; convert.format_int(-9223372036854775807 - 1);`},
		{"format-uint", `yobu "kiso/convert"; convert.format_uint(uint32(4294967295));`},
		{"format-bool", `yobu "kiso/convert"; convert.format_bool(hai);`},
		{"parse-float32", `yobu "kiso/convert"; convert.parse_float32("123.456");`},
		{"parse-float64", `yobu "kiso/convert"; convert.parse_float64("123.456");`},
		{"parse-scientific", `yobu "kiso/convert"; convert.parse_float64("1.2345678901234567e-200");`},
		{"parse-subnormal", `yobu "kiso/convert"; convert.parse_float64("5e-324");`},
		{"parse-max-finite", `yobu "kiso/convert"; convert.parse_float64("1.7976931348623157e308");`},
		{"parse-float-failure", `yobu "kiso/convert"; convert.parse_float64("1.7976931348623159e308");`},
		{"format-float32", `yobu "kiso/convert"; convert.format_float32(float32(0.1));`},
		{"format-float64", `yobu "kiso/convert"; convert.format_float64(0.1);`},
		{"format-max-finite", `yobu "kiso/convert"; convert.format_float64(1.7976931348623157e308);`},
		{"format-batch", `yobu "kiso/convert"; kaku values: []float64 = [0.1, 1.25, 1e-200, 1.7976931348623157e308]; kaku index = 0; aida (index < len(values)) { convert.format_float64(values[index]); index = index + 1; }`},
		{"random-batch-64", `yobu "kiso/convert"; kaku values: []float64 = [3.141592653589793, 2.718281828459045, 6.02214076e23, 1.2345678901234567e-200]; kaku repeat = 0; aida (repeat < 16) { kaku index = 0; aida (index < len(values)) { kaku text = convert.format_float64(values[index]); convert.parse_float64(text); index = index + 1; } repeat = repeat + 1; }`},
		{"repeat-1000", `yobu "kiso/convert"; kaku index = 0; aida (index < 1000) { convert.parse_int("-123456789"); convert.format_int(-123456789); index = index + 1; }`},
	}
	for _, test := range tests {
		b.Run(test.name, func(b *testing.B) { benchmarkConvertProgram(b, test.source) })
	}
}

// BenchmarkKisoConvertRun 测量解析、编译和执行的完整 CLI 路径
func BenchmarkKisoConvertRun(b *testing.B) {
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, `yobu "kiso/convert"; convert.parse_int("-9223372036854775808"); convert.format_uint(uint32(4294967295)); convert.format_bool(hai);`)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		var stdout, stderr bytes.Buffer
		if exit := runCLI([]string{"run", path}, &stdout, &stderr); exit != 0 {
			b.Fatalf("exit=%d stderr=%s", exit, stderr.String())
		}
	}
}

// BenchmarkKisoConvertStrconvReference 测量 Go 参考算法的直接成本
func BenchmarkKisoConvertStrconvReference(b *testing.B) {
	b.Run("parse-int64", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			_, _ = strconv.ParseInt("-9223372036854775808", 10, 64)
		}
	})
	b.Run("format-int64", func(b *testing.B) {
		b.ReportAllocs()
		for index := 0; index < b.N; index++ {
			_ = strconv.FormatInt(-9223372036854775808, 10)
		}
	})
}
