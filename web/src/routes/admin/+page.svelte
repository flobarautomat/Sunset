<script lang="ts">
	import { onMount, onDestroy, tick } from 'svelte';
	import { createAdminWs, type SessionWithStats, type EventWithSession, type FilmStatsWithMeta, type SystemSnapshot, type AISnapshot, type HeatmapBucket } from '$lib/adminWs';
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
	let systemStats = $state<SystemSnapshot | undefined>(undefined);
	let aiStats = $state<AISnapshot | undefined>(undefined);
	let heatmapBuckets = $state<HeatmapBucket[]>([]);
	let hoveredBucket = $state<HeatmapBucket | null>(null);
	let heatmapMouseX = $state(0);
	let heatmapMouseY = $state(0);
	let localUptime = $state(0);
	let uptimeInterval: ReturnType<typeof setInterval> | undefined;

	let activeView = $state<'feed' | 'sessions' | 'films' | 'system' | 'ai' | 'heatmap'>('feed');

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

	function formatUptime(seconds: number): string {
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = Math.floor(seconds % 60);
		if (h > 0) return `${h}h ${m}m`;
		if (m > 0) return `${m}m ${s}s`;
		return `${s}s`;
	}

	function formatBytes(bytes: number): string {
		if (bytes < 1024) return `${bytes} B`;
		if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
		return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
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

	let aiConversations = $derived(
		feedEvents.filter(e => e.kind === 'ai_message' || e.kind === 'ai_response').slice(0, 50)
	);

	// Heatmap SVG dimensions
	const heatmapPadding = { top: 20, right: 20, bottom: 50, left: 50 };
	const heatmapHeight = 300;

	let heatmapMaxTotal = $derived(
		heatmapBuckets.length > 0 ? Math.max(...heatmapBuckets.map(b => b.total)) : 1
	);

	// Get film duration for heatmap time axis
	let heatmapFilmDuration = $derived(
		filmStats.length > 0 ? filmStats[0].duration : 0
	);

	let heatmapFilmCues = $derived(
		filmStats.length > 0 ? filmStats[0].cues : []
	);

	let heatmapChartW = $derived(800 - heatmapPadding.left - heatmapPadding.right);
	let heatmapLabelInterval = $derived(heatmapFilmDuration > 7200 ? 1800 : heatmapFilmDuration > 3600 ? 900 : 300);
	let heatmapTimeLabels = $derived(
		Array.from({ length: Math.floor(heatmapFilmDuration / heatmapLabelInterval) + 1 }, (_, i) => i * heatmapLabelInterval)
	);

	onMount(() => {
		const cleanup = createAdminWs({
			onSnapshot(data) {
				const map = new Map<string, SessionWithStats>();
				for (const s of data.sessions) map.set(s.id, s);
				sessions = map;

				feedCounter = 0;
				const entries = data.events.map(e => toFeedEntry(e)).reverse();
				feedEvents = entries.slice(0, 500);

				filmStats = data.filmStats;
				systemStats = data.systemStats;
				aiStats = data.aiStats;
				heatmapBuckets = data.heatmap;

				// Set up uptime ticking
				if (data.systemStats) {
					localUptime = data.systemStats.uptime_seconds;
					if (uptimeInterval) clearInterval(uptimeInterval);
					uptimeInterval = setInterval(() => { localUptime++; }, 1000);
				}

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

				filmStats = filmStats.map(f =>
					f.id === payload.video_id ? { ...f, session_count: f.session_count + 1, active_sessions: f.active_sessions + 1 } : f
				);

				// Increment system stats
				if (systemStats) {
					systemStats = { ...systemStats, total_sessions: systemStats.total_sessions + 1, active_sessions: systemStats.active_sessions + 1 };
				}

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

					// Increment system event count
					if (systemStats) {
						systemStats = { ...systemStats, total_events: systemStats.total_events + events.length };
					}

					// Increment AI stats
					if (aiStats) {
						const aiMsgs = events.filter((e: any) => e.kind === 'ai_message').length;
						const aiResps = events.filter((e: any) => e.kind === 'ai_response').length;
						const cuePlays = events.filter((e: any) => e.kind === 'cue_played').length;
						if (aiMsgs || aiResps || cuePlays) {
							aiStats = {
								...aiStats,
								total_messages: aiStats.total_messages + aiMsgs,
								total_responses: aiStats.total_responses + aiResps,
								total_cue_plays: aiStats.total_cue_plays + cuePlays,
							};
						}
					}

					// Update heatmap buckets
					for (const e of events) {
						if (e.video_pos != null && e.kind !== 'heartbeat') {
							const bucketStart = Math.floor(e.video_pos / 60) * 60;
							const idx = heatmapBuckets.findIndex(b => b.bucket_start === bucketStart);
							if (idx >= 0) {
								const b = { ...heatmapBuckets[idx] };
								b.total++;
								if (e.kind === 'video_play') b.play_count++;
								else if (e.kind === 'video_pause') b.pause_count++;
								else if (e.kind === 'video_seek') b.seek_count++;
								else if (e.kind === 'ai_message' || e.kind === 'ai_response') b.chat_count++;
								else if (e.kind === 'cue_played') b.cue_count++;
								heatmapBuckets = [...heatmapBuckets.slice(0, idx), b, ...heatmapBuckets.slice(idx + 1)];
							} else {
								const newBucket: HeatmapBucket = {
									bucket_start: bucketStart,
									bucket_end: bucketStart + 60,
									play_count: e.kind === 'video_play' ? 1 : 0,
									pause_count: e.kind === 'video_pause' ? 1 : 0,
									seek_count: e.kind === 'video_seek' ? 1 : 0,
									chat_count: (e.kind === 'ai_message' || e.kind === 'ai_response') ? 1 : 0,
									cue_count: e.kind === 'cue_played' ? 1 : 0,
									total: 1,
								};
								heatmapBuckets = [...heatmapBuckets, newBucket].sort((a, b) => a.bucket_start - b.bucket_start);
							}
						}
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

		return () => {
			cleanup();
			if (uptimeInterval) clearInterval(uptimeInterval);
		};
	});

	function filmDisplayName(videoId: string): string {
		const film = filmStats.find(f => f.id === videoId);
		if (!film) return videoId;
		const parts = [film.title];
		if (film.year) parts.push(`(${film.year})`);
		if (film.director) parts.push(`by ${film.director}`);
		return parts.join(' ');
	}

	function goToFilms() {
		activeView = 'films';
	}

	const sidebarItems: { key: typeof activeView; label: string; icon: string }[] = [
		{ key: 'feed', label: 'Live Feed', icon: '\u25C9' },
		{ key: 'sessions', label: 'Sessions', icon: '\u2630' },
		{ key: 'films', label: 'Films', icon: '\uD83C\uDFAC' },
		{ key: 'system', label: 'System', icon: '\u2699' },
		{ key: 'ai', label: 'AI Usage', icon: '\uD83E\uDD16' },
		{ key: 'heatmap', label: 'Heatmap', icon: '\u2593' },
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
									<!-- svelte-ignore a11y_click_events_have_key_events -->
								<!-- svelte-ignore a11y_no_static_element_interactions -->
								<span class="session-video film-link" onclick={(e) => { e.stopPropagation(); goToFilms(); }}>{filmDisplayName(session.video_id)}</span>
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
											<span class="film-year">({film.year})</span>
										{/if}
										{#if film.director}
											<span class="film-director">by {film.director}</span>
										{/if}
									</div>
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

			{:else if activeView === 'system'}
				<section class="panel system-panel">
					<div class="panel-header">
						<span class="panel-title">SYSTEM HEALTH</span>
					</div>
					<div class="widget-content">
						{#if systemStats}
							<div class="stats-grid-4">
								<div class="stat-card">
									<span class="stat-value">{formatUptime(localUptime)}</span>
									<span class="stat-label">Uptime</span>
								</div>
								<div class="stat-card">
									<span class="stat-value" style="color: #4caf50">{systemStats.ws_connections}</span>
									<span class="stat-label">WS Connections</span>
								</div>
								<div class="stat-card">
									<span class="stat-value">{systemStats.total_sessions}</span>
									<span class="stat-label">Total Sessions</span>
								</div>
								<div class="stat-card">
									<span class="stat-value">{systemStats.total_events}</span>
									<span class="stat-label">Total Events</span>
								</div>
								<div class="stat-card">
									<span class="stat-value" style="color: {systemStats.active_sessions > 0 ? '#4caf50' : 'inherit'}">{systemStats.active_sessions}</span>
									<span class="stat-label">Active Sessions</span>
								</div>
								<div class="stat-card">
									<span class="stat-value">{systemStats.cache_files}</span>
									<span class="stat-label">TTS Cache Files</span>
								</div>
								<div class="stat-card">
									<span class="stat-value">{formatBytes(systemStats.cache_size_bytes)}</span>
									<span class="stat-label">Cache Size</span>
								</div>
							</div>
						{:else}
							<div class="feed-empty">Loading system stats...</div>
						{/if}
					</div>
				</section>

			{:else if activeView === 'ai'}
				<section class="panel ai-panel">
					<div class="panel-header">
						<span class="panel-title">AI USAGE</span>
					</div>
					<div class="widget-content">
						{#if aiStats}
							<div class="stats-grid-4">
								<div class="stat-card">
									<span class="stat-value" style="color: #4fc3f7">{aiStats.total_messages}</span>
									<span class="stat-label">Messages Sent</span>
								</div>
								<div class="stat-card">
									<span class="stat-value" style="color: #81c784">{aiStats.total_responses}</span>
									<span class="stat-label">AI Responses</span>
								</div>
								<div class="stat-card">
									<span class="stat-value">~{Math.round(aiStats.avg_response_length)}</span>
									<span class="stat-label">Avg Response Chars</span>
								</div>
								<div class="stat-card">
									<span class="stat-value" style="color: #f5c518">{aiStats.total_cue_plays}</span>
									<span class="stat-label">Cues Triggered</span>
								</div>
							</div>

							<div class="ai-conversations">
								<div class="ai-conversations-header">RECENT CONVERSATIONS</div>
								<div class="ai-conversations-list">
									{#each aiConversations as entry (entry.id)}
										{@const meta = getKindMeta(entry.kind)}
										<div class="feed-line">
											<span class="feed-time">{formatTimestamp(entry.timestamp)}</span>
											<span class="feed-session">{shortId(entry.sessionId)}</span>
											<span class="feed-icon" style="color: {meta.color}">{meta.icon}</span>
											<span class="feed-kind" style="color: {meta.color}">{meta.label}</span>
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
									{#if aiConversations.length === 0}
										<div class="feed-empty">No AI conversations yet</div>
									{/if}
								</div>
							</div>
						{:else}
							<div class="feed-empty">Loading AI stats...</div>
						{/if}
					</div>
				</section>

			{:else if activeView === 'heatmap'}
				<section class="panel heatmap-panel">
					<div class="panel-header">
						<span class="panel-title">TIMELINE HEATMAP</span>
						{#if filmStats.length > 0}
							<!-- svelte-ignore a11y_click_events_have_key_events -->
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<span class="panel-badge film-link" onclick={() => goToFilms()}>{filmDisplayName(filmStats[0].id)}</span>
						{/if}
					</div>
					<div class="widget-content heatmap-content">
						{#if heatmapBuckets.length > 0 && heatmapFilmDuration > 0}
							<!-- svelte-ignore a11y_no_static_element_interactions -->
							<svg class="heatmap-svg" viewBox="0 0 800 {heatmapHeight}" preserveAspectRatio="xMidYMid meet"
								onmouseleave={() => hoveredBucket = null}>
								<!-- Y-axis labels -->
								<text x="{heatmapPadding.left - 8}" y="{heatmapPadding.top + 4}" text-anchor="end" fill="rgba(255,255,255,0.3)" font-size="10" font-family="monospace">{heatmapMaxTotal}</text>
								<text x="{heatmapPadding.left - 8}" y="{heatmapHeight - heatmapPadding.bottom}" text-anchor="end" fill="rgba(255,255,255,0.3)" font-size="10" font-family="monospace">0</text>

								<!-- Bars -->
								{#each heatmapBuckets as bucket}
									{@const chartW = 800 - heatmapPadding.left - heatmapPadding.right}
									{@const chartH = heatmapHeight - heatmapPadding.top - heatmapPadding.bottom}
									{@const x = heatmapPadding.left + (bucket.bucket_start / heatmapFilmDuration) * chartW}
									{@const barW = Math.max(2, (60 / heatmapFilmDuration) * chartW - 1)}
									{@const totalH = (bucket.total / heatmapMaxTotal) * chartH}
									{@const baseY = heatmapHeight - heatmapPadding.bottom}

									{@const playH = (bucket.play_count + bucket.pause_count) / bucket.total * totalH}
									{@const seekH = bucket.seek_count / bucket.total * totalH}
									{@const chatH = bucket.chat_count / bucket.total * totalH}
									{@const cueH = bucket.cue_count / bucket.total * totalH}

									<!-- svelte-ignore a11y_no_static_element_interactions -->
									<!-- svelte-ignore a11y_mouse_events_have_key_events -->
									<g onmouseenter={(e) => { hoveredBucket = bucket; heatmapMouseX = x; heatmapMouseY = baseY - totalH - 10; }}
									   onmouseleave={() => hoveredBucket = null}>
										<rect x={x} y={baseY - playH} width={barW} height={Math.max(0, playH)} fill="rgba(255,255,255,0.5)" />
										<rect x={x} y={baseY - playH - seekH} width={barW} height={Math.max(0, seekH)} fill="#f5c518" />
										<rect x={x} y={baseY - playH - seekH - chatH} width={barW} height={Math.max(0, chatH)} fill="#4fc3f7" />
										<rect x={x} y={baseY - playH - seekH - chatH - cueH} width={barW} height={Math.max(0, cueH)} fill="#ff9800" />
									</g>
								{/each}

								<!-- Cue markers -->
								{#each heatmapFilmCues as cue, i}
									{@const chartW = 800 - heatmapPadding.left - heatmapPadding.right}
									{@const cx = heatmapPadding.left + (cue.at_seconds / heatmapFilmDuration) * chartW}
									<line x1={cx} y1={heatmapPadding.top} x2={cx} y2={heatmapHeight - heatmapPadding.bottom} stroke="#ff9800" stroke-width="1" stroke-dasharray="3,3" opacity="0.5" />
									<text x={cx} y={heatmapHeight - heatmapPadding.bottom + 24} text-anchor="middle" fill="#ff9800" font-size="9" font-family="monospace" opacity="0.7">C{i + 1}</text>
								{/each}

								<!-- X-axis time labels -->
								{#each heatmapTimeLabels as t}
									{@const tx = heatmapPadding.left + (t / heatmapFilmDuration) * heatmapChartW}
									<text x={tx} y={heatmapHeight - heatmapPadding.bottom + 14} text-anchor="middle" fill="rgba(255,255,255,0.3)" font-size="10" font-family="monospace">{formatTime(t)}</text>
									<line x1={tx} y1={heatmapHeight - heatmapPadding.bottom} x2={tx} y2={heatmapHeight - heatmapPadding.bottom + 4} stroke="rgba(255,255,255,0.15)" />
								{/each}

								<!-- Baseline -->
								<line x1={heatmapPadding.left} y1={heatmapHeight - heatmapPadding.bottom} x2={800 - heatmapPadding.right} y2={heatmapHeight - heatmapPadding.bottom} stroke="rgba(255,255,255,0.15)" />

								<!-- Tooltip -->
								{#if hoveredBucket}
									{@const ttX = Math.min(heatmapMouseX, 650)}
									<g>
										<rect x={ttX} y={Math.max(5, heatmapMouseY - 60)} width="140" height="75" rx="4" fill="rgba(0,0,0,0.9)" stroke="rgba(255,255,255,0.2)" />
										<text x={ttX + 8} y={Math.max(5, heatmapMouseY - 60) + 14} fill="#fff" font-size="10" font-family="monospace" font-weight="bold">{formatTime(hoveredBucket.bucket_start)} - {formatTime(hoveredBucket.bucket_end)}</text>
										<text x={ttX + 8} y={Math.max(5, heatmapMouseY - 60) + 28} fill="rgba(255,255,255,0.5)" font-size="9" font-family="monospace">play/pause: {hoveredBucket.play_count + hoveredBucket.pause_count}</text>
										<text x={ttX + 8} y={Math.max(5, heatmapMouseY - 60) + 40} fill="#f5c518" font-size="9" font-family="monospace">seek: {hoveredBucket.seek_count}</text>
										<text x={ttX + 8} y={Math.max(5, heatmapMouseY - 60) + 52} fill="#4fc3f7" font-size="9" font-family="monospace">chat: {hoveredBucket.chat_count}</text>
										<text x={ttX + 8} y={Math.max(5, heatmapMouseY - 60) + 64} fill="#ff9800" font-size="9" font-family="monospace">cue: {hoveredBucket.cue_count}</text>
									</g>
								{/if}
							</svg>

							<!-- Legend -->
							<div class="heatmap-legend">
								<span class="legend-item"><span class="legend-swatch" style="background: rgba(255,255,255,0.5)"></span> play/pause</span>
								<span class="legend-item"><span class="legend-swatch" style="background: #f5c518"></span> seek</span>
								<span class="legend-item"><span class="legend-swatch" style="background: #4fc3f7"></span> chat</span>
								<span class="legend-item"><span class="legend-swatch" style="background: #ff9800"></span> cue</span>
							</div>
						{:else}
							<div class="feed-empty">No interaction data yet. Play a video to see the heatmap.</div>
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

	.header-left { display: flex; align-items: center; gap: 12px; }
	.brand { font-size: 1.1rem; font-weight: 700; color: #e50914; letter-spacing: -0.5px; }
	.header-label { font-size: 0.7rem; font-weight: 600; letter-spacing: 2px; color: rgba(255, 255, 255, 0.4); }

	.layout { flex: 1; display: flex; min-height: 0; }

	/* Sidebar */
	.sidebar { width: 200px; flex-shrink: 0; display: flex; flex-direction: column; background: rgba(15, 15, 15, 0.95); border-right: 1px solid rgba(255, 255, 255, 0.08); }
	.sidebar-items { flex: 1; padding: 12px 0; }
	.sidebar-item { display: flex; align-items: center; gap: 10px; padding: 10px 20px; cursor: pointer; color: rgba(255, 255, 255, 0.45); font-size: 0.85rem; font-weight: 500; transition: all 0.15s; border-left: 3px solid transparent; }
	.sidebar-item:hover { color: rgba(255, 255, 255, 0.7); background: rgba(255, 255, 255, 0.04); }
	.sidebar-item.active { color: #fff; background: rgba(255, 255, 255, 0.06); border-left-color: #e50914; }
	.sidebar-icon { font-size: 1rem; width: 20px; text-align: center; flex-shrink: 0; }
	.sidebar-label { white-space: nowrap; }
	.sidebar-footer { padding: 12px 20px; border-top: 1px solid rgba(255, 255, 255, 0.08); display: flex; flex-direction: column; gap: 10px; }
	.sidebar-status { display: flex; align-items: center; gap: 8px; }
	.connection-dot { width: 8px; height: 8px; border-radius: 50%; background: #666; transition: background 0.3s, box-shadow 0.3s; }
	.connection-dot.connected { background: #4caf50; box-shadow: 0 0 6px rgba(76, 175, 80, 0.6); }
	.connection-label { font-size: 0.75rem; color: rgba(255, 255, 255, 0.4); font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }
	.sidebar-watch { display: flex; align-items: center; gap: 6px; color: rgba(255, 255, 255, 0.4); text-decoration: none; font-size: 0.8rem; font-weight: 500; transition: color 0.15s; }
	.sidebar-watch:hover { color: rgba(255, 255, 255, 0.7); }

	/* Main */
	.main { flex: 1; min-width: 0; display: flex; flex-direction: column; min-height: 0; }

	/* Panels */
	.panel { flex: 1; display: flex; flex-direction: column; min-height: 0; overflow: hidden; }
	.panel-header { flex-shrink: 0; display: flex; align-items: center; gap: 8px; padding: 10px 20px; background: rgba(255, 255, 255, 0.03); border-bottom: 1px solid rgba(255, 255, 255, 0.08); }
	.panel-title { font-size: 0.7rem; font-weight: 600; letter-spacing: 2px; color: rgba(255, 255, 255, 0.4); }
	.panel-badge { font-size: 0.7rem; padding: 1px 8px; border-radius: 10px; background: rgba(255, 255, 255, 0.08); color: rgba(255, 255, 255, 0.5); font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }

	.heartbeat-toggle { margin-left: auto; background: rgba(255, 255, 255, 0.06); border: 1px solid rgba(255, 255, 255, 0.1); color: rgba(255, 255, 255, 0.4); padding: 2px 10px; border-radius: 10px; font-size: 0.65rem; font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; cursor: pointer; transition: all 0.15s; }
	.heartbeat-toggle:hover { background: rgba(255, 255, 255, 0.1); color: rgba(255, 255, 255, 0.6); }
	.heartbeat-toggle.active { background: rgba(255, 255, 255, 0.1); color: rgba(255, 255, 255, 0.6); border-color: rgba(255, 255, 255, 0.2); }

	/* Feed */
	.feed-panel { position: relative; }
	.feed { flex: 1; overflow-y: auto; padding: 8px 0; min-height: 0; }
	.feed::-webkit-scrollbar { width: 6px; }
	.feed::-webkit-scrollbar-track { background: transparent; }
	.feed::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 3px; }
	.feed::-webkit-scrollbar-thumb:hover { background: rgba(255, 255, 255, 0.2); }

	.feed-line { display: flex; align-items: baseline; gap: 12px; padding: 2px 20px; font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; font-size: 0.8rem; line-height: 1.6; }
	.feed-line.heartbeat { opacity: 0.35; }
	.feed-line:hover { background: rgba(255, 255, 255, 0.03); }
	.feed-time { color: rgba(255, 255, 255, 0.3); flex-shrink: 0; min-width: 100px; }
	.feed-session { color: rgba(255, 255, 255, 0.5); flex-shrink: 0; min-width: 72px; }
	.feed-icon { flex-shrink: 0; width: 20px; text-align: center; }
	.feed-kind { flex-shrink: 0; min-width: 72px; }
	.feed-pos { color: rgba(255, 255, 255, 0.3); flex-shrink: 0; }

	.feed-detail { padding: 2px 20px 4px 156px; font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; font-size: 0.75rem; line-height: 1.5; opacity: 0.7; white-space: pre-wrap; word-break: break-word; }
	.feed-detail.is-response { opacity: 0.8; }
	.feed-detail-md :global(p) { margin: 0 0 0.3em; }
	.feed-detail-md :global(p:last-child) { margin-bottom: 0; }
	.feed-detail-md :global(strong) { font-weight: 700; }
	.feed-detail-md :global(code) { background: rgba(255, 255, 255, 0.08); padding: 1px 4px; border-radius: 2px; font-size: 0.85em; }
	.feed-detail-md :global(ul), .feed-detail-md :global(ol) { margin: 0.2em 0; padding-left: 1.4em; }

	.feed-empty { padding: 20px; text-align: center; color: rgba(255, 255, 255, 0.2); font-size: 0.85rem; }

	.jump-btn { position: absolute; top: 44px; left: 50%; transform: translateX(-50%); background: rgba(229, 9, 20, 0.9); color: #fff; border: none; padding: 6px 16px; border-radius: 16px; font-size: 0.75rem; font-family: system-ui, -apple-system, sans-serif; cursor: pointer; z-index: 10; transition: background 0.15s; box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4); }
	.jump-btn:hover { background: #e50914; }

	/* Sessions */
	.sessions-list { flex: 1; overflow-y: auto; min-height: 0; }
	.sessions-list::-webkit-scrollbar { width: 6px; }
	.sessions-list::-webkit-scrollbar-track { background: transparent; }
	.sessions-list::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 3px; }

	.session-row { cursor: pointer; transition: background 0.1s; border-bottom: 1px solid rgba(255, 255, 255, 0.04); }
	.session-row:hover { background: rgba(255, 255, 255, 0.04); }
	.session-row.expanded { background: rgba(255, 255, 255, 0.03); }
	.session-summary { display: flex; align-items: center; gap: 12px; padding: 10px 20px; font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; font-size: 0.8rem; }
	.status-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
	.session-id { color: rgba(255, 255, 255, 0.7); flex-shrink: 0; min-width: 72px; }
	.session-status { flex-shrink: 0; min-width: 52px; font-size: 0.75rem; }
	.session-video { color: rgba(255, 255, 255, 0.35); flex-shrink: 0; }
	.film-link { color: #e50914; cursor: pointer; transition: color 0.15s; }
	.film-link:hover { color: #f40612; text-decoration: underline; }
	.session-events { color: rgba(255, 255, 255, 0.35); flex-shrink: 0; }
	.session-age { color: rgba(255, 255, 255, 0.25); margin-left: auto; }
	.session-expand { color: rgba(255, 255, 255, 0.3); font-size: 0.65rem; flex-shrink: 0; }

	.session-timeline { background: rgba(0, 0, 0, 0.3); border-top: 1px solid rgba(255, 255, 255, 0.04); padding: 6px 0; max-height: 300px; overflow-y: auto; }
	.session-timeline::-webkit-scrollbar { width: 6px; }
	.session-timeline::-webkit-scrollbar-track { background: transparent; }
	.session-timeline::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 3px; }
	.timeline-line { padding-left: 40px; }

	/* Films */
	.films-list { flex: 1; overflow-y: auto; padding: 16px 20px; min-height: 0; display: flex; flex-direction: column; gap: 16px; }
	.films-list::-webkit-scrollbar { width: 6px; }
	.films-list::-webkit-scrollbar-track { background: transparent; }
	.films-list::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 3px; }

	.film-card { background: rgba(255, 255, 255, 0.04); border: 1px solid rgba(255, 255, 255, 0.08); border-radius: 10px; padding: 20px; }
	.film-header { margin-bottom: 12px; }
	.film-title-row { display: flex; align-items: baseline; gap: 10px; }
	.film-title { font-size: 1.2rem; font-weight: 700; color: #e50914; margin: 0; }
	.film-year { font-size: 0.95rem; color: rgba(255, 255, 255, 0.5); }
	.film-director { font-size: 0.95rem; color: rgba(255, 255, 255, 0.4); }
	.film-meta-row { display: flex; gap: 16px; margin-bottom: 12px; }
	.film-meta-item { font-size: 0.78rem; color: rgba(255, 255, 255, 0.35); font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; background: rgba(255, 255, 255, 0.06); padding: 2px 8px; border-radius: 4px; }
	.film-synopsis-toggle, .film-cues-toggle { font-size: 0.78rem; color: rgba(255, 255, 255, 0.45); cursor: pointer; padding: 4px 0; transition: color 0.15s; user-select: none; }
	.film-synopsis-toggle:hover, .film-cues-toggle:hover { color: rgba(255, 255, 255, 0.7); }
	.film-synopsis { font-size: 0.82rem; color: rgba(255, 255, 255, 0.5); line-height: 1.6; margin: 6px 0 12px; padding-left: 16px; border-left: 2px solid rgba(255, 255, 255, 0.08); }

	.film-stats-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; margin: 12px 0; }
	.film-stat { display: flex; flex-direction: column; align-items: center; padding: 10px 8px; background: rgba(255, 255, 255, 0.03); border: 1px solid rgba(255, 255, 255, 0.06); border-radius: 8px; }
	.film-stat-value { font-size: 1.3rem; font-weight: 700; color: rgba(255, 255, 255, 0.8); font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }
	.film-stat-label { font-size: 0.65rem; color: rgba(255, 255, 255, 0.35); text-transform: uppercase; letter-spacing: 0.5px; margin-top: 2px; }

	.film-cues-table { margin-top: 8px; border: 1px solid rgba(255, 255, 255, 0.06); border-radius: 6px; overflow: hidden; }
	.film-cue-row { display: flex; gap: 16px; padding: 8px 12px; font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; font-size: 0.75rem; border-bottom: 1px solid rgba(255, 255, 255, 0.04); }
	.film-cue-row:last-child { border-bottom: none; }
	.film-cue-row:hover { background: rgba(255, 255, 255, 0.03); }
	.film-cue-time { color: #f5c518; flex-shrink: 0; min-width: 60px; }
	.film-cue-text { color: rgba(255, 255, 255, 0.5); line-height: 1.5; }

	/* Widget content (shared) */
	.widget-content { flex: 1; overflow-y: auto; padding: 20px; min-height: 0; }
	.widget-content::-webkit-scrollbar { width: 6px; }
	.widget-content::-webkit-scrollbar-track { background: transparent; }
	.widget-content::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 3px; }

	/* Stats grid (shared) */
	.stats-grid-4 { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; margin-bottom: 24px; }
	.stat-card { display: flex; flex-direction: column; align-items: center; padding: 16px 12px; background: rgba(255, 255, 255, 0.04); border: 1px solid rgba(255, 255, 255, 0.08); border-radius: 10px; }
	.stat-value { font-size: 1.5rem; font-weight: 700; color: rgba(255, 255, 255, 0.8); font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }
	.stat-label { font-size: 0.65rem; color: rgba(255, 255, 255, 0.35); text-transform: uppercase; letter-spacing: 0.5px; margin-top: 4px; }

	/* AI conversations */
	.ai-conversations { margin-top: 8px; }
	.ai-conversations-header { font-size: 0.7rem; font-weight: 600; letter-spacing: 2px; color: rgba(255, 255, 255, 0.4); padding-bottom: 8px; border-bottom: 1px solid rgba(255, 255, 255, 0.08); margin-bottom: 8px; }
	.ai-conversations-list { max-height: 400px; overflow-y: auto; }
	.ai-conversations-list::-webkit-scrollbar { width: 6px; }
	.ai-conversations-list::-webkit-scrollbar-track { background: transparent; }
	.ai-conversations-list::-webkit-scrollbar-thumb { background: rgba(255, 255, 255, 0.1); border-radius: 3px; }

	/* Heatmap */
	.heatmap-content { display: flex; flex-direction: column; align-items: center; }
	.heatmap-svg { width: 100%; max-width: 900px; height: auto; }
	.heatmap-legend { display: flex; gap: 20px; margin-top: 16px; justify-content: center; }
	.legend-item { display: flex; align-items: center; gap: 6px; font-size: 0.75rem; color: rgba(255, 255, 255, 0.5); font-family: 'SF Mono', 'Fira Code', 'Cascadia Code', monospace; }
	.legend-swatch { width: 12px; height: 12px; border-radius: 2px; flex-shrink: 0; }

	@media (max-width: 900px) {
		.stats-grid-4 { grid-template-columns: repeat(2, 1fr); }
	}
</style>
