class MemoryStorage implements Storage {
	private data = new Map<string, string>();

	get length(): number {
		return this.data.size;
	}

	key(index: number): string | null {
		return Array.from(this.data.keys())[index] ?? null;
	}

	getItem(key: string): string | null {
		return this.data.get(key) ?? null;
	}

	setItem(key: string, value: string): void {
		this.data.set(key, String(value));
	}

	removeItem(key: string): void {
		this.data.delete(key);
	}

	clear(): void {
		this.data.clear();
	}
}

if (typeof globalThis.localStorage === 'undefined') {
	(globalThis as unknown as { localStorage: Storage }).localStorage = new MemoryStorage();
}

if (typeof globalThis.sessionStorage === 'undefined') {
	(globalThis as unknown as { sessionStorage: Storage }).sessionStorage = new MemoryStorage();
}
