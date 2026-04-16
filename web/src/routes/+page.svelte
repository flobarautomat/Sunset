<script lang="ts">
	// nothing dynamic needed
</script>

<svelte:head>
	<title>Moonrise</title>
</svelte:head>

<div class="page">
	<div class="content">
		<header class="hero">
			<h1 class="brand">moonrise</h1>
			<p class="tagline">AI-powered interactive video experience. Watch a film with AI voice cues and chat — while a live admin dashboard tracks every session in real time.</p>
			<nav class="hero-nav">
				<a href="/watch" class="nav-btn primary">
					<svg viewBox="0 0 24 24" fill="currentColor" width="18" height="18"><polygon points="5,3 19,12 5,21" /></svg>
					Watch
				</a>
				<a href="/admin" class="nav-btn secondary">
					<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
					Dashboard
				</a>
			</nav>
		</header>

		<section class="section">
			<h2>Setup</h2>

			<h3>Prerequisites</h3>
			<ul>
				<li><strong>Go 1.22+</strong> — <code>brew install go</code></li>
				<li><strong>Node 22</strong> — <code>brew install node@22</code></li>
			</ul>

			<h3>Install dependencies</h3>
			<pre><code>go mod download
cd web && npm install && cd ..</code></pre>

			<h3>Download the video file</h3>
			<p>The demo uses a real video file (~2 GB) that's too large for git.</p>
			<ol>
				<li>Download from: <a href="https://drive.google.com/file/d/1nEugaQe9h2ZUtQbnQNC_Nwtg1NHL3CKc/view?usp=drive_link" class="link" target="_blank" rel="noopener">Google Drive</a></li>
				<li>Save to: <code>data/films/heat/film.mp4</code></li>
			</ol>
			<pre><code>mkdir -p data/films/heat
mv ~/Downloads/Heat.mp4 data/films/heat/film.mp4</code></pre>

			<h3>Configure environment</h3>
			<pre><code>cp .env.example .env
# Edit .env and set your SUNSET_API_KEY</code></pre>

			<div class="config-grid">
				<div class="config-card">
					<h4>AI Chat</h4>
					<p><code>AI_PROVIDER=sunset</code> (default) uses the Sunset staging API. Set <code>AI_PROVIDER=anthropic</code> with your own <code>ANTHROPIC_API_KEY</code> to use the Anthropic Messages API directly.</p>
				</div>
				<div class="config-card">
					<h4>TTS / Voice Cues</h4>
					<p>5 narration-style voice cues trigger at key moments during the video.</p>
					<ul>
						<li><code>TTS_PROVIDER=browser</code> (default) — Web Speech API, no key needed</li>
						<li><code>TTS_PROVIDER=sunset</code> — Real AI voices via Sunset TTS API</li>
					</ul>
				</div>
			</div>

			<h3>Run</h3>
			<pre><code>./run.sh</code></pre>
			<p>Starts the Go backend on <code>:8080</code> and the SvelteKit dev server on <code>:5173</code>.</p>
		</section>

		<section class="section">
			<h2>Architecture</h2>
			<div class="arch-grid">
				<div class="arch-card">
					<span class="arch-icon">🎬</span>
					<h4>Video Player</h4>
					<p>Custom Netflix-style controls with seek bar, cue markers, and keyboard shortcuts</p>
				</div>
				<div class="arch-card">
					<span class="arch-icon">🤖</span>
					<h4>AI Chat</h4>
					<p>SSE streaming chat with context-aware responses about the current scene</p>
				</div>
				<div class="arch-card">
					<span class="arch-icon">🔊</span>
					<h4>Voice Cues</h4>
					<p>Timed narration cues with pluggable TTS — browser or Sunset API</p>
				</div>
				<div class="arch-card">
					<span class="arch-icon">📡</span>
					<h4>Live Dashboard</h4>
					<p>WebSocket-powered admin view with real-time session tracking and event feed</p>
				</div>
			</div>
			<p class="docs-link">Full design decisions: <a href="https://github.com" class="link" onclick={(e) => e.preventDefault()}>docs/design-decisions.md</a></p>
		</section>

		<footer class="footer">
			<span class="brand-sm">moonrise</span>
			<span class="divider">·</span>
			<span>Sunset Founding Engineer Challenge</span>
		</footer>
	</div>
</div>

<style>
	:global(body) {
		margin: 0;
		padding: 0;
		background: #0a0a0a;
		overflow-y: auto;
		overflow-x: hidden;
	}

	.page {
		min-height: 100vh;
		color: #fff;
		font-family: system-ui, -apple-system, sans-serif;
	}

	.content {
		max-width: 720px;
		margin: 0 auto;
		padding: 0 24px 60px;
	}

	/* Hero */
	.hero {
		padding: 80px 0 48px;
		text-align: center;
	}

	.brand {
		font-size: 3rem;
		font-weight: 700;
		letter-spacing: -0.04em;
		color: #e50914;
		margin: 0;
	}

	.tagline {
		color: rgba(255, 255, 255, 0.6);
		font-size: 1.05rem;
		line-height: 1.6;
		max-width: 520px;
		margin: 16px auto 32px;
	}

	.hero-nav {
		display: flex;
		gap: 12px;
		justify-content: center;
	}

	.nav-btn {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		padding: 12px 24px;
		border-radius: 8px;
		font-size: 0.9rem;
		font-weight: 600;
		text-decoration: none;
		transition: background 0.15s ease, transform 0.1s ease;
	}

	.nav-btn:active {
		transform: scale(0.97);
	}

	.nav-btn.primary {
		background: #e50914;
		color: #fff;
	}

	.nav-btn.primary:hover {
		background: #f40612;
	}

	.nav-btn.secondary {
		background: rgba(255, 255, 255, 0.1);
		color: rgba(255, 255, 255, 0.85);
		border: 1px solid rgba(255, 255, 255, 0.12);
	}

	.nav-btn.secondary:hover {
		background: rgba(255, 255, 255, 0.15);
		color: #fff;
	}

	/* Sections */
	.section {
		margin-top: 48px;
	}

	h2 {
		font-size: 1.3rem;
		font-weight: 600;
		color: #fff;
		margin: 0 0 20px;
		padding-bottom: 8px;
		border-bottom: 1px solid rgba(255, 255, 255, 0.1);
	}

	h3 {
		font-size: 0.95rem;
		font-weight: 600;
		color: rgba(255, 255, 255, 0.85);
		margin: 24px 0 8px;
	}

	h4 {
		font-size: 0.85rem;
		font-weight: 600;
		color: #fff;
		margin: 0 0 6px;
	}

	p {
		color: rgba(255, 255, 255, 0.6);
		font-size: 0.88rem;
		line-height: 1.6;
		margin: 6px 0;
	}

	ul, ol {
		color: rgba(255, 255, 255, 0.6);
		font-size: 0.88rem;
		line-height: 1.7;
		padding-left: 20px;
		margin: 8px 0;
	}

	li {
		margin: 4px 0;
	}

	code {
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		font-size: 0.82rem;
		background: rgba(255, 255, 255, 0.08);
		padding: 2px 6px;
		border-radius: 4px;
		color: #e50914;
	}

	pre {
		background: rgba(255, 255, 255, 0.05);
		border: 1px solid rgba(255, 255, 255, 0.08);
		border-radius: 8px;
		padding: 14px 16px;
		overflow-x: auto;
		margin: 12px 0;
	}

	pre code {
		background: none;
		padding: 0;
		color: rgba(255, 255, 255, 0.7);
		font-size: 0.82rem;
		line-height: 1.6;
	}

	.link {
		color: #e50914;
		text-decoration: none;
		transition: color 0.15s;
	}

	.link:hover {
		color: #f40612;
		text-decoration: underline;
	}

	/* Config cards */
	.config-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
		margin: 16px 0;
	}

	.config-card {
		background: rgba(255, 255, 255, 0.04);
		border: 1px solid rgba(255, 255, 255, 0.08);
		border-radius: 8px;
		padding: 16px;
	}

	.config-card ul {
		padding-left: 16px;
		margin: 6px 0 0;
		font-size: 0.82rem;
	}

	/* Architecture cards */
	.arch-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
		margin: 16px 0;
	}

	.arch-card {
		background: rgba(255, 255, 255, 0.04);
		border: 1px solid rgba(255, 255, 255, 0.08);
		border-radius: 8px;
		padding: 16px;
	}

	.arch-icon {
		font-size: 1.4rem;
		display: block;
		margin-bottom: 8px;
	}

	.arch-card p {
		font-size: 0.82rem;
		margin: 0;
	}

	.docs-link {
		margin-top: 16px;
		font-size: 0.85rem;
	}

	/* Footer */
	.footer {
		margin-top: 60px;
		padding-top: 20px;
		border-top: 1px solid rgba(255, 255, 255, 0.08);
		text-align: center;
		font-size: 0.8rem;
		color: rgba(255, 255, 255, 0.35);
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
	}

	.brand-sm {
		color: #e50914;
		font-weight: 600;
	}

	.divider {
		opacity: 0.4;
	}

	@media (max-width: 600px) {
		.config-grid, .arch-grid {
			grid-template-columns: 1fr;
		}

		.hero {
			padding: 48px 0 32px;
		}

		.brand {
			font-size: 2.2rem;
		}
	}
</style>
