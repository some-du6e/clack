package taut

// What the data store tests call that the fix added (a red tree on the
// code before it replaces this file with the old equivalents).

// closeData is Taut.Close's last flush.
func closeData(d *dataStore) error { return d.close() }
