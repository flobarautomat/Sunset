type SpeechState = 'idle' | 'speaking' | 'paused';

let currentId: string | null = null;
let state: SpeechState = 'idle';
let listeners: Array<() => void> = [];
let activeUtterance: SpeechSynthesisUtterance | null = null;

function notify() {
	for (const fn of listeners) fn();
}

function stripMarkdown(md: string): string {
	return md
		.replace(/```[\s\S]*?```/g, '')       // code blocks
		.replace(/`([^`]+)`/g, '$1')           // inline code
		.replace(/!\[.*?\]\(.*?\)/g, '')       // images
		.replace(/\[([^\]]+)\]\(.*?\)/g, '$1') // links → link text
		.replace(/#{1,6}\s+/g, '')             // headings
		.replace(/(\*\*|__)(.*?)\1/g, '$2')    // bold
		.replace(/(\*|_)(.*?)\1/g, '$2')       // italic
		.replace(/~~(.*?)~~/g, '$1')           // strikethrough
		.replace(/^\s*[-*+]\s+/gm, '')         // unordered list markers
		.replace(/^\s*\d+\.\s+/gm, '')         // ordered list markers
		.replace(/^\s*>\s+/gm, '')             // blockquotes
		.replace(/---+/g, '')                  // horizontal rules
		.replace(/\n{2,}/g, '. ')              // paragraph breaks → pause
		.replace(/\n/g, ' ')                   // remaining newlines
		.replace(/\s{2,}/g, ' ')               // collapse whitespace
		.trim();
}

export function speak(text: string, id?: string): void {
	if (!window.speechSynthesis) return;
	cancel();
	const clean = stripMarkdown(text);
	if (!clean) return;
	const utterance = new SpeechSynthesisUtterance(clean);
	utterance.rate = 0.9;
	utterance.pitch = 1.0;
	activeUtterance = utterance;
	utterance.onend = () => {
		// Only reset if this is still the active utterance and we're not paused.
		// Browsers can fire onend spuriously during pause/cancel.
		if (activeUtterance === utterance && state !== 'paused') {
			state = 'idle';
			currentId = null;
			activeUtterance = null;
			notify();
		}
	};
	currentId = id ?? null;
	state = 'speaking';
	window.speechSynthesis.speak(utterance);
	notify();
}

export function pause(): void {
	if (state === 'speaking') {
		state = 'paused';
		window.speechSynthesis?.pause();
		notify();
	}
}

export function resume(): void {
	if (state === 'paused') {
		state = 'speaking';
		window.speechSynthesis?.resume();
		notify();
	}
}

export function cancel(): void {
	state = 'idle';
	currentId = null;
	activeUtterance = null;
	window.speechSynthesis?.cancel();
	notify();
}

export function getState(): SpeechState {
	return state;
}

export function getActiveId(): string | null {
	return currentId;
}

export function onChange(fn: () => void): () => void {
	listeners.push(fn);
	return () => {
		listeners = listeners.filter((l) => l !== fn);
	};
}
