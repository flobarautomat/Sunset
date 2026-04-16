<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { createAdminWs, type SessionWithStats, type EventWithSession, type FilmStatsWithMeta } from '$lib/adminWs';
	import snarkdown from 'snarkdown';

	interface FeedEntry {
		id: string;
		timestamp: number;
		sessionId: string;
		kind: string;
		videoPos?: number;
		payload?: any;
	}

	let connected = $state(false);
	let sessions = $state<Map<string, SessionWithStats>>(new Map());
	let idleSessions = $state<Set<string>>(new Set());
	let feedEvents = $state<FeedEntry[]>([]);
	let expandedSession = $state<string | null>(null);
	let sessionDetails = $state<Map<string, FeedEntry[]>>(new Map());
	let autoScroll = $state(true);
	let showHeartbeats = $state(false);
	let feedEl = $state<HTMLDivElement>();
	let filmStats = $state<FilmStatsWithMeta[]>([]);
	let expandedFilmCues = $state<Set<string>>(new Set());
	let expandedFilmSynopsis = $state<Set<string>>(new Set());

	let activeView = $state<'feed' | 'sessions' | 'films'>('feed');

	let feedCounter = 0;

	function toFeedEntry(e: EventWithSession | any, sessionId?: string): FeedEntry {
		return {
			id: `feed-${feedCounter++}`,
			timestamp: e.at,
			sessionId: sessionId || e.session_id,
			kind: e.kind,
			videoPos: e.video_pos ?? undefined,
			payload: e.payload ? (typeof e.payload === 'string' ? tryParse(e.payload) : e.payload) : undefined
		};
	}

	function tryParse(s: string): any {
		try { return JSON.parse(s); } catch { return s; }
	}

	function pushFeed(entries: FeedEntry[]) {
		feedEvents = [...entries.reverse(), ...feedEvents].slice(0, 500);
		if (autoScroll) {
			tick().then(() => {
				if (feedEl) feedEl.scrollTop = 0;
			});
		}
	}

	function formatTimestamp(ms: number): string {
		const d = new Date(ms);
		const h = d.getHours().toString().padStart(2, '0');
		const m = d.getMinutes().toString().padStart(2, '0');
		const s = d.getSeconds().toString().padStart(2, '0');
		const ms3 = d.getMilliseconds().toString().padStart(3, '0');
		return `${h}:${m}:${s}.${ms3}`;
	}

	function formatTime(seconds: number): string {
		if (!isFinite(seconds) || seconds < 0) return '0:00';
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = Math.floor(seconds % 60);
		const pad = (n: number) => n.toString().padStart(2, '0');
		return h > 0 ? `${h}:${pad(m)}:${pad(s)}` : `${m}:${pad(s)}`;
	}

	function formatAge(ms: number): string {
		const now = Date.now();
		const diff = now - ms;
		if (diff < 60000) return `${Math.floor(diff / 1000)}s ago`;
		if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`;
		return `${Math.floor(diff / 3600000)}h ago`;
	}

	function shortId(id: string): string {
		return id.substring(0, 8);
	}

	const kindMeta: Record<string, { icon: string; label: string; color: string }> = {
		video_play:  { icon: '\u25B6', label: 'play',     color: 'rgba(255,255,255,0.7)' },
		video_pause: { icon: '\u23F8', label: 'pause',    color: 'rgba(255,255,255,0.7)' },
		video_seek:  { icon: '\u23E9', label: 'seek',     color: 'rgba(255,255,255,0.7)' },
		video_ended: { icon: '\u23F9', label: 'ended',    color: 'rgba(255,255,255,0.5)' },
		ai_message:  { icon: '\uD83D\uDCAC', label: 'message',  color: '#4fc3f7' },
		ai_response: { icon: '\uD83E\uDD16', label: 'response', color: '#81c784' },
		cue_played:  { icon: '\uD83D\uDD0A', label: 'cue',      color: '#f5c518' },
		heartbeat:   { icon: '\u00B7',  label: 'heartbeat', color: 'rgba(255,255,255,0.25)' }
	};

	function getKindMeta(kind: string) {
		return kindMeta[kind] || { icon: '?', label: kind, color: 'rgba(255,255,255,0.5)' };
	}

	function getPayloadText(entry: FeedEntry): string | null {
		if (!entry.payload) return null;
		if (entry.kind === 'ai_message' || entry.kind === 'ai_response') {
			return entry.payload.text || null;
		}
		if (entry.kind === 'cue_played') {
			return entry.payload.prompt || entry.payload.text || null;
		}
		return null;
	}

	function sessionStatus(s: SessionWithStats): 'live' | 'idle' | 'disconnected' {
		if (idleSessions.has(s.id)) return 'idle';
		const age = Date.now() - s.last_seen_at;
		if (age > 60000) return 'disconnected';
		if (age > 30000) return 'idle';
		return 'live';
	}

	function statusColor(status: string): string {
		switch (status) {
			case 'live': return '#4caf50';
			case 'idle': return '#ff9800';
			default: return '#666';
		}
	}

	function onFeedScroll() {
		if (!feedEl) return;
		const atTop = feedEl.scrollTop <= 30;
		autoScroll = atTop;
	}

	function jumpToLatest() {
		autoScroll = true;
		if (feedEl) feedEl.scrollTop = 0;
	}

	async function toggleSession(id: string) {
		if (expandedSession === id) {
			expandedSession = null;
			return;
		}
		expandedSession = id;

		if (!sessionDetails.has(id)) {
			try {
				const res = await fetch(`/api/admin/sessions/${id}`);
				if (res.ok) {
					const data = await res.json();
					const entries = (data.events || []).map((e: any) => toFeedEntry(e, id));
					sessionDetails = new Map(sessionDetails).set(id, entries);
				}
			} catch {
				// Non-critical
			}
		}
	}

	function toggleFilmCues(filmId: string) {
		const next = new Set(expandedFilmCues);
		if (next.has(filmId)) next.delete(filmId);
		else next.add(filmId);
		expandedFilmCues = next;
	}

	function toggleFilmSynopsis(filmId: string) {
		const next = new Set(expandedFilmSynopsis);
		if (next.has(filmId)) next.delete(filmId);
		else next.add(filmId);
		expandedFilmSynopsis = next;
	}

	let filteredFeed = $derived(
		showHeartbeats ? feedEvents : feedEvents.filter(e => e.kind !== 'heartbeat')
	);

	let sortedSessions = $derived(
		[...sessions.values()].sort((a, b) => b.last_seen_at - a.last_seen_at)
	);

	onMount(() => {
		const cleanup = createAdminWs({
			onSnapshot(snapshotSessions, snapshotEvents, snapshotFilmStats) {
				const map = new Map<string, SessionWithStats>();
				for (const s of snapshotSessions) map.set(s.id, s);
				sessions = map;

				feedCounter = 0;
				const entries = snapshotEvents.map(e => toFeedEntry(e)).reverse();
				feedEvents = entries.slice(0, 500);

				filmStats = snapshotFilmStats;

				tick().then(() => {
					if (feedEl) feedEl.scrollTop = 0;
				});
			},
			onSessionCreated(sessionId, payload) {
				const now = Date.now();
				const newSession: SessionWithStats = {
					id: sessionId,
					video_id: payload.video_id || 'heat',
					user_agent: payload.user_agent || '',
					created_at: now,
					last_seen_at: now,
					event_count: 0
				};
				sessions = new Map(sessions).set(sessionId, newSession);
				idleSessions = new Set([...idleSessions].filter(id => id !== sessionId));

				// Increment film session count
				filmStats = filmStats.map(f =>
					f.id === payload.video_id ? { ...f, session_count: f.session_count + 1, active_sessions: f.active_sessions + 1 } : f
				);

				pushFeed([{
					id: `feed-${feedCounter++}`,
					timestamp: now,
					sessionId,
					kind: 'session_created',
					payload: { video_id: payload.video_id }
				}]);
			},
			onEventsRecorded(sessionId, events) {
				const entries = events.map((e: any) => toFeedEntry(e, sessionId));
				pushFeed(entries);

				const existing = sessions.get(sessionId);
				if (existing) {
					const updated = {
						...existing,
						event_count: existing.event_count + events.length,
						last_seen_at: Date.now(),
						last_event_kind: events[events.length - 1]?.kind
					};
					sessions = new Map(sessions).set(sessionId, updated);

					// Update film stats live
					const videoId = existing.video_id;
					const deltas: Record<string, number> = {};
					for (const e of events) {
						if (e.kind === 'video_play') deltas['play_count'] = (deltas['play_count'] || 0) + 1;
						if (e.kind === 'ai_message') deltas['chat_messages'] = (deltas['chat_messages'] || 0) + 1;
						if (e.kind === 'ai_response') deltas['ai_responses'] = (deltas['ai_responses'] || 0) + 1;
						if (e.kind === 'cue_played') deltas['cues_triggered'] = (deltas['cues_triggered'] || 0) + 1;
					}
					if (Object.keys(deltas).length > 0) {
						filmStats = filmStats.map(f => {
							if (f.id !== videoId) return f;
							return {
								...f,
								play_count: f.play_count + (deltas['play_count'] || 0),
								chat_messages: f.chat_messages + (deltas['chat_messages'] || 0),
								ai_responses: f.ai_responses + (deltas['ai_responses'] || 0),
								cues_triggered: f.cues_triggered + (deltas['cues_triggered'] || 0),
							};
						});
					}
				}
				idleSessions = new Set([...idleSessions].filter(id => id !== sessionId));

				if (expandedSession === sessionId) {
					const existing = sessionDetails.get(sessionId) || [];
					sessionDetails = new Map(sessionDetails).set(sessionId, [...existing, ...entries]);
				}
			},
			onSessionIdle(sessionId) {
				idleSessions = new Set([...idleSessions, sessionId]);
			},
			onConnectionChange(isConnected) {
				connected = isConnected;
			}
		});

		return cleanup;
	});

	const sidebarItems: { key: typeof activeView; label: string; icon: string }[] = [
		{ key: 'feed', label: 'Live Feed', icon: '\u25C9' },
		{ key: 'sessions', label: 'Sessions', icon: '\u2630' },
		{ key: 'films', label: 'Films', icon: '\uD83C\uDFAC' },
	];
</script>

<svelte:head>
	<title>Dashboard — Moonrise</title>
</svelte:head>

<div class="dashboard">
	<header class="header">
		<div class="header-left">
			<span class="brand">moonrise</span>
			<span class="header-label">DASHBOARD</span>
		</div>
	</header>

	<div class="layout">
		<nav class="sidebar">
			<div class="sidebar-items">
				{#each sidebarItems as item}
					<!-- svelte-ignore a11y_click_events_have_key_events -->
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<div
						class="sidebar-item"
						class:active={activeView === item.key}
						onclick={() => activeView = item.key}
					>
						<span class="sidebar-icon">{item.icon}</span>
						<span class="sidebar-label">{item.label}</span>
					</div>
				{/each}
			</div>
			<div class="sidebar-footer">
				<div class="sidebar-status">
					<span class="connection-dot" class:connected></span>
					<span class="connection-label">{connected ? 'live' : 'offline'}</span>
				</div>
				<a href="/watch" class="sidebar-watch">
					<svg viewBox="0 0 24 24" fill="currentColor" width="14" height="14"><polygon points="5,3 19,12 5,21" /></svg>
					Watch
				</a>
			</div>
		</nav>

		<main class="main">
			{#if activeView === 'feed'}
				<section class="panel feed-panel">
					<div class="panel-header">
						<span class="panel-title">LIVE FEED</span>
						<span class="panel-badge">{filteredFeed.length}</span>
						<button class="heartbeat-toggle" class:active={showHeartbeats} onclick={() => showHeartbeats = !showHeartbeats}>
							{showHeartbeats ? 'hide' : 'show'} heartbeats
						</button>
					</div>
					<div class="feed" bind:this={feedEl} onscroll={onFeedScroll}>
						{#each filteredFeed as entry (entry.id)}
							{@const meta = entry.kind === 'session_created'
								? { icon: '+', label: 'new session', color: '#4caf50' }
								: getKindMeta(entry.kind)}
							<div class="feed-line" class:heartbeat={entry.kind === 'heartbeat'}>
								<span class="feed-time">{formatTimestamp(entry.timestamp)}</span>
								<span class="feed-session">{shortId(entry.sessionId)}</span>
								<span class="feed-icon" style="color: {meta.color}">{meta.icon}</span>
								<span class="feed-kind" style="color: {meta.color}">{meta.label}</span>
								{#if entry.videoPos !== undefined}
									<span class="feed-pos">pos {formatTime(entry.videoPos)}</span>
								{/if}
							</div>
							{#if getPayloadText(entry)}
								<div class="feed-detail" class:is-response={entry.kind === 'ai_response'} style="color: {meta.color}">
									{#if entry.kind === 'ai_response'}
										<div class="feed-detail-md">{@html snarkdown(getPayloadText(entry) || '')}</div>
									{:else}
										"{getPayloadText(entry)}"
									{/if}
								</div>
							{/if}
						{/each}
						{#if feedEvents.length === 0}
							<div class="feed-empty">Waiting for events...</div>
						{/if}
					</div>
					{#if !autoScroll}
						<button class="jump-btn" onclick={jumpToLatest}>&#8593; Jump to latest</button>
					{/if}
				</section>

			{:else if activeView === 'sessions'}
				<section class="panel sessions-panel">
					<div class="panel-header">
						<span class="panel-title">SESSIONS</span>
						<span class="panel-badge">{sortedSessions.length}</span>
					</div>
					<div class="sessions-list">
						{#each sortedSessions as session (session.id)}
							{@const status = sessionStatus(session)}
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<!-- svelte-ignore a11y_click_events_have_key_events -->
							<div class="session-row" class:expanded={expandedSession === session.id} onclick={() => toggleSession(session.id)}>
								<div class="session-summary">
									<span class="status-dot" style="background: {statusColor(status)}; {status === 'disconnected' ? 'opacity: 0.4;' : ''}"></span>
									<span class="session-id">{shortId(session.id)}</span>
									<span class="session-status" style="color: {statusColor(status)}">{status}</span>
									<span class="session-video">{session.video_id}</span>
									<span class="session-events">{session.event_count} events</span>
									<span class="session-age">{formatAge(session.last_seen_at)}</span>
									<span class="session-expand">{expandedSession === session.id ? '\u25BC' : '\u25B6'}</span>
								</div>
							</div>
							{#if expandedSession === session.id}
								<div class="session-timeline">
									{#if sessionDetails.has(session.id)}
										{#each sessionDetails.get(session.id) || [] as entry (entry.id)}
											{@const meta = getKindMeta(entry.kind)}
											<div class="feed-line timeline-line" class:heartbeat={entry.kind === 'heartbeat'}>
												<span class="feed-time">{formatTimestamp(entry.timestamp)}</span>
												<span class="feed-icon" style="color: {meta.color}">{meta.icon}</span>
												<span class="feed-kind" style="color: {meta.color}">{meta.label}</span>
												{#if entry.videoPos !== undefined}
													<span class="feed-pos">pos {formatTime(entry.videoPos)}</span>
												{/if}
											</div>
											{#if getPayloadText(entry)}
												<div class="feed-detail" class:is-response={entry.kind === 'ai_response'} style="color: {meta.color}">
													{#if entry.kind === 'ai_response'}
														<div class="feed-detail-md">{@html snarkdown(getPayloadText(entry) || '')}</div>
													{:else}
														"{getPayloadText(entry)}"
													{/if}
												</div>
											{/if}
										{/each}
										{#if (sessionDetails.get(session.id) || []).length === 0}
											<div class="feed-empty">No events recorded</div>
										{/if}
									{:else}
										<div class="feed-empty">Loading...</div>
									{/if}
								</div>
							{/if}
						{/each}
						{#if sortedSessions.length === 0}
							<div class="feed-empty">No sessions yet</div>
						{/if}
					</div>
				</section>

			{:else if activeView === 'films'}
				<section class="panel films-panel">
					<div class="panel-header">
						<span class="panel-title">FILMS</span>
						<span class="panel-badge">{filmStats.length}</span>
					</div>
					<div class="films-list">
						{#each filmStats as film (film.id)}
							<div class="film-card">
								<div class="film-header">
									<div class="film-title-row">
										<h3 class="film-title">{film.title}</h3>
										{#if film.year}
											<span class="film-year">{film.year}</span>
										{/if}
									</div>
									{#if film.director}
										<span class="film-director">Directed by {film.director}</span>
									{/if}
								</div>

								<div class="film-meta-row">
									<span class="film-meta-item">{formatTime(film.duration)}</span>
									{#if film.width && film.height}
										<span class="film-meta-item">{film.width}x{film.height}</span>
									{/if}
								</div>

								{#if film.synopsis}
									<!-- svelte-ignore a11y_click_events_have_key_events -->
									<!-- svelte-ignore a11y_no_static_element_interactions -->
									<div class="film-synopsis-toggle" onclick={() => toggleFilmSynopsis(film.id)}>
										{expandedFilmSynopsis.has(film.id) ? '\u25BC' : '\u25B6'} Synopsis
									</div>
									{#if expandedFilmSynopsis.has(film.id)}
										<p class="film-synopsis">{film.synopsis}</p>
									{/if}
								{/if}

								<div class="film-stats-grid">
									<div class="film-stat">
										<span class="film-stat-value">{film.session_count}</span>
										<span class="film-stat-label">Sessions</span>
									</div>
									<div class="film-stat">
										<span class="film-stat-value" style="color: {film.active_sessions > 0 ? '#4caf50' : 'inherit'}">{film.active_sessions}</span>
										<span class="film-stat-label">Active</span>
									</div>
									<div class="film-stat">
										<span class="film-stat-value">{film.play_count}</span>
										<span class="film-stat-label">Plays</span>
									</div>
									<div class="film-stat">
										<span class="film-stat-value" style="color: #4fc3f7">{film.chat_messages}</span>
										<span class="film-stat-label">Chat Msgs</span>
									</div>
									<div class="film-stat">
										<span class="film-stat-value" style="color: #81c784">{film.ai_responses}</span>
										<span class="film-stat-label">AI Replies</span>
									</div>
									<div class="film-stat">
										<span class="film-stat-value" style="color: #f5c518">{film.cues_triggered}</span>
										<span class="film-stat-label">Cues Fired</span>
									</div>
								</div>

								{#if film.cues.length > 0}
									<!-- svelte-ignore a11y_click_events_have_key_events -->
									<!-- svelte-ignore a11y_no_static_element_interactions -->
									<div class="film-cues-toggle" onclick={() => toggleFilmCues(film.id)}>
										{expandedFilmCues.has(film.id) ? '\u25BC' : '\u25B6'} Cues ({film.cues.length})
									</div>
									{#if expandedFilmCues.has(film.id)}
										<div class="film-cues-table">
											{#each film.cues as cue}
												<div class="film-cue-row">
													<span class="film-cue-time">{formatTime(cue.at_seconds)}</span>
													<span class="film-cue-text">{cue.prompt}</span>
												</div>
											{/each}
										</div>
									{/if}
								{/if}
							</div>
						{/each}
						{#if filmStats.length === 0}
							<div class="feed-empty">No films loaded</div>
						{/if}
					</div>
				</section>
			{/if}
		</main>
	</div>
</div>

<style>
	:global(body) {
		margin: 0;
		padding: 0;
		overflow: hidden;
		background: #0a0a0a;
	}

	.dashboard {
		display: flex;
		flex-direction: column;
		width: 100vw;
		height: 100vh;
		color: #fff;
		font-family: system-ui, -apple-system, sans-serif;
	}

	/* Header */
	.header {
		height: 48px;
		flex-shrink: 0;
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0 20px;
		background: rgba(20, 20, 20, 0.95);
		border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	}

	.header-left {
		display: flex;
		align-items: center;
		gap: 12px;
	}

	.brand {
		font-size: 1.1rem;
		font-weight: 700;
		color: #e50914;
		letter-spacing: -0.5px;
	}

	.header-label {
		font-size: 0.7rem;
		font-weight: 600;
		letter-spacing: 2px;
		color: rgba(255, 255, 255, 0.4);
	}

	/* Layout: sidebar + main */
	.layout {
		flex: 1;
		display: flex;
		min-height: 0;
	}

	/* Sidebar */
	.sidebar {
		width: 200px;
		flex-shrink: 0;
		display: flex;
		flex-direction: column;
		background: rgba(15, 15, 15, 0.95);
		border-right: 1px solid rgba(255, 255, 255, 0.08);
	}

	.sidebar-items {
		flex: 1;
		padding: 12px 0;
	}

	.sidebar-item {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 10px 20px;
		cursor: pointer;
		color: rgba(255, 255, 255, 0.45);
		font-size: 0.85rem;
		font-weight: 500;
		transition: all 0.15s;
		border-left: 3px solid transparent;
	}

	.sidebar-item:hover {
		color: rgba(255, 255, 255, 0.7);
		background: rgba(255, 255, 255, 0.04);
	}

	.sidebar-item.active {
		color: #fff;
		background: rgba(255, 255, 255, 0.06);
		border-left-color: #e50914;
	}

	.sidebar-icon {
		font-size: 1rem;
		width: 20px;
		text-align: center;
		flex-shrink: 0;
	}

	.sidebar-label {
		white-space: nowrap;
	}

	.sidebar-footer {
		padding: 12px 20px;
		border-top: 1px solid rgba(255, 255, 255, 0.08);
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.sidebar-status {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.connection-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: #666;
		transition: background 0.3s, box-shadow 0.3s;
	}

	.connection-dot.connected {
		background: #4caf50;
		box-shadow: 0 0 6px rgba(76, 175, 80, 0.6);
	}

	.connection-label {
		font-size: 0.75rem;
		color: rgba(255, 255, 255, 0.4);
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
	}

	.sidebar-watch {
		display: flex;
		align-items: center;
		gap: 6px;
		color: rgba(255, 255, 255, 0.4);
		text-decoration: none;
		font-size: 0.8rem;
		font-weight: 500;
		transition: color 0.15s;
	}

	.sidebar-watch:hover {
		color: rgba(255, 255, 255, 0.7);
	}

	/* Main content */
	.main {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		min-height: 0;
	}

	/* Panels */
	.panel {
		flex: 1;
		display: flex;
		flex-direction: column;
		min-height: 0;
		overflow: hidden;
	}

	.panel-header {
		flex-shrink: 0;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 10px 20px;
		background: rgba(255, 255, 255, 0.03);
		border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	}

	.panel-title {
		font-size: 0.7rem;
		font-weight: 600;
		letter-spacing: 2px;
		color: rgba(255, 255, 255, 0.4);
	}

	.panel-badge {
		font-size: 0.7rem;
		padding: 1px 8px;
		border-radius: 10px;
		background: rgba(255, 255, 255, 0.08);
		color: rgba(255, 255, 255, 0.5);
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
	}

	.heartbeat-toggle {
		margin-left: auto;
		background: rgba(255, 255, 255, 0.06);
		border: 1px solid rgba(255, 255, 255, 0.1);
		color: rgba(255, 255, 255, 0.4);
		padding: 2px 10px;
		border-radius: 10px;
		font-size: 0.65rem;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		cursor: pointer;
		transition: all 0.15s;
	}

	.heartbeat-toggle:hover {
		background: rgba(255, 255, 255, 0.1);
		color: rgba(255, 255, 255, 0.6);
	}

	.heartbeat-toggle.active {
		background: rgba(255, 255, 255, 0.1);
		color: rgba(255, 255, 255, 0.6);
		border-color: rgba(255, 255, 255, 0.2);
	}

	/* Feed */
	.feed-panel {
		position: relative;
	}

	.feed {
		flex: 1;
		overflow-y: auto;
		padding: 8px 0;
		min-height: 0;
	}

	.feed::-webkit-scrollbar {
		width: 6px;
	}

	.feed::-webkit-scrollbar-track {
		background: transparent;
	}

	.feed::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.1);
		border-radius: 3px;
	}

	.feed::-webkit-scrollbar-thumb:hover {
		background: rgba(255, 255, 255, 0.2);
	}

	.feed-line {
		display: flex;
		align-items: baseline;
		gap: 12px;
		padding: 2px 20px;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		font-size: 0.8rem;
		line-height: 1.6;
	}

	.feed-line.heartbeat {
		opacity: 0.35;
	}

	.feed-line:hover {
		background: rgba(255, 255, 255, 0.03);
	}

	.feed-time {
		color: rgba(255, 255, 255, 0.3);
		flex-shrink: 0;
		min-width: 100px;
	}

	.feed-session {
		color: rgba(255, 255, 255, 0.5);
		flex-shrink: 0;
		min-width: 72px;
	}

	.feed-icon {
		flex-shrink: 0;
		width: 20px;
		text-align: center;
	}

	.feed-kind {
		flex-shrink: 0;
		min-width: 72px;
	}

	.feed-pos {
		color: rgba(255, 255, 255, 0.3);
		flex-shrink: 0;
	}

	.feed-detail {
		padding: 2px 20px 4px 156px;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		font-size: 0.75rem;
		line-height: 1.5;
		opacity: 0.7;
		white-space: pre-wrap;
		word-break: break-word;
	}

	.feed-detail.is-response {
		opacity: 0.8;
	}

	.feed-detail-md :global(p) {
		margin: 0 0 0.3em;
	}

	.feed-detail-md :global(p:last-child) {
		margin-bottom: 0;
	}

	.feed-detail-md :global(strong) {
		font-weight: 700;
	}

	.feed-detail-md :global(code) {
		background: rgba(255, 255, 255, 0.08);
		padding: 1px 4px;
		border-radius: 2px;
		font-size: 0.85em;
	}

	.feed-detail-md :global(ul),
	.feed-detail-md :global(ol) {
		margin: 0.2em 0;
		padding-left: 1.4em;
	}

	.feed-empty {
		padding: 20px;
		text-align: center;
		color: rgba(255, 255, 255, 0.2);
		font-size: 0.85rem;
	}

	.jump-btn {
		position: absolute;
		top: 44px;
		left: 50%;
		transform: translateX(-50%);
		background: rgba(229, 9, 20, 0.9);
		color: #fff;
		border: none;
		padding: 6px 16px;
		border-radius: 16px;
		font-size: 0.75rem;
		font-family: system-ui, -apple-system, sans-serif;
		cursor: pointer;
		z-index: 10;
		transition: background 0.15s;
		box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
	}

	.jump-btn:hover {
		background: #e50914;
	}

	/* Sessions */
	.sessions-list {
		flex: 1;
		overflow-y: auto;
		min-height: 0;
	}

	.sessions-list::-webkit-scrollbar {
		width: 6px;
	}

	.sessions-list::-webkit-scrollbar-track {
		background: transparent;
	}

	.sessions-list::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.1);
		border-radius: 3px;
	}

	.session-row {
		cursor: pointer;
		transition: background 0.1s;
		border-bottom: 1px solid rgba(255, 255, 255, 0.04);
	}

	.session-row:hover {
		background: rgba(255, 255, 255, 0.04);
	}

	.session-row.expanded {
		background: rgba(255, 255, 255, 0.03);
	}

	.session-summary {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 10px 20px;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		font-size: 0.8rem;
	}

	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.session-id {
		color: rgba(255, 255, 255, 0.7);
		flex-shrink: 0;
		min-width: 72px;
	}

	.session-status {
		flex-shrink: 0;
		min-width: 52px;
		font-size: 0.75rem;
	}

	.session-video {
		color: rgba(255, 255, 255, 0.35);
		flex-shrink: 0;
	}

	.session-events {
		color: rgba(255, 255, 255, 0.35);
		flex-shrink: 0;
	}

	.session-age {
		color: rgba(255, 255, 255, 0.25);
		margin-left: auto;
	}

	.session-expand {
		color: rgba(255, 255, 255, 0.3);
		font-size: 0.65rem;
		flex-shrink: 0;
	}

	.session-timeline {
		background: rgba(0, 0, 0, 0.3);
		border-top: 1px solid rgba(255, 255, 255, 0.04);
		padding: 6px 0;
		max-height: 300px;
		overflow-y: auto;
	}

	.session-timeline::-webkit-scrollbar {
		width: 6px;
	}

	.session-timeline::-webkit-scrollbar-track {
		background: transparent;
	}

	.session-timeline::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.1);
		border-radius: 3px;
	}

	.timeline-line {
		padding-left: 40px;
	}

	/* Films */
	.films-list {
		flex: 1;
		overflow-y: auto;
		padding: 16px 20px;
		min-height: 0;
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.films-list::-webkit-scrollbar {
		width: 6px;
	}

	.films-list::-webkit-scrollbar-track {
		background: transparent;
	}

	.films-list::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.1);
		border-radius: 3px;
	}

	.film-card {
		background: rgba(255, 255, 255, 0.04);
		border: 1px solid rgba(255, 255, 255, 0.08);
		border-radius: 10px;
		padding: 20px;
	}

	.film-header {
		margin-bottom: 12px;
	}

	.film-title-row {
		display: flex;
		align-items: baseline;
		gap: 10px;
	}

	.film-title {
		font-size: 1.2rem;
		font-weight: 700;
		color: #fff;
		margin: 0;
	}

	.film-year {
		font-size: 0.85rem;
		color: rgba(255, 255, 255, 0.4);
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
	}

	.film-director {
		font-size: 0.82rem;
		color: rgba(255, 255, 255, 0.45);
		display: block;
		margin-top: 2px;
	}

	.film-meta-row {
		display: flex;
		gap: 16px;
		margin-bottom: 12px;
	}

	.film-meta-item {
		font-size: 0.78rem;
		color: rgba(255, 255, 255, 0.35);
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		background: rgba(255, 255, 255, 0.06);
		padding: 2px 8px;
		border-radius: 4px;
	}

	.film-synopsis-toggle,
	.film-cues-toggle {
		font-size: 0.78rem;
		color: rgba(255, 255, 255, 0.45);
		cursor: pointer;
		padding: 4px 0;
		transition: color 0.15s;
		user-select: none;
	}

	.film-synopsis-toggle:hover,
	.film-cues-toggle:hover {
		color: rgba(255, 255, 255, 0.7);
	}

	.film-synopsis {
		font-size: 0.82rem;
		color: rgba(255, 255, 255, 0.5);
		line-height: 1.6;
		margin: 6px 0 12px;
		padding-left: 16px;
		border-left: 2px solid rgba(255, 255, 255, 0.08);
	}

	.film-stats-grid {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 10px;
		margin: 12px 0;
	}

	.film-stat {
		display: flex;
		flex-direction: column;
		align-items: center;
		padding: 10px 8px;
		background: rgba(255, 255, 255, 0.03);
		border: 1px solid rgba(255, 255, 255, 0.06);
		border-radius: 8px;
	}

	.film-stat-value {
		font-size: 1.3rem;
		font-weight: 700;
		color: rgba(255, 255, 255, 0.8);
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
	}

	.film-stat-label {
		font-size: 0.65rem;
		color: rgba(255, 255, 255, 0.35);
		text-transform: uppercase;
		letter-spacing: 0.5px;
		margin-top: 2px;
	}

	.film-cues-table {
		margin-top: 8px;
		border: 1px solid rgba(255, 255, 255, 0.06);
		border-radius: 6px;
		overflow: hidden;
	}

	.film-cue-row {
		display: flex;
		gap: 16px;
		padding: 8px 12px;
		font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace;
		font-size: 0.75rem;
		border-bottom: 1px solid rgba(255, 255, 255, 0.04);
	}

	.film-cue-row:last-child {
		border-bottom: none;
	}

	.film-cue-row:hover {
		background: rgba(255, 255, 255, 0.03);
	}

	.film-cue-time {
		color: #f5c518;
		flex-shrink: 0;
		min-width: 60px;
	}

	.film-cue-text {
		color: rgba(255, 255, 255, 0.5);
		line-height: 1.5;
	}
</style>
