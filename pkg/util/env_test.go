package util

import (
	"testing"
)

func TestGetenv_String(t *testing.T) {
	t.Setenv("TEST_STR", "hello")
	got := Getenv[string]("TEST_STR")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != "hello" {
		t.Errorf("expected hello, got %s", *got)
	}
}

func TestGetenv_StringMissing(t *testing.T) {
	got := Getenv[string]("TEST_MISSING_VAR")
	if got != nil {
		t.Errorf("expected nil for missing var, got %v", *got)
	}
}

func TestGetenv_Bool_True(t *testing.T) {
	t.Setenv("TEST_BOOL", "true")
	got := Getenv[bool]("TEST_BOOL")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != true {
		t.Errorf("expected true, got %v", *got)
	}
}

func TestGetenv_Bool_False(t *testing.T) {
	t.Setenv("TEST_BOOL", "false")
	got := Getenv[bool]("TEST_BOOL")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != false {
		t.Errorf("expected false, got %v", *got)
	}
}

func TestGetenv_Bool_Invalid(t *testing.T) {
	t.Setenv("TEST_BOOL", "notabool")
	got := Getenv[bool]("TEST_BOOL")
	if got != nil {
		t.Errorf("expected nil for invalid bool, got %v", *got)
	}
}

func TestGetenv_Int(t *testing.T) {
	t.Setenv("TEST_INT", "42")
	got := Getenv[int]("TEST_INT")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != 42 {
		t.Errorf("expected 42, got %d", *got)
	}
}

func TestGetenv_IntNegative(t *testing.T) {
	t.Setenv("TEST_INT", "-10")
	got := Getenv[int]("TEST_INT")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != -10 {
		t.Errorf("expected -10, got %d", *got)
	}
}

func TestGetenv_IntInvalid(t *testing.T) {
	t.Setenv("TEST_INT", "notanint")
	got := Getenv[int]("TEST_INT")
	if got != nil {
		t.Errorf("expected nil for invalid int, got %v", *got)
	}
}

func TestGetenv_Float64(t *testing.T) {
	t.Setenv("TEST_FLOAT", "3.14")
	got := Getenv[float64]("TEST_FLOAT")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != 3.14 {
		t.Errorf("expected 3.14, got %f", *got)
	}
}

func TestGetMultienv_FirstMatch(t *testing.T) {
	t.Setenv("PRIMARY", "first")
	t.Setenv("SECONDARY", "second")
	got := GetMultienv[string]("PRIMARY", "SECONDARY")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != "first" {
		t.Errorf("expected first, got %s", *got)
	}
}

func TestGetMultienv_SecondMatch(t *testing.T) {
	t.Setenv("SECONDARY", "second")
	got := GetMultienv[string]("XMISSING", "SECONDARY")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != "second" {
		t.Errorf("expected second, got %s", *got)
	}
}

func TestGetMultienv_NoneSet(t *testing.T) {
	got := GetMultienv[string]("XMISSING1", "XMISSING2")
	if got != nil {
		t.Errorf("expected nil, got %v", *got)
	}
}

func TestGetMultienv_Bool(t *testing.T) {
	t.Setenv("BOOL_VAR", "true")
	got := GetMultienv[bool]("XMISSING", "BOOL_VAR")
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if *got != true {
		t.Errorf("expected true, got %v", *got)
	}
}

func TestParse_String(t *testing.T) {
	got, err := Parse[string]("hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello" {
		t.Errorf("expected hello, got %s", got)
	}
}

func TestParse_Bool(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
		{"TRUE", true},
		{"FALSE", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := Parse[bool](tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("Parse[bool](%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParse_BoolInvalid(t *testing.T) {
	_, err := Parse[bool]("notabool")
	if err == nil {
		t.Error("expected error for invalid bool")
	}
}

func TestParse_Int(t *testing.T) {
	got, err := Parse[int]("123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 123 {
		t.Errorf("expected 123, got %d", got)
	}
}

func TestParse_IntInvalid(t *testing.T) {
	_, err := Parse[int]("abc")
	if err == nil {
		t.Error("expected error for invalid int")
	}
}

func TestParse_Int8(t *testing.T) {
	got, err := Parse[int8]("127")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 127 {
		t.Errorf("expected 127, got %d", got)
	}
}

func TestParse_Int16(t *testing.T) {
	got, err := Parse[int16]("32767")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 32767 {
		t.Errorf("expected 32767, got %d", got)
	}
}

func TestParse_Int32(t *testing.T) {
	got, err := Parse[int32]("2147483647")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 2147483647 {
		t.Errorf("expected 2147483647, got %d", got)
	}
}

func TestParse_Int64(t *testing.T) {
	got, err := Parse[int64]("9223372036854775807")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 9223372036854775807 {
		t.Errorf("expected max int64, got %d", got)
	}
}

func TestParse_Uint(t *testing.T) {
	got, err := Parse[uint]("42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Errorf("expected 42, got %d", got)
	}
}

func TestParse_Uint8(t *testing.T) {
	got, err := Parse[uint8]("255")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 255 {
		t.Errorf("expected 255, got %d", got)
	}
}

func TestParse_Uint8_Overflow(t *testing.T) {
	_, err := Parse[uint8]("256")
	if err == nil {
		t.Error("expected error for uint8 overflow")
	}
}

func TestParse_Uint16(t *testing.T) {
	got, err := Parse[uint16]("65535")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 65535 {
		t.Errorf("expected 65535, got %d", got)
	}
}

func TestParse_Uint32(t *testing.T) {
	got, err := Parse[uint32]("4294967295")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 4294967295 {
		t.Errorf("expected 4294967295, got %d", got)
	}
}

func TestParse_Uint64(t *testing.T) {
	got, err := Parse[uint64]("18446744073709551615")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 18446744073709551615 {
		t.Errorf("expected max uint64, got %d", got)
	}
}

func TestParse_Float32(t *testing.T) {
	got, err := Parse[float32]("1.5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 1.5 {
		t.Errorf("expected 1.5, got %f", got)
	}
}
