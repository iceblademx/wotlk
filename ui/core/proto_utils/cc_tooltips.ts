// In-game style tooltips for custom 3.3.5a content. Wowhead knows nothing about custom items and
// spells, so tools/cc renders their tooltips from client/server data into cc_tooltips.json, and
// links to them show these instead of wowhead tooltips.
import tippy, { Instance } from 'tippy.js';

const tooltipsUrl = '/wotlk/assets/database/cc_tooltips.json';

interface TooltipData {
	items: Record<string, string>;
	spells: Record<string, string>;
}

let data: TooltipData = { items: {}, spells: {} };
let loadPromise: Promise<void> | null = null;

// Loads tooltip data once. A missing file just means there is no custom content.
export function loadCustomTooltips(): Promise<void> {
	if (loadPromise == null) {
		loadPromise = fetch(tooltipsUrl)
			.then(response => (response.ok ? response.json() : null))
			.then(json => {
				if (json) {
					data = { items: json.items ?? {}, spells: json.spells ?? {} };
				}
			})
			.catch(() => {});
	}
	return loadPromise;
}

export function customTooltipHtml(itemId: number, spellId: number): string | undefined {
	if (itemId) {
		return data.items[itemId];
	}
	if (spellId) {
		return data.spells[spellId];
	}
	return undefined;
}

type WithCustomTooltip = HTMLElement & { _ccTooltip?: Instance };

// Shows the custom tooltip on elem if the item/spell has one. Returns false (and removes any
// previously attached custom tooltip) otherwise, so the caller can fall back to wowhead.
export function applyCustomTooltip(elem: HTMLElement, itemId: number, spellId: number): boolean {
	const target = elem as WithCustomTooltip;
	const html = customTooltipHtml(itemId, spellId);
	if (!html) {
		target._ccTooltip?.destroy();
		target._ccTooltip = undefined;
		return false;
	}

	// Keep wowhead's script away from this element.
	delete target.dataset.wowhead;
	if (elem instanceof HTMLAnchorElement) {
		elem.href = 'javascript:void(0)';
	}

	if (target._ccTooltip) {
		target._ccTooltip.setContent(html);
	} else {
		target._ccTooltip = tippy(target, {
			content: html,
			allowHTML: true,
			theme: 'cc',
			placement: 'right',
			maxWidth: 360,
			ignoreAttributes: true,
		});
	}
	return true;
}
