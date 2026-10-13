package recognizers

// digitValues appends the decimal digits of s to dst as integers, ignoring any
// other bytes, and returns the extended slice. Validators run once per regex
// match, so callers pass a stack buffer (`var buf [n]int; digitValues(buf[:0],
// s)`) sized for the identifier to keep the hot path allocation-free.
func digitValues(dst []int, s string) []int {
	for i := range len(s) {
		if c := s[i]; c >= '0' && c <= '9' {
			dst = append(dst, int(c-'0'))
		}
	}
	return dst
}

// digitsExactly appends the decimal digits of s to dst and reports whether
// there are exactly n of them. It is the common guard for fixed-length
// checksums; dst needs capacity for n digits and is never grown past that.
func digitsExactly(dst []int, s string, n int) ([]int, bool) {
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			continue
		}
		if len(dst) == n {
			return nil, false
		}
		dst = append(dst, int(c-'0'))
	}
	if len(dst) != n {
		return nil, false
	}
	return dst, true
}

// digitsInt parses s as an unsigned decimal integer. Unlike strconv.Atoi it
// accepts no sign, rejects the empty string and allocates no error value.
func digitsInt(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// Verhoeff multiplication (d) and permutation (p) tables.
var verhoeffD = [10][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	{1, 2, 3, 4, 0, 6, 7, 8, 9, 5},
	{2, 3, 4, 0, 1, 7, 8, 9, 5, 6},
	{3, 4, 0, 1, 2, 8, 9, 5, 6, 7},
	{4, 0, 1, 2, 3, 9, 5, 6, 7, 8},
	{5, 9, 8, 7, 6, 0, 4, 3, 2, 1},
	{6, 5, 9, 8, 7, 1, 0, 4, 3, 2},
	{7, 6, 5, 9, 8, 2, 1, 0, 4, 3},
	{8, 7, 6, 5, 9, 3, 2, 1, 0, 4},
	{9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
}

var verhoeffP = [8][10]int{
	{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
	{1, 5, 7, 6, 2, 8, 3, 0, 9, 4},
	{5, 8, 0, 3, 7, 9, 6, 1, 4, 2},
	{8, 9, 1, 6, 0, 4, 3, 5, 2, 7},
	{9, 4, 5, 3, 1, 2, 6, 8, 7, 0},
	{4, 2, 8, 6, 5, 7, 3, 9, 0, 1},
	{2, 7, 9, 3, 8, 0, 6, 4, 1, 5},
	{7, 0, 4, 6, 9, 1, 3, 2, 5, 8},
}

// verhoeffValid runs the Verhoeff checksum over the decimal digits of s
// (the check digit must be included). It returns true when the checksum is 0.
func verhoeffValid(s string) bool {
	var buf [32]int
	return verhoeffDigits(digitValues(buf[:0], s))
}

// verhoeffDigits is verhoeffValid over already extracted digits.
func verhoeffDigits(ds []int) bool {
	c := 0
	for i, idx := 0, len(ds)-1; idx >= 0; i, idx = i+1, idx-1 {
		c = verhoeffD[c][verhoeffP[i%8][ds[idx]]]
	}
	return c == 0
}

// luhnValid runs the Luhn (mod-10) checksum over the decimal digits of s,
// ignoring any separators between them.
func luhnValid(s string) bool {
	var buf [32]int
	ds := digitValues(buf[:0], s)
	if len(ds) == 0 {
		return false
	}
	sum := 0
	alt := false
	for i := len(ds) - 1; i >= 0; i-- {
		n := ds[i]
		if alt {
			if n *= 2; n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}
