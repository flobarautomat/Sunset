<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { createTracker } from '$lib/tracker';
	import { sendMessage, type ChatMessage } from '$lib/chat';
	import { createCueScheduler } from '$lib/cueScheduler';
	import { speak, pause as pauseSpeech, resume as resumeSpeech, cancel as cancelSpeech, getState as getSpeechState, getActiveId as getSpeechActiveId, onChange as onSpeechChange } from '$lib/speech';
	import snarkdown from 'snarkdown';

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
	let messagesEl = $state<HTMLDivElement>();

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
	let ttsProvider = $state<'browser' | 'sunset'>('browser');

	// Chat state
	let chatMessages = $state<ChatMessage[]>([]);
	let chatOpen = $state(false);
	let chatHeight = $state(300);
	let streaming = $state(false);
	let dragging = $state(false);

	// Speech state — reactive via onChange listener
	let speechState = $state<'idle' | 'speaking' | 'paused'>('idle');
	let speechActiveId = $state<string | null>(null);

	function toggleSpeech(msgIndex: number, content: string) {
		const id = `msg-${msgIndex}`;
		if (getSpeechActiveId() === id) {
			if (getSpeechState() === 'speaking') {
				pauseSpeech();
			} else if (getSpeechState() === 'paused') {
				resumeSpeech();
			}
		} else {
			speak(content, id);
		}
	}

	let hideTimeout: ReturnType<typeof setTimeout>;

	function formatTime(seconds: number): string {
		if (!isFinite(seconds) || seconds < 0) return '0:00';
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = Math.floor(seconds % 60);
		const pad = (n: number) => n.toString().padStart(2, '0');
		return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
	}

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

	// Drag-to-resize chat panel
	function onDragHandleMouseDown(e: MouseEvent) {
		e.preventDefault();
		dragging = true;
		const startY = e.clientY;
		const startHeight = chatHeight;

		function onMove(e: MouseEvent) {
			const delta = startY - e.clientY;
			const newHeight = Math.max(56, Math.min(window.innerHeight * 0.7, startHeight + delta));
			chatHeight = newHeight;
			if (newHeight > 80) chatOpen = true;
		}

		function onUp() {
			dragging = false;
			if (chatHeight < 80) {
				chatOpen = false;
			}
			window.removeEventListener('mousemove', onMove);
			window.removeEventListener('mouseup', onUp);
		}

		window.addEventListener('mousemove', onMove);
		window.addEventListener('mouseup', onUp);
	}

	async function handlePromptSubmit() {
		if (!promptText.trim() || streaming) return;

		const userMsg: ChatMessage = { role: 'user', content: promptText };
		chatMessages = [...chatMessages, userMsg];
		const assistantMsg: ChatMessage = { role: 'assistant', content: '' };
		chatMessages = [...chatMessages, assistantMsg];

		chatOpen = true;
		if (chatHeight < 200) chatHeight = 300;
		const msgText = promptText;
		promptText = '';
		streaming = true;

		const history = chatMessages.slice(0, -2);

		await sendMessage(
			sessionId,
			msgText,
			currentTime,
			history,
			(token) => {
				const lastIdx = chatMessages.length - 1;
				chatMessages[lastIdx] = {
					...chatMessages[lastIdx],
					content: chatMessages[lastIdx].content + token
				};
				tick().then(() => {
					if (messagesEl) messagesEl.scrollTop = messagesEl.scrollHeight;
				});
			},
			(fullText) => {
				streaming = false;
				if (ttsProvider === 'browser') {
					speak(fullText);
				} else {
					fetch('/api/tts', {
						method: 'POST',
						headers: { 'Content-Type': 'application/json' },
						body: JSON.stringify({ text: fullText, voice_id: '' })
					})
						.then((res) => {
							if (res.headers.get('content-type')?.includes('audio/mpeg')) {
								return res.blob().then((blob) => {
									const url = URL.createObjectURL(blob);
									new Audio(url).play();
								});
							}
						})
						.catch(console.error);
				}
			},
			(error) => {
				streaming = false;
				const lastIdx = chatMessages.length - 1;
				chatMessages[lastIdx] = {
					...chatMessages[lastIdx],
					content: `Error: ${error}`
				};
			}
		);
	}

	onMount(() => {
		let trackerCleanup: (() => void) | undefined;
		let cueCleanup: (() => void) | undefined;

		const speechCleanup = onSpeechChange(() => {
			speechState = getSpeechState();
			speechActiveId = getSpeechActiveId();
		});

		chatHeight = Math.round(window.innerHeight / 3);

		async function init() {
			try {
				// Fetch TTS config
				try {
					const configRes = await fetch('/api/config');
					if (configRes.ok) {
						const cfg = await configRes.json();
						if (cfg.tts_provider === 'sunset' || cfg.tts_provider === 'browser') {
							ttsProvider = cfg.tts_provider;
						}
					}
				} catch {
					// Non-critical, defaults to browser
				}

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
				trackerCleanup = createTracker(sessionId, videoEl);

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

				try {
					const cueRes = await fetch('/api/videos/default/cues');
					if (cueRes.ok) {
						cues = await cueRes.json();
						if (cues.length > 0) {
							cueCleanup = createCueScheduler(videoEl, { cues, sessionId });
						}
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
			cancelSpeech();
			speechCleanup();
			if (trackerCleanup) trackerCleanup();
			if (cueCleanup) cueCleanup();
		};
	});

	let progress = $derived(effectiveDuration() > 0 ? (currentTime / effectiveDuration()) * 100 : 0);
</script>

<svelte:head>
	<title>Watch — Moonrise</title>
</svelte:head>

<svelte:window onkeydown={handleKeydown} />

<div class="page-layout">
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div
		class="video-section"
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
	</div>

	<div
		class="chat-panel"
		class:dragging
		style="height: {chatOpen ? chatHeight : 56}px"
	>
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<div class="drag-handle" onmousedown={onDragHandleMouseDown}>
			<div class="drag-pill"></div>
		</div>

		{#if chatOpen}
			<div class="chat-messages" bind:this={messagesEl}>
				{#each chatMessages as msg, i}
					<div class="bubble-row {msg.role}">
						<div class="bubble {msg.role}">
							{#if msg.role === 'assistant'}
								{@html snarkdown(msg.content)}
								{#if streaming && msg === chatMessages[chatMessages.length - 1] && !msg.content}
									<span class="typing-indicator">...</span>
								{/if}
							{:else}
								{msg.content}
							{/if}
						</div>
						{#if msg.role === 'assistant' && msg.content}
							<button
								class="speech-btn"
								class:active={speechActiveId === `msg-${i}`}
								onclick={() => toggleSpeech(i, msg.content)}
								aria-label={speechActiveId === `msg-${i}` && speechState === 'speaking' ? 'Pause speech' : 'Speak message'}
							>
								{#if speechActiveId === `msg-${i}` && speechState === 'speaking'}
									<svg viewBox="0 0 24 24" fill="currentColor" width="14" height="14">
										<rect x="6" y="4" width="4" height="16" />
										<rect x="14" y="4" width="4" height="16" />
									</svg>
								{:else if speechActiveId === `msg-${i}` && speechState === 'paused'}
									<svg viewBox="0 0 24 24" fill="currentColor" width="14" height="14">
										<polygon points="5,3 19,12 5,21" />
									</svg>
								{:else}
									<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
										<polygon points="11,5 6,9 2,9 2,15 6,15 11,19" fill="currentColor" />
										<path d="M15.54 8.46a5 5 0 010 7.07" />
										<path d="M19.07 4.93a10 10 0 010 14.14" />
									</svg>
								{/if}
							</button>
						{/if}
					</div>
				{/each}
			</div>
		{/if}

		<div class="prompt-bar">
			<form onsubmit={(e) => { e.preventDefault(); handlePromptSubmit(); }}>
				<input
					type="text"
					bind:value={promptText}
					placeholder="Ask something about the scene..."
					disabled={streaming}
				/>
				<button type="submit" aria-label="Send" disabled={streaming || !promptText.trim()}>
					<svg viewBox="0 0 24 24" fill="currentColor" width="20" height="20">
						<path d="M2.01 21L23 12 2.01 3 2 10l15 2-15 2z" />
					</svg>
				</button>
			</form>
		</div>
	</div>
</div>

<style>
	:global(body) {
		margin: 0;
		padding: 0;
		overflow: hidden;
		background: #000;
	}

	.page-layout {
		display: flex;
		flex-direction: column;
		width: 100vw;
		height: 100vh;
	}

	.video-section {
		flex: 1;
		min-height: 0;
		position: relative;
		background: #000;
		display: flex;
		align-items: center;
		justify-content: center;
		overflow: hidden;
	}

	.video-section.hide-cursor {
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
		bottom: 0;
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
		z-index: 0;
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
		z-index: 1;
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

	/* Chat panel */
	.chat-panel {
		flex-shrink: 0;
		display: flex;
		flex-direction: column;
		background: rgba(20, 20, 20, 0.95);
		border-top: 1px solid rgba(255, 255, 255, 0.1);
		transition: height 0.2s ease;
		overflow: hidden;
	}

	.chat-panel.dragging {
		transition: none;
		user-select: none;
	}

	.drag-handle {
		height: 6px;
		flex-shrink: 0;
		cursor: ns-resize;
		display: flex;
		align-items: center;
		justify-content: center;
		background: rgba(255, 255, 255, 0.05);
	}

	.drag-handle:hover {
		background: rgba(255, 255, 255, 0.1);
	}

	.drag-pill {
		width: 40px;
		height: 3px;
		background: rgba(255, 255, 255, 0.3);
		border-radius: 2px;
	}

	.chat-messages {
		flex: 1;
		overflow-y: auto;
		padding: 12px 16px;
		display: flex;
		flex-direction: column;
		gap: 8px;
		min-height: 0;
	}

	.bubble {
		max-width: 75%;
		padding: 10px 14px;
		border-radius: 16px;
		font-size: 0.9rem;
		font-family: system-ui, -apple-system, sans-serif;
		line-height: 1.4;
		word-wrap: break-word;
	}

	.bubble.user {
		white-space: pre-wrap;
	}

	.bubble.user {
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
		border-bottom-left-radius: 4px;
	}

	.bubble.assistant {
		background: #e50914;
		color: #fff;
		border-bottom-right-radius: 4px;
		white-space: normal;
	}

	.bubble.assistant :global(p) {
		margin: 0 0 0.5em;
	}

	.bubble.assistant :global(p:last-child) {
		margin-bottom: 0;
	}

	.bubble.assistant :global(strong) {
		font-weight: 700;
	}

	.bubble.assistant :global(ul),
	.bubble.assistant :global(ol) {
		margin: 0.3em 0;
		padding-left: 1.4em;
	}

	.bubble.assistant :global(li) {
		margin-bottom: 0.2em;
	}

	.bubble.assistant :global(code) {
		background: rgba(0, 0, 0, 0.2);
		padding: 1px 5px;
		border-radius: 3px;
		font-size: 0.85em;
	}

	.bubble.assistant :global(a) {
		color: #ffd4d6;
		text-decoration: underline;
	}

	.bubble-row {
		display: flex;
		align-items: flex-end;
		gap: 6px;
	}

	.bubble-row.user {
		justify-content: flex-start;
	}

	.bubble-row.assistant {
		justify-content: flex-end;
	}

	.speech-btn {
		background: rgba(255, 255, 255, 0.1);
		border: none;
		border-radius: 50%;
		width: 28px;
		height: 28px;
		display: flex;
		align-items: center;
		justify-content: center;
		cursor: pointer;
		color: rgba(255, 255, 255, 0.5);
		flex-shrink: 0;
		transition: color 0.15s ease, background 0.15s ease;
		margin-bottom: 4px;
	}

	.speech-btn:hover {
		color: #fff;
		background: rgba(255, 255, 255, 0.2);
	}

	.speech-btn.active {
		color: #e50914;
		background: rgba(229, 9, 20, 0.15);
	}

	.speech-btn.active:hover {
		background: rgba(229, 9, 20, 0.25);
	}

	.typing-indicator {
		opacity: 0.6;
		animation: blink 1s infinite;
	}

	@keyframes blink {
		0%, 50% { opacity: 0.6; }
		25% { opacity: 0.2; }
	}

	.prompt-bar {
		flex-shrink: 0;
		padding: 10px 16px;
		background: rgba(0, 0, 0, 0.3);
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

	.prompt-bar input:disabled {
		opacity: 0.5;
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

	.prompt-bar button:hover:not(:disabled) {
		background: #f40612;
	}

	.prompt-bar button:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}
</style>
