package store

import (
	"fmt"
	"strconv"
	"strings"
)

func vectorLiteral(values []float32) string {
	var builder strings.Builder
	builder.Grow(len(values) * 10)
	builder.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(float64(value), 'f', -1, 32))
	}
	builder.WriteByte(']')
	return builder.String()
}

func validateVector(values []float32, dimension int) error {
	if len(values) != dimension {
		return fmt.Errorf("embedding dimension mismatch: got %d, want %d", len(values), dimension)
	}
	return nil
}
