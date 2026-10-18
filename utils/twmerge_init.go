package utils

import twmerge "github.com/Oudwins/tailwind-merge-go"

// tailwind-merge-go (v0.2.2) builds twmerge.Merge lazily: the first call
// assigns its config, LRU cache and merge function to closure variables
// without synchronization, so the first concurrent calls of CN (parallel
// templ renders) race (#62). The LRU itself is mutex-guarded and the merge
// is stateless once built, so building it here, during package
// initialization, is enough: package init happens before any goroutine that
// can call CN, which orders every later read after these writes. Merging ""
// runs the full build and returns early.
//
// This lives outside shadcn-templ.go on purpose, so regenerating that file
// with the shadcn-templ CLI keeps the fix. Delete it once the library
// initializes Merge eagerly or under a sync.Once.
func init() {
	twmerge.Merge("")
}
