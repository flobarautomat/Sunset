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
	const played = new Set<number>();

	function onTimeUpdate() {
		const now = videoEl.currentTime;
		for (const cue of options.cues) {
			if (prevTime < cue.at_seconds && cue.at_seconds <= now && !played.has(cue.id)) {
				played.add(cue.id);
				triggerCue(cue);
			}
		}
		prevTime = now;
	}

	function onSeeked() {
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
	videoEl.addEventListener('seeked', onSeeked);

	return function cleanup() {
		videoEl.removeEventListener('timeupdate', onTimeUpdate);
		videoEl.removeEventListener('seeked', onSeeked);
	};
}
