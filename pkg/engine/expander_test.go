package engine

import (
	"testing"
)

func TestIsWildcard(t *testing.T) {
	tests := []struct {
		address  string
		expected bool
	}{
		{"aws_instance.web", false},
		{`aws_s3_bucket.data["key"]`, false},
		{"aws_s3_bucket.data[*]", true},
		{"module.vpc.aws_subnet.public[*]", true},
		{"aws_instance.web[0]", false},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			result := IsWildcard(tt.address)
			if result != tt.expected {
				t.Errorf("IsWildcard(%q) = %v, want %v", tt.address, result, tt.expected)
			}
		})
	}
}

func TestBaseAddress(t *testing.T) {
	tests := []struct {
		address  string
		expected string
	}{
		{"aws_s3_bucket.data[*]", "aws_s3_bucket.data"},
		{"aws_instance.web", "aws_instance.web"},
		{"module.vpc.aws_subnet.public[*]", "module.vpc.aws_subnet.public"},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			result := BaseAddress(tt.address)
			if result != tt.expected {
				t.Errorf("BaseAddress(%q) = %q, want %q", tt.address, result, tt.expected)
			}
		})
	}
}
