package value

import (
	"fmt"
	"io"
	"strconv"
)

// WriteLine 按 Mio 的显示规则输出一个基础值，并追加换行；不改变值的类型。
func WriteLine(out io.Writer, v Value) error {
	var text string
	switch v.Kind() {
	case KindInt:
		number, _ := v.Int64()
		text = strconv.FormatInt(number, 10)
	case KindInt8, KindInt16, KindInt32:
		number, _ := v.SignedInteger()
		text = strconv.FormatInt(number, 10)
	case KindUint8, KindUint16, KindUint32:
		number, _ := v.UnsignedInteger()
		text = strconv.FormatUint(number, 10)
	case KindFloat:
		number, _ := v.Float64()
		text = strconv.FormatFloat(number, 'g', -1, 64)
	case KindFloat32:
		number, _ := v.Float32()
		text = strconv.FormatFloat(float64(number), 'g', -1, 32)
	case KindBool:
		boolean, _ := v.Bool()
		text = "iie"
		if boolean {
			text = "hai"
		}
	case KindString:
		text, _ = v.Text()
	case KindNil:
		text = "mu"
	default:
		return fmt.Errorf("cannot display runtime value of type %s", v.Kind())
	}
	if out == nil {
		return fmt.Errorf("missing output writer")
	}
	line := text + "\n"
	n, err := io.WriteString(out, line)
	if err == nil && n != len(line) {
		return io.ErrShortWrite
	}
	return err
}
