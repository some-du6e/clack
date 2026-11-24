package core

// ExpMediaViewerFileDetailsTab is media_viewer_file_details_tab:
// boot-shared 2mfZ zq, `"treatment" === getGroupForUser(state, name)`
// without logging exposure (1S7V ze), read by the media viewer (bDav) for
// its sidebar's Details tab (popups/lightbox_sidebar.go). The account is
// in treatment (experiments drift, build 889ac449a32d, 2026-11-24).
const ExpMediaViewerFileDetailsTab = "media_viewer_file_details_tab"

func init() {
	experimentReads[ExpMediaViewerFileDetailsTab] = []string{"treatment"}
	defaultExperimentGroups[ExpMediaViewerFileDetailsTab] = "treatment"
}
