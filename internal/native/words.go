package native

import "fmt"

// plural is one when n is 1, else many.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// countWord is "1 host" / "2 hosts".
func countWord(n int, one, many string) string {
	return fmt.Sprintf("%d %s", n, plural(n, one, many))
}
