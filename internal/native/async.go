package native

import "context"

// One way to ask keel serve something: do runs off the UI thread under callTimeout, done runs back on it with the
// answer. A result that arrives after the window closed is dropped here, so no site has to remember to check.
// Both go through w.work and w.post, which tests replace to run in line.

// call runs do and hands its answer to done.
func call[T any](w *workspace, do func(context.Context) (T, error), done func(T, error)) {
	callIf(w, nil, do, done)
}

// callIf is call, and drops the answer when wanted says it is stale (the page was left, a newer press went out).
func callIf[T any](w *workspace, wanted func() bool, do func(context.Context) (T, error), done func(T, error)) {
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		v, err := do(c)
		w.post(func() {
			if w.closed || (wanted != nil && !wanted()) {
				return
			}
			done(v, err)
		})
	})
}

// callOnce is callIf with one read in flight: it returns at once while *busy is set, and clears it when the
// answer lands, wanted or not.
func callOnce[T any](w *workspace, busy *bool, wanted func() bool, do func(context.Context) (T, error), done func(T, error)) {
	if *busy {
		return
	}
	*busy = true
	w.work(func() {
		c, cancel := ctx()
		defer cancel()
		v, err := do(c)
		w.post(func() {
			*busy = false
			if w.closed || (wanted != nil && !wanted()) {
				return
			}
			done(v, err)
		})
	})
}

// callErr is call for a request that answers with an error only.
func callErr(w *workspace, do func(context.Context) error, done func(error)) {
	call(w, func(c context.Context) (struct{}, error) { return struct{}{}, do(c) }, func(_ struct{}, err error) { done(err) })
}

// callErrIf is callErr that drops a stale answer.
func callErrIf(w *workspace, wanted func() bool, do func(context.Context) error, done func(error)) {
	callIf(w, wanted, func(c context.Context) (struct{}, error) { return struct{}{}, do(c) }, func(_ struct{}, err error) { done(err) })
}
