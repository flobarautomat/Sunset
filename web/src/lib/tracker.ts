interface TrackerEvent {
	kind: string;
	at: number;
	video_pos?: number;
}

export function createTracker(sessionId: string, videoEl: HTMLVideoElement) {
	let buffer: TrackerEvent[] = [];
	let heartbeatInterval: ReturnType<typeof setInterval>;
	let flushInterval: ReturnType<typeof setInterval>;

	function pushEvent(kind: string) {
		buffer.push({
			kind,
			at: Date.now(),
			video_pos: videoEl.currentTime
		});
	}

	function onPlay() { pushEvent('video_play'); }
	function onPause() { pushEvent('video_pause'); }
	function onSeeking() { pushEvent('video_seek'); }
	function onEnded() { pushEvent('video_ended'); }

	videoEl.addEventListener('play', onPlay);
	videoEl.addEventListener('pause', onPause);
	videoEl.addEventListener('seeking', onSeeking);
	videoEl.addEventListener('ended', onEnded);

	heartbeatInterval = setInterval(() => {
		pushEvent('heartbeat');
	}, 10_000);

	async function flush() {
		if (buffer.length === 0) return;

		const events = buffer;
		buffer = [];

		try {
			await fetch(`/api/sessions/${sessionId}/events`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify(events)
			});
		} catch {
			// Re-queue on failure so events aren't lost.
			buffer = events.concat(buffer);
		}
	}

	flushInterval = setInterval(flush, 2000);

	return function cleanup() {
		videoEl.removeEventListener('play', onPlay);
		videoEl.removeEventListener('pause', onPause);
		videoEl.removeEventListener('seeking', onSeeking);
		videoEl.removeEventListener('ended', onEnded);
		clearInterval(heartbeatInterval);
		clearInterval(flushInterval);
		flush(); // final flush
	};
}
