package controller

func clampInt32(x, lo, hi int32) int32 {

	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}

	return x
}
