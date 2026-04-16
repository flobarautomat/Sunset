<script lang="ts">
	let status = $state('connecting...');

	async function checkBackend() {
		try {
			const res = await fetch('/api/healthz');
			status = res.ok ? 'backend connected' : `backend error: ${res.status}`;
		} catch {
			status = 'backend unreachable';
		}
	}

	$effect(() => {
		checkBackend();
	});
</script>

<svelte:head>
	<title>Watch — Moonrise</title>
</svelte:head>

<main>
	<h1>Watch</h1>
	<p>Backend: {status}</p>
</main>
