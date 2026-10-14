package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/mochi"
)

// 测试 rune 贯穿复合类型、泛型、闭包与 Mochi
func TestCLIRuneIntegration(t *testing.T) {
	source := `
teisu LETTER: rune = '\x41';
kata Box<T> { value: T, }
waza twice<T: Integer>(value: T) -> T { kaesu value + value; }
waza pass(value: rune) -> rune { kaesu value; }
waza capture(value: rune) -> () -> rune {
    kaesu waza () -> rune { kaesu value; };
}
kaku ascii = 'A';
kaku chinese: rune = '你';
kaku japanese: rune = '日';
kaku emoji: rune = '🍡';
kaku escapes: []rune = ['\n', '\t', '\\', '\'', '\x41', '\u4F60', '\U0001F361'];
kaku box: Box<rune> = Box[rune]{ value: japanese, };
kaku table: Map<rune, rune> = Map();
table['A'] = chinese;
kaku found = table['A'];
chant(ascii); chant(chinese); chant(japanese); chant(emoji);
chant(escapes[0]); chant(escapes[1]); chant(escapes[2]); chant(escapes[3]);
chant(escapes[4]); chant(escapes[5]); chant(escapes[6]);
chant(box.value); chant(pass(LETTER)); chant(twice[rune]('A'));
kaku get = capture('A'); chant(get());
moshi (found != mu) { chant(found); }
`
	exit, runOutput, runError := runSourceCLI(t, "run", source)
	if exit != 0 || runError != "" {
		t.Fatalf("run exit=%d stderr=%q", exit, runError)
	}
	want := "65\n20320\n26085\n127841\n10\n9\n92\n39\n65\n20320\n127841\n26085\n65\n130\n65\n20320\n"
	if runOutput != want {
		t.Fatalf("output=%q, want %q", runOutput, want)
	}

	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "rune.mio", source)
	mochiPath := filepath.Join(directory, "rune"+mochi.Extension)
	if code, stdout, stderr := runFileCLI("build", sourcePath); code != 0 || stdout != "" || stderr != "" {
		t.Fatalf("build exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, stdout, stderr := runFileCLI("exec", mochiPath); code != 0 || stdout != want || stderr != "" {
		t.Fatalf("exec exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if code, stdout, stderr := runFileCLI("disasm", mochiPath); code != 0 || stderr != "" || !strings.Contains(stdout, "int32 65") {
		t.Fatalf("disasm exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

// 测试 rune 与 string 保持强类型区分
func TestCLIRuneTypeDistinction(t *testing.T) {
	if exit, _, stderr := runSourceCLI(t, "check", `kaku value: uint8 = 'A';`); exit != 0 {
		t.Fatalf("范围内字符常量 exit=%d stderr=%q", exit, stderr)
	}
	for _, source := range []string{
		`kaku value: string = 'A';`,
		`kaku value: rune = "A";`,
		`'A' == "A";`,
		`kaku value: uint8 = '你';`,
	} {
		exit, _, stderr := runSourceCLI(t, "check", source)
		if exit == 0 || stderr == "" {
			t.Fatalf("source=%q exit=%d stderr=%q", source, exit, stderr)
		}
	}
}

// 测试跨包 rune 常量
func TestCLIRunePackageConstant(t *testing.T) {
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	writeInfrastructureSource(t, filepath.Join(bunkoRoot, "sample", "rune.mio"), `
koukai teisu marker: rune = '🍡';
koukai waza identity(value: rune) -> rune { kaesu value; }
`)
	mainPath := filepath.Join(root, "main.mio")
	writeInfrastructureSource(t, mainPath, `yobu "sample"; chant(sample.identity(sample.marker));`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", mainPath}, &stdout, &stderr); exit != 0 || stdout.String() != "127841\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

// 测试 rune 不改变 Mochi 文件版本
func TestCLIRuneMochiVersion(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "version.mio", `'A';`)
	if code, _, stderr := runFileCLI("build", sourcePath); code != 0 {
		t.Fatalf("build exit=%d stderr=%q", code, stderr)
	}
	data, err := os.ReadFile(filepath.Join(directory, "version"+mochi.Extension))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) < 7 {
		t.Fatalf("Mochi 文件过短: %d", len(data))
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV11 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV11)
	}
}
