import { toast } from 'svelte-sonner';

// Single source of truth for copying text to the clipboard.
// Works on both HTTPS and plain HTTP LAN installs.
// Always shows a toast so every copy button has consistent feedback.
export async function copyToClipboard(
	text: string,
	label?: string,
	selectTarget?: HTMLElement
): Promise<boolean> {
	if (!text) return false;

	// Secure contexts (HTTPS / localhost) can use the modern Clipboard API.
	if (navigator.clipboard && window.isSecureContext) {
		try {
			await navigator.clipboard.writeText(text);
			toast.success(label ? `${label} copied` : 'Copied to clipboard');
			return true;
		} catch {
			// Fall through to the user-gesture fallback below.
		}
	}

	// Fallback for plain HTTP: hook the native 'copy' event to push custom data
	// into the clipboard. ClipboardEvent.setData still works in insecure
	// contexts as long as it is triggered by a real user gesture such as a click.
	// We also focus/select a temporary input so execCommand has a selection.
	const selection = window.getSelection();
	const savedRange = selection && selection.rangeCount > 0 ? selection.getRangeAt(0) : null;
	let wrote = false;

	const onCopy = (e: ClipboardEvent) => {
		try {
			e.clipboardData?.setData('text/plain', text);
			e.preventDefault();
			wrote = true;
		} catch {
			wrote = false;
		}
	};

	document.addEventListener('copy', onCopy);

	const ta = document.createElement('textarea');
	ta.value = text;
	ta.setAttribute('readonly', '');
	ta.style.position = 'fixed';
	ta.style.left = '0';
	ta.style.top = '0';
	ta.style.width = '1px';
	ta.style.height = '1px';
	ta.style.opacity = '0';
	ta.style.pointerEvents = 'none';
	document.body.appendChild(ta);

	try {
		ta.focus({ preventScroll: true });
		ta.setSelectionRange(0, text.length);
		document.execCommand('copy');
	} finally {
		document.body.removeChild(ta);
		document.removeEventListener('copy', onCopy);
		if (savedRange) {
			selection?.removeAllRanges();
			selection?.addRange(savedRange);
		}
	}

	if (wrote) {
		toast.success(label ? `${label} copied` : 'Copied to clipboard');
		return true;
	}

	// If the browser blocked programmatic copy, select the visible text so the
	// user can press Ctrl+C / Cmd+C as a last resort.
	if (selectTarget) {
		selectElementText(selectTarget);
		toast.info(label ? `${label} selected — press Ctrl+C / Cmd+C to copy` : 'Selected — press Ctrl+C / Cmd+C to copy');
	} else {
		toast.error('Copy failed — select and copy manually');
	}
	return false;
}

export function selectElementText(el: HTMLElement) {
	const range = document.createRange();
	range.selectNodeContents(el);
	const sel = window.getSelection();
	sel?.removeAllRanges();
	sel?.addRange(range);
}
