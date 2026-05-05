package agent

import "fmt"

// emitProgress notifies OnTranscriptEvent (if set) and appends legacy stderr lines to Progress (if non-nil).
func (r *Runner) emitProgress(ev *TranscriptEvent) {
	if r == nil || ev == nil {
		return
	}
	if r.OnTranscriptEvent != nil {
		r.OnTranscriptEvent(*ev)
	}
	if r.Progress != nil {
		if s := transcriptLegacyText(r.ProgressPlain, r.ProgressMode, ev); s != "" {
			fmt.Fprint(r.Progress, s)
		}
	}
}
