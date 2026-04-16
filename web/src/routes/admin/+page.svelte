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
	<title>Dashboard — Moonrise</title>
</svelte:head>

<main>
	<h1>Dashboard</h1>
	<p>Backend: {status}</p>
</main>
