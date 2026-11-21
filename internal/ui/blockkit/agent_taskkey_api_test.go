package blockkit

// taskStateKey is the open-state key of the plan block's task id at index
// i. The red-evidence commit replaces this file with a stub giving the
// old block/task_id/index key.
func taskStateKey(block, id string, i int) string { return "task\x00" + block + "\x00" + id }
