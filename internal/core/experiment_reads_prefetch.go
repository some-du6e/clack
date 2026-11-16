package core

// The experiment the sidebar's press prefetch reads (TestExperimentReadsListed;
// ui sidebar_prefetch.go): boot-render PZNh prefetchPointerdownHistory
// starts a pressed row's history request only for "treatment", the
// account's group (account_experiments.json), which the defaults keep
// until experiments.getByUser answers.
func init() {
	experimentReads["pointerdown_history_prefetch"] = []string{"treatment"}
	defaultExperimentGroups["pointerdown_history_prefetch"] = "treatment"
}
