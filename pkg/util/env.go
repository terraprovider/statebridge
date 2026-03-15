package util

import (
	"fmt"
	"os"
	"strconv"
)

// SupportedEnvTypes constrains the generic type parameter to types that
// can be parsed from environment variable string values.
type SupportedEnvTypes interface {
	int | int8 | int16 | int32 | int64 |
		uint | uint8 | uint16 | uint32 | uint64 |
		float32 | float64 |
		string | bool
}

func Getenv[T SupportedEnvTypes](envVar string) *T {
	if val, ok := os.LookupEnv(envVar); ok {
		parsed, err := Parse[T](val)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: invalid value for %s: %v\n", envVar, err)
			return nil
		}
		return &parsed
	}
	return nil
}

func GetMultienv[T SupportedEnvTypes](envVars ...string) *T {
	for _, envVar := range envVars {
		if val, ok := os.LookupEnv(envVar); ok {
			parsed, err := Parse[T](val)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: invalid value for %s: %v\n", envVar, err)
				continue
			}
			return &parsed
		}
	}
	return nil
}

func Parse[T SupportedEnvTypes](value string) (def T, err error) {
	var result any
	switch any(def).(type) {
	case string:
		result = value
	case bool:
		result, err = strconv.ParseBool(value)
	case float32:
		var temp float64
		temp, err = strconv.ParseFloat(value, 32)
		result = float32(temp)
	case float64:
		result, err = strconv.ParseFloat(value, 64)
	case int:
		var temp int64
		temp, err = strconv.ParseInt(value, 10, 0)
		result = int(temp)
	case int8:
		var temp int64
		temp, err = strconv.ParseInt(value, 10, 8)
		result = int8(temp)
	case int16:
		var temp int64
		temp, err = strconv.ParseInt(value, 10, 16)
		result = int16(temp)
	case int32:
		var temp int64
		temp, err = strconv.ParseInt(value, 10, 32)
		result = int32(temp)
	case int64:
		var temp int64
		temp, err = strconv.ParseInt(value, 10, 64)
		result = int64(temp)
	case uint:
		var temp uint64
		temp, err = strconv.ParseUint(value, 10, 0)
		result = uint(temp)
	case uint8:
		var temp uint64
		temp, err = strconv.ParseUint(value, 10, 8)
		result = uint8(temp)
	case uint16:
		var temp uint64
		temp, err = strconv.ParseUint(value, 10, 16)
		result = uint16(temp)
	case uint32:
		var temp uint64
		temp, err = strconv.ParseUint(value, 10, 32)
		result = uint32(temp)
	case uint64:
		var temp uint64
		temp, err = strconv.ParseUint(value, 10, 64)
		result = uint64(temp)
	default:
		return def, fmt.Errorf("unsupported type %T", def)
	}
	return result.(T), err
}
