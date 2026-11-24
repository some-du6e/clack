package store

// Red-run shim (thread-ephemerals): the branch's API on the old code.

func (s *Store) ThreadSliceMissing(ch, ts string, t ThreadSlice) []string { return nil }
