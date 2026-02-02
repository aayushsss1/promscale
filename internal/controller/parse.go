package controller

import (
	"fmt"
	"strconv"
)

func clampInt32(x, lo, hi int32) int32 {

	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}

	return x
}

func parseStringToFloat(s string, fieldName string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse %s value '%s' to float: %v", fieldName, s, err)
	}
	return f, nil
}
