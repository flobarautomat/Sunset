<script lang="ts">
	import { onMount } from 'svelte';
	import { createTracker } from '$lib/tracker';

	let videoEl: HTMLVideoElement;
	let sessionId = $state('');
	let status = $state('initializing...');

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
			} catch {
				status = 'backend unreachable';
			}
		}

		init();

		return () => {
			if (cleanup) cleanup();
		};
	});
</script>

<svelte:head>
	<title>Watch — Moonrise</title>
</svelte:head>

<main>
	<h1>Watch</h1>
	<p>Session: {sessionId || status}</p>

	<video
		bind:this={videoEl}
		src="/api/videos/default/stream"
		controls
		width="800"
	>
		<track kind="captions" />
	</video>
</main>
