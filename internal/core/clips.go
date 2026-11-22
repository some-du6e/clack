package core

// AudioClipsAllowed is whether the composer for ch may offer the audio
// clip button, Slack's canPostAudioMessage (boot-shared Qre6 hP, which
// InputContainer's mapStateToProps calls with the channel and thread)
// without its last term, window.MediaRecorder, which is the caller's (a
// build that can record):
//
//   - canDestinationReceiveAudioClips: the team allows audio clips
//     (allow_audio_clips), and in a Slack Connect channel (is_ext_shared)
//     also allow_audio_clip_sharing_slack_connect. Its can-post term
//     (canUserPostInChannel, or canThread in a thread) is the composer's
//     roadblock, which replaces the input altogether;
//   - canPostAudioMessageInDestination: not where external huddles keep
//     clips out (M8Ke f: with the external_huddles experiment on, an
//     external guest, or a huddle channel).
func (a *App) AudioClipsAllowed(ch string) bool {
	c, _ := a.S.Conv(ch)
	if !a.teamPrefOn("allow_audio_clips") {
		return false
	}
	if c != nil && c.IsExtShared && !a.teamPrefOn("allow_audio_clip_sharing_slack_connect") {
		return false
	}
	if a.experiment(ExpExternalHuddles) == "on" {
		self := a.inviteSelf()
		if self.IsRestricted && self.RestrictedUserType == "external_guest" {
			return false
		}
		if c != nil && convHuddleChannel(c) {
			return false
		}
	}
	return true
}
