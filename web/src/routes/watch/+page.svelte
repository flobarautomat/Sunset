<script lang="ts">
	import { onMount } from 'svelte';
	import { createTracker } from '$lib/tracker';

	interface Cue {
		id: number;
		video_id: string;
		at_seconds: number;
		prompt: string;
		voice_id: string;
	}

	let videoEl: HTMLVideoElement;
	let containerEl: HTMLDivElement;
	let seekBarEl: HTMLDivElement;

	let sessionId = $state('');
	let status = $state('initializing...');
	let playing = $state(false);
	let currentTime = $state(0);
	let duration = $state(0);
	let muted = $state(false);
	let showControls = $state(true);
	let cues = $state<Cue[]>([]);
	let hoveredCue = $state<Cue | null>(null);
	let cueTooltipX = $state(0);
	let promptText = $state('');
	let seeking = $state(false);
	let backendDuration = $state(0);

	let hideTimeout: ReturnType<typeof setTimeout>;

	function formatTime(seconds: number): string {
		if (!isFinite(seconds) || seconds < 0) return '0:00';
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = Math.floor(seconds % 60);
		const pad = (n: number) => n.toString().padStart(2, '0');
		return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
	}

	// For large mp4s where the browser can't determine duration (moov atom at end of file),
	// we fall back to the backend's metadata which parsed it at startup.
	function effectiveDuration(): number {
		if (duration > 0 && isFinite(duration)) return duration;
		return backendDuration;
	}

	function togglePlay() {
		if (videoEl.paused) {
			videoEl.play();
		} else {
			videoEl.pause();
		}
	}

	function toggleMute() {
		videoEl.muted = !videoEl.muted;
		muted = videoEl.muted;
	}

	function onMouseActivity() {
		showControls = true;
		clearTimeout(hideTimeout);
		hideTimeout = setTimeout(() => {
			if (playing) showControls = false;
		}, 3000);
	}

	function onSeekBarClick(e: MouseEvent) {
		const dur = effectiveDuration();
		if (!seekBarEl || !dur) return;
		const rect = seekBarEl.getBoundingClientRect();
		const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
		videoEl.currentTime = pct * dur;
	}

	function onSeekBarMouseDown(e: MouseEvent) {
		seeking = true;
		onSeekBarClick(e);

		function onMove(e: MouseEvent) {
			const dur = effectiveDuration();
			if (!seekBarEl || !dur) return;
			const rect = seekBarEl.getBoundingClientRect();
			const pct = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
			videoEl.currentTime = pct * dur;
		}

		function onUp() {
			seeking = false;
			window.removeEventListener('mousemove', onMove);
			window.removeEventListener('mouseup', onUp);
		}

		window.addEventListener('mousemove', onMove);
		window.addEventListener('mouseup', onUp);
	}

	function seekToCue(cue: Cue) {
		videoEl.currentTime = cue.at_seconds;
	}

	function onCueHover(cue: Cue, e: MouseEvent) {
		hoveredCue = cue;
		if (seekBarEl) {
			const rect = seekBarEl.getBoundingClientRect();
			cueTooltipX = e.clientX - rect.left;
		}
	}

	function onCueLeave() {
		hoveredCue = null;
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.target instanceof HTMLInputElement) return;
		if (e.code === 'Space') {
			e.preventDefault();
			togglePlay();
		}
	}

	function handlePromptSubmit() {
		if (!promptText.trim()) return;
		// Stub — Phase 3 will wire this to /api/chat
		promptText = '';
	}

	onMount(() => {
		let cleanup: (() => void) | undefined;

		async function init() {
			try {
				const res = await fetch('/api/sessions', {
					method: 'POST',
					headers: { 'Content-Type': 'application/json' },
					body: JSON.stringify({ video_id: 'default' })
				});
				if (!res.ok) {
					status = `session error: ${res.status}`;
					return;
				}
				const data = await res.json();
				sessionId = data.session_id;
				status = 'connected';
				cleanup = createTracker(sessionId, videoEl);

				// Fetch video metadata for duration fallback (browser may not know it for large files)
				try {
					const videosRes = await fetch('/api/videos');
					if (videosRes.ok) {
						const videos = await videosRes.json();
						const defaultVideo = videos.find((v: { id: string }) => v.id === 'default');
						if (defaultVideo?.duration) {
							backendDuration = defaultVideo.duration;
						}
					}
				} catch {
					// Non-critical
				}

				// Fetch cues
				try {
					const cueRes = await fetch('/api/videos/default/cues');
					if (cueRes.ok) {
						cues = await cueRes.json();
					}
				} catch {
					// Cues are non-critical
				}
			} catch {
				status = 'backend unreachable';
			}
		}

		init();

		return () => {
			clearTimeout(hideTimeout);
			if (cleanup) cleanup();
		};
	});

	let progress = $derived(effectiveDuration() > 0 ? (currentTime / effectiveDuration()) * 100 : 0);
</script>

<svelte:head>
	<title>Watch — Moonrise</title>
</svelte:head>

<svelte:window onkeydown={handleKeydown} />

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
	class="player-container"
	class:hide-cursor={!showControls}
	bind:this={containerEl}
	onmousemove={onMouseActivity}
>
	<!-- svelte-ignore a11y_media_has_caption -->
	<video
		bind:this={videoEl}
		src="/api/videos/default/stream"
		onclick={togglePlay}
		onplay={() => playing = true}
		onpause={() => playing = false}
		ontimeupdate={() => { if (!seeking) currentTime = videoEl.currentTime; }}
		onloadedmetadata={() => duration = videoEl.duration}
		ondurationchange={() => duration = videoEl.duration}
	></video>

	{#if status !== 'connected'}
		<div class="status-overlay">
			<p>{status}</p>
		</div>
	{/if}

	<div class="controls" class:visible={showControls}>
		<div class="seek-container">
			<!-- svelte-ignore a11y_no_static_element_interactions -->
			<div
				class="seek-bar"
				bind:this={seekBarEl}
				onmousedown={onSeekBarMouseDown}
			>
				<div class="seek-track">
					<div class="seek-progress" style="width: {progress}%"></div>
					<div class="seek-thumb" style="left: {progress}%"></div>
				</div>

				{#each cues as cue}
					{@const pos = effectiveDuration() > 0 ? (cue.at_seconds / effectiveDuration()) * 100 : 0}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div
						class="cue-marker"
						style="left: {pos}%"
						onmouseenter={(e) => onCueHover(cue, e)}
						onmouseleave={onCueLeave}
						onclick={(e) => { e.stopPropagation(); seekToCue(cue); }}
					></div>
				{/each}
			</div>

			{#if hoveredCue}
				<div class="cue-tooltip" style="left: {cueTooltipX}px">
					{hoveredCue.prompt}
				</div>
			{/if}
		</div>

		<div class="controls-row">
			<button class="control-btn" onclick={togglePlay} aria-label={playing ? 'Pause' : 'Play'}>
				{#if playing}
					<svg viewBox="0 0 24 24" fill="currentColor" width="24" height="24">
						<rect x="6" y="4" width="4" height="16" />
						<rect x="14" y="4" width="4" height="16" />
					</svg>
				{:else}
					<svg viewBox="0 0 24 24" fill="currentColor" width="24" height="24">
						<polygon points="5,3 19,12 5,21" />
					</svg>
				{/if}
			</button>

			<button class="control-btn" onclick={toggleMute} aria-label={muted ? 'Unmute' : 'Mute'}>
				{#if muted}
					<svg viewBox="0 0 24 24" fill="currentColor" width="24" height="24">
						<polygon points="11,5 6,9 2,9 2,15 6,15 11,19" />
						<line x1="23" y1="9" x2="17" y2="15" stroke="currentColor" stroke-width="2" />
						<line x1="17" y1="9" x2="23" y2="15" stroke="currentColor" stroke-width="2" />
					</svg>
				{:else}
					<svg viewBox="0 0 24 24" fill="currentColor" width="24" height="24">
						<polygon points="11,5 6,9 2,9 2,15 6,15 11,19" />
						<path d="M15.54 8.46a5 5 0 010 7.07" fill="none" stroke="currentColor" stroke-width="2" />
						<path d="M19.07 4.93a10 10 0 010 14.14" fill="none" stroke="currentColor" stroke-width="2" />
					</svg>
				{/if}
			</button>

			<span class="time-display">
				{formatTime(currentTime)} / {formatTime(effectiveDuration())}
			</span>
		</div>
	</div>

	<div class="prompt-bar">
		<form onsubmit={(e) => { e.preventDefault(); handlePromptSubmit(); }}>
			<input
				type="text"
				bind:value={promptText}
				placeholder="Ask something about the scene..."
			/>
			<button type="submit" aria-label="Send">
				<svg viewBox="0 0 24 24" fill="currentColor" width="20" height="20">
					<path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
				</svg>
			</button>
		</form>
	</div>
</div>

<style>
	:global(body) {
		margin: 0;
		padding: 0;
		overflow: hidden;
		background: #000;
	}

	.player-container {
		width: 100vw;
		height: 100vh;
		position: relative;
		background: #000;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.player-container.hide-cursor {
		cursor: none;
	}

	video {
		width: 100%;
		height: 100%;
		object-fit: contain;
	}

	.status-overlay {
		position: absolute;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		color: #aaa;
		font-family: system-ui, -apple-system, sans-serif;
		font-size: 1.1rem;
	}

	.controls {
		position: absolute;
		bottom: 60px;
		left: 0;
		right: 0;
		padding: 40px 16px 12px;
		background: linear-gradient(transparent, rgba(0, 0, 0, 0.85));
		opacity: 0;
		transition: opacity 0.3s ease;
		pointer-events: none;
	}

	.controls.visible {
		opacity: 1;
		pointer-events: auto;
	}

	.seek-container {
		position: relative;
		margin-bottom: 8px;
	}

	.seek-bar {
		position: relative;
		height: 20px;
		display: flex;
		align-items: center;
		cursor: pointer;
	}

	.seek-track {
		width: 100%;
		height: 4px;
		background: rgba(255, 255, 255, 0.2);
		border-radius: 2px;
		position: relative;
		transition: height 0.15s ease;
	}

	.seek-bar:hover .seek-track {
		height: 6px;
	}

	.seek-progress {
		height: 100%;
		background: #e50914;
		border-radius: 2px;
		position: absolute;
		top: 0;
		left: 0;
	}

	.seek-thumb {
		position: absolute;
		top: 50%;
		width: 14px;
		height: 14px;
		background: #e50914;
		border-radius: 50%;
		transform: translate(-50%, -50%);
		opacity: 0;
		transition: opacity 0.15s ease;
	}

	.seek-bar:hover .seek-thumb {
		opacity: 1;
	}

	.cue-marker {
		position: absolute;
		top: 50%;
		width: 8px;
		height: 8px;
		background: #f5c518;
		border-radius: 50%;
		transform: translate(-50%, -50%);
		z-index: 2;
		cursor: pointer;
		transition: transform 0.15s ease;
	}

	.cue-marker:hover {
		transform: translate(-50%, -50%) scale(1.5);
	}

	.cue-tooltip {
		position: absolute;
		bottom: 28px;
		transform: translateX(-50%);
		background: rgba(0, 0, 0, 0.9);
		color: #fff;
		padding: 6px 10px;
		border-radius: 4px;
		font-size: 0.8rem;
		font-family: system-ui, -apple-system, sans-serif;
		white-space: nowrap;
		pointer-events: none;
		z-index: 10;
	}

	.controls-row {
		display: flex;
		align-items: center;
		gap: 12px;
	}

	.control-btn {
		background: none;
		border: none;
		color: #fff;
		cursor: pointer;
		padding: 4px;
		display: flex;
		align-items: center;
		justify-content: center;
		opacity: 0.9;
		transition: opacity 0.15s ease;
	}

	.control-btn:hover {
		opacity: 1;
	}

	.control-btn svg {
		width: 24px;
		height: 24px;
	}

	.time-display {
		color: #fff;
		font-family: system-ui, -apple-system, sans-serif;
		font-size: 0.85rem;
		font-variant-numeric: tabular-nums;
		user-select: none;
	}

	.prompt-bar {
		position: absolute;
		bottom: 0;
		left: 0;
		right: 0;
		padding: 10px 16px;
		background: rgba(0, 0, 0, 0.7);
	}

	.prompt-bar form {
		display: flex;
		gap: 8px;
		max-width: 600px;
		margin: 0 auto;
	}

	.prompt-bar input {
		flex: 1;
		padding: 10px 14px;
		border-radius: 24px;
		border: 1px solid rgba(255, 255, 255, 0.2);
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
		font-family: system-ui, -apple-system, sans-serif;
		font-size: 0.9rem;
		outline: none;
		transition: border-color 0.2s ease;
	}

	.prompt-bar input::placeholder {
		color: rgba(255, 255, 255, 0.4);
	}

	.prompt-bar input:focus {
		border-color: rgba(255, 255, 255, 0.5);
	}

	.prompt-bar button {
		background: #e50914;
		border: none;
		border-radius: 50%;
		width: 40px;
		height: 40px;
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: pointer;
		color: #fff;
		transition: background 0.15s ease;
		flex-shrink: 0;
	}

	.prompt-bar button:hover {
		background: #f40612;
	}
</style>
