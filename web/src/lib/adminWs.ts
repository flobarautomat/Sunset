export interface SessionWithStats {
	id: string;
	video_id: string;
	user_agent: string;
	created_at: number;
	last_seen_at: number;
	event_count: number;
	last_event_kind?: string;
}

export interface EventWithSession {
	session_id: string;
	kind: string;
	at: number;
	video_pos?: number;
	payload?: any;
}

export interface AdminCallbacks {
	onSnapshot: (sessions: SessionWithStats[], events: EventWithSession[]) => void;
	onSessionCreated: (sessionId: string, payload: any) => void;
	onEventsRecorded: (sessionId: string, events: any[]) => void;
	onSessionIdle: (sessionId: string) => void;
	onConnectionChange: (connected: boolean) => void;
}

export function createAdminWs(callbacks: AdminCallbacks): () => void {
	let ws: WebSocket | null = null;
	let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	let backoff = 1000;
	let intentionallyClosed = false;

	function connect() {
		const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
		const url = `${proto}//${window.location.host}/ws/admin`;

		ws = new WebSocket(url);

		ws.onopen = () => {
			backoff = 1000;
			callbacks.onConnectionChange(true);
		};

		ws.onmessage = (event) => {
			try {
				const msg = JSON.parse(event.data);
				switch (msg.type) {
					case 'snapshot':
						callbacks.onSnapshot(msg.sessions || [], msg.events || []);
						break;
					case 'session_created':
						// payload is already parsed by top-level JSON.parse (json.RawMessage embeds inline)
						callbacks.onSessionCreated(msg.session_id, msg.payload || {});
						break;
					case 'events_recorded':
						callbacks.onEventsRecorded(msg.session_id, msg.payload || []);
						break;
					case 'session_idle':
						callbacks.onSessionIdle(msg.session_id);
						break;
				}
			} catch {
				// Ignore malformed messages
			}
		};

		ws.onclose = () => {
			callbacks.onConnectionChange(false);
			if (!intentionallyClosed) {
				reconnectTimer = setTimeout(() => {
					backoff = Math.min(backoff * 2, 15000);
					connect();
				}, backoff);
			}
		};

		ws.onerror = () => {
			// onclose will fire after onerror
		};
	}

	connect();

	return () => {
		intentionallyClosed = true;
		if (reconnectTimer) clearTimeout(reconnectTimer);
		if (ws) ws.close();
	};
}
