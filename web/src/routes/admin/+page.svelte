<script lang="ts">
	import { onMount, tick } from 'svelte';
	import { createAdminWs, type SessionWithStats, type EventWithSession } from '$lib/adminWs';
	import snarkdown from 'snarkdown';

	interface FeedEntry {
		id: string;
		timestamp: number;
		sessionId: string;
		kind: string;
		videoPos?: number;
		payload?: any;
	}

	interface SessionDetail {
		session: SessionWithStats;
		events: FeedEntry[];
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
		// Newest first — prepend new entries
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

	let filteredFeed = $derived(
		showHeartbeats ? feedEvents : feedEvents.filter(e => e.kind !== 'heartbeat')
	);

	let sortedSessions = $derived(
		[...sessions.values()].sort((a, b) => b.last_seen_at - a.last_seen_at)
	);

	onMount(() => {
		const cleanup = createAdminWs({
			onSnapshot(snapshotSessions, snapshotEvents) {
				const map = new Map<string, SessionWithStats>();
				for (const s of snapshotSessions) map.set(s.id, s);
				sessions = map;

				feedCounter = 0;
				// snapshotEvents arrive chronological (oldest first) — reverse for newest-first display
				const entries = snapshotEvents.map(e => toFeedEntry(e)).reverse();
				feedEvents = entries.slice(0, 500);

				tick().then(() => {
					if (feedEl) feedEl.scrollTop = 0;
				});
			},
			onSessionCreated(sessionId, payload) {
				const now = Date.now();
				const newSession: SessionWithStats = {
					id: sessionId,
					video_id: payload.video_id || 'default',
					user_agent: payload.user_agent || '',
					created_at: now,
					last_seen_at: now,
					event_count: 0
				};
				sessions = new Map(sessions).set(sessionId, newSession);
				idleSessions = new Set([...idleSessions].filter(id => id !== sessionId));

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

				// Update session stats
				const existing = sessions.get(sessionId);
				if (existing) {
					const updated = {
						...existing,
						event_count: existing.event_count + events.length,
						last_seen_at: Date.now(),
						last_event_kind: events[events.length - 1]?.kind
					};
					sessions = new Map(sessions).set(sessionId, updated);
				}
				idleSessions = new Set([...idleSessions].filter(id => id !== sessionId));

				// Update expanded session detail if open
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
		<div class="header-right">
			<span class="connection-dot" class:connected title={connected ? 'Connected' : 'Disconnected'}></span>
			<span class="connection-label">{connected ? 'live' : 'offline'}</span>
		</div>
	</header>

	<div class="panels">
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
	</div>

	<nav class="tab-nav">
		<a href="/watch" class="tab">
			<svg viewBox="0 0 24 24" fill="currentColor" width="16" height="16"><polygon points="5,3 19,12 5,21" /></svg>
			Watch
		</a>
		<a href="/admin" class="tab active" aria-current="page">
			<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
			Dashboard
		</a>
	</nav>
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

	.header-right {
		display: flex;
		align-items: center;
		gap: 10px;
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

	/* Panels */
	.panels {
		flex: 1;
		display: flex;
		flex-direction: column;
		min-height: 0;
	}

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
		border-bottom: 1px solid rgba(255, 255, 255, 0.08);
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
	.sessions-panel {
		flex: 1;
	}

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

	/* Expanded session timeline */
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

	/* Tab nav */
	.tab-nav {
		flex-shrink: 0;
		display: flex;
		gap: 2px;
		padding: 8px 16px;
		background: rgba(0, 0, 0, 0.4);
		border-top: 1px solid rgba(255, 255, 255, 0.08);
	}

	.tab {
		display: flex;
		align-items: center;
		gap: 5px;
		padding: 8px 12px;
		border-radius: 8px;
		font-family: system-ui, -apple-system, sans-serif;
		font-size: 0.8rem;
		font-weight: 500;
		color: rgba(255, 255, 255, 0.45);
		text-decoration: none;
		transition: color 0.15s ease, background 0.15s ease;
		white-space: nowrap;
	}

	.tab:hover {
		color: rgba(255, 255, 255, 0.7);
		background: rgba(255, 255, 255, 0.06);
	}

	.tab.active {
		color: #fff;
		background: rgba(255, 255, 255, 0.1);
	}

	.tab svg {
		opacity: 0.8;
	}
</style>
