export interface ChatMessage {
	role: 'user' | 'assistant';
	content: string;
}

export async function sendMessage(
	sessionId: string,
	message: string,
	videoPos: number,
	history: ChatMessage[],
	onToken: (token: string) => void,
	onDone: (fullText: string) => void,
	onError: (error: string) => void
): Promise<void> {
	let response: Response;
	try {
		response = await fetch('/api/chat', {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				session_id: sessionId,
				message,
				video_pos: videoPos,
				history
			})
		});
	} catch {
		onError('Failed to connect to server');
		return;
	}

	if (!response.ok) {
		const text = await response.text();
		onError(text || `Server error: ${response.status}`);
		return;
	}

	const reader = response.body?.getReader();
	if (!reader) {
		onError('No response stream');
		return;
	}

	const decoder = new TextDecoder();
	let buffer = '';
	let accumulated = '';

	while (true) {
		const { done, value } = await reader.read();
		if (done) break;

		buffer += decoder.decode(value, { stream: true });

		const frames = buffer.split('\n\n');
		// Last element may be incomplete — keep it in buffer
		buffer = frames.pop() ?? '';

		for (const frame of frames) {
			const lines = frame.split('\n');
			let eventType = '';
			let data = '';

			for (const line of lines) {
				if (line.startsWith('event: ')) {
					eventType = line.slice(7).trim();
				} else if (line.startsWith('data: ')) {
					data = line.slice(6);
				}
			}

			if (eventType === 'error') {
				try {
					const parsed = JSON.parse(data);
					onError(parsed.error || 'Unknown error');
				} catch {
					onError(data || 'Unknown error');
				}
				return;
			}

			if (eventType === 'done') {
				onDone(accumulated);
				return;
			}

			if (data) {
				try {
					const parsed = JSON.parse(data);
					if (parsed.content) {
						accumulated += parsed.content;
						onToken(parsed.content);
					}
				} catch {
					// skip malformed data
				}
			}
		}
	}

	// Stream ended without explicit done event
	if (accumulated) {
		onDone(accumulated);
	}
}
