type SpeechState = 'idle' | 'speaking' | 'paused';
type PlaybackMode = 'browser' | 'audio';

let currentId: string | null = null;
let state: SpeechState = 'idle';
let mode: PlaybackMode = 'browser';
let listeners: Array<() => void> = [];
let activeUtterance: SpeechSynthesisUtterance | null = null;
let activeAudio: HTMLAudioElement | null = null;

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

// Speak text via browser speechSynthesis
export function speak(text: string, id?: string): void {
	if (!window.speechSynthesis) return;
	cancel();
	const clean = stripMarkdown(text);
	if (!clean) return;
	const utterance = new SpeechSynthesisUtterance(clean);
	utterance.rate = 0.9;
	utterance.pitch = 1.0;
	activeUtterance = utterance;
	mode = 'browser';
	utterance.onend = () => {
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

// Play mp3 audio from a blob
export function speakAudio(blob: Blob, id?: string): void {
	cancel();
	const url = URL.createObjectURL(blob);
	const audio = new Audio(url);
	activeAudio = audio;
	mode = 'audio';
	audio.onended = () => {
		if (activeAudio === audio && state !== 'paused') {
			state = 'idle';
			currentId = null;
			activeAudio = null;
			URL.revokeObjectURL(url);
			notify();
		}
	};
	currentId = id ?? null;
	state = 'speaking';
	audio.play();
	notify();
}

export function pause(): void {
	if (state === 'speaking') {
		state = 'paused';
		if (mode === 'browser') {
			window.speechSynthesis?.pause();
		} else if (activeAudio) {
			activeAudio.pause();
		}
		notify();
	}
}

export function resume(): void {
	if (state === 'paused') {
		state = 'speaking';
		if (mode === 'browser') {
			window.speechSynthesis?.resume();
		} else if (activeAudio) {
			activeAudio.play();
		}
		notify();
	}
}

export function cancel(): void {
	state = 'idle';
	currentId = null;
	if (activeUtterance) {
		activeUtterance = null;
		window.speechSynthesis?.cancel();
	}
	if (activeAudio) {
		activeAudio.pause();
		URL.revokeObjectURL(activeAudio.src);
		activeAudio = null;
	}
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
