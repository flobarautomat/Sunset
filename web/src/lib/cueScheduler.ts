interface Cue {
	id: number;
	video_id: string;
	at_seconds: number;
	prompt: string;
	voice_id: string;
}

interface CueSchedulerOptions {
	cues: Cue[];
	sessionId: string;
	onCue: (cue: Cue) => void;
}

export function createCueScheduler(
	videoEl: HTMLVideoElement,
	options: CueSchedulerOptions
): () => void {
	let prevTime = 0;
	let seeking = false;
	const played = new Set<number>();

	function onTimeUpdate() {
		if (seeking) return;
		const now = videoEl.currentTime;
		const delta = now - prevTime;
		// Skip cue checks on large jumps (>1.5s) — likely a seek that
		// didn't go through the seeking/seeked event pair yet
		if (delta > 1.5 || delta < 0) {
			prevTime = now;
			return;
		}
		for (const cue of options.cues) {
			if (prevTime < cue.at_seconds && cue.at_seconds <= now && !played.has(cue.id)) {
				played.add(cue.id);
				triggerCue(cue);
			}
		}
		prevTime = now;
	}

	function onSeeking() {
		seeking = true;
	}

	function onSeeked() {
		seeking = false;
		const now = videoEl.currentTime;
		for (const cue of options.cues) {
			if (cue.at_seconds > now) {
				played.delete(cue.id);
			}
		}
		prevTime = now;
	}

	function triggerCue(cue: Cue) {
		// Record cue_played event
		fetch(`/api/sessions/${options.sessionId}/events`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify([
				{
					kind: 'cue_played',
					at: Date.now(),
					video_pos: videoEl.currentTime,
					payload: JSON.stringify({ cue_id: cue.id })
				}
			])
		}).catch(() => {});

		options.onCue(cue);
	}

	videoEl.addEventListener('timeupdate', onTimeUpdate);
	videoEl.addEventListener('seeking', onSeeking);
	videoEl.addEventListener('seeked', onSeeked);

	return function cleanup() {
		videoEl.removeEventListener('timeupdate', onTimeUpdate);
		videoEl.removeEventListener('seeking', onSeeking);
		videoEl.removeEventListener('seeked', onSeeked);
	};
}
