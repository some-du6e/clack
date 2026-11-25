package jsrt

// The Options the IndexedDB tests set that the fix added (a red tree on the
// code before it replaces this file with no-ops).

// withKeyWrap gives the runtime KeyWrap's secret.
func withKeyWrap(secret []byte) func(*Options) {
	return func(o *Options) { o.KeyWrap = func() ([]byte, error) { return secret, nil } }
}

// withIDBQuota sets IndexedDB's quota.
func withIDBQuota(n int64) func(*Options) {
	return func(o *Options) { o.IndexedDBQuota = n }
}
