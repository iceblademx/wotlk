// In-game style tooltips for custom 3.3.5a content. Wowhead knows nothing about custom items and
// spells, so tools/cc renders their tooltips from client/server data into cc_tooltips.json, and
// links to them show these instead of wowhead tooltips.
//
// Item tooltips mark their gear-dependent parts with data-cc-* attributes; those are filled in each
// time the tooltip opens, from the context the UI already attaches for wowhead (data-wowhead:
// gems=..&ench=..&pcs=..&sock, see Player.setWowheadData).
import tippy, { Instance } from 'tippy.js';

import { databaseSiteIconUrl } from '../constants/database_site.js';
import { GemColor } from '../proto/common.js';
import type { Database } from './database.js';
import { gemMatchesSocket } from './gems.js';

const tooltipsUrl = '/wotlk/assets/database/cc_tooltips.json';

const colorGreen = '#1eff00';
const colorGray = '#9d9d9d';
const colorEquippedPiece = '#ffff98';

interface TooltipData {
	items: Record<string, string>;
	spells: Record<string, string>;
	gems: Record<string, string>;
	enchants: Record<string, string>;
}

let data: TooltipData = { items: {}, spells: {}, gems: {}, enchants: {} };
let loadPromise: Promise<void> | null = null;
let database: Database | null = null;

// Loads tooltip data once. A missing file just means there is no custom content.
export function loadCustomTooltips(): Promise<void> {
	if (loadPromise == null) {
		loadPromise = fetch(tooltipsUrl)
			.then(response => (response.ok ? response.json() : null))
			.then(json => {
				if (json) {
					data = { items: json.items ?? {}, spells: json.spells ?? {}, gems: json.gems ?? {}, enchants: json.enchants ?? {} };
				}
			})
			.catch(() => {});
	}
	return loadPromise;
}

// Gives the tooltips access to gem data (colors, icons) once the database has loaded.
export function setCustomTooltipDatabase(db: Database) {
	database = db;
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

interface GearContext {
	gems: Array<number>;
	enchant: number;
	equippedItems: Set<number>;
	extraSocket: boolean;
}

function parseGearContext(elem: HTMLElement): GearContext {
	const ctx: GearContext = { gems: [], enchant: 0, equippedItems: new Set(), extraSocket: false };
	for (const part of (elem.dataset.wowhead ?? '').split('&')) {
		const [key, value] = part.split('=');
		if (key == 'gems' && value) {
			ctx.gems = value.split(':').map(Number);
		} else if (key == 'ench' && value) {
			ctx.enchant = Number(value);
		} else if (key == 'pcs' && value) {
			ctx.equippedItems = new Set(value.split(':').map(Number));
		} else if (key == 'sock') {
			ctx.extraSocket = true;
		}
	}
	return ctx;
}

function gemLine(icon: string, text: string): DocumentFragment {
	const frag = document.createDocumentFragment();
	const img = document.createElement('img');
	img.src = databaseSiteIconUrl(icon);
	img.style.cssText = 'width:14px;height:14px;vertical-align:middle;margin-right:4px;border-radius:2px';
	frag.append(img, document.createTextNode(text));
	return frag;
}

// Fills the gear-dependent parts of an item tooltip.
export function renderCustomTooltip(html: string, ctx: GearContext): string {
	const template = document.createElement('template');
	template.innerHTML = html;
	const root = template.content;

	const enchantElem = root.querySelector<HTMLElement>('[data-cc-enchant]');
	if (enchantElem && ctx.enchant && data.enchants[ctx.enchant]) {
		enchantElem.textContent = data.enchants[ctx.enchant];
	}

	const sockets = Array.from(root.querySelectorAll<HTMLElement>('[data-cc-socket]'));
	if (ctx.extraSocket && sockets.length > 0) {
		// Belt buckle / blacksmithing socket: an extra prismatic socket after the item's own.
		const extra = sockets[sockets.length - 1].cloneNode() as HTMLElement;
		extra.dataset.ccSocket = String(GemColor.GemColorPrismatic);
		extra.textContent = 'Prismatic Socket';
		sockets[sockets.length - 1].after(extra);
		sockets.push(extra);
	}
	let bonusActive = sockets.length > 0;
	sockets.forEach((socketElem, i) => {
		const socketColor = Number(socketElem.dataset.ccSocket) as GemColor;
		const gem = ctx.gems[i] && database ? database.lookupGem(ctx.gems[i]) : null;
		if (!gem) {
			bonusActive = false;
			return;
		}
		// Same rule the sim uses for socket bonuses (prismatic sockets accept any color, etc.).
		if (!gemMatchesSocket(gem, socketColor)) {
			bonusActive = false;
		}
		socketElem.style.color = '#ffffff';
		socketElem.replaceChildren(gemLine(gem.icon, data.gems[gem.id] ?? gem.name));
	});
	const bonusElem = root.querySelector<HTMLElement>('[data-cc-socket-bonus]');
	if (bonusElem) {
		bonusElem.style.color = bonusActive ? colorGreen : colorGray;
	}

	const header = root.querySelector<HTMLElement>('[data-cc-set-header]');
	if (header) {
		let equipped = 0;
		root.querySelectorAll<HTMLElement>('[data-cc-set-piece]').forEach(pieceElem => {
			if (ctx.equippedItems.has(Number(pieceElem.dataset.ccSetPiece))) {
				equipped++;
				pieceElem.style.color = colorEquippedPiece;
			}
		});
		header.textContent = `${header.dataset.name} (${equipped}/${header.dataset.total})`;
		root.querySelectorAll<HTMLElement>('[data-cc-set-bonus]').forEach(bonus => {
			bonus.style.color = equipped >= Number(bonus.dataset.ccSetBonus) ? colorGreen : colorGray;
		});
	}

	const container = document.createElement('div');
	container.append(root);
	return container.innerHTML;
}

type WithCustomTooltip = HTMLElement & { _ccTooltip?: Instance; _ccTooltipHtml?: string };

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

	// Without a wowhead URL, wowhead's script ignores the element; data-wowhead stays as gear context.
	if (elem instanceof HTMLAnchorElement) {
		elem.href = 'javascript:void(0)';
	}

	target._ccTooltipHtml = html;
	if (!target._ccTooltip) {
		target._ccTooltip = tippy(target, {
			content: html,
			allowHTML: true,
			theme: 'cc',
			placement: 'right',
			maxWidth: 360,
			ignoreAttributes: true,
			onShow(instance) {
				instance.setContent(renderCustomTooltip(target._ccTooltipHtml!, parseGearContext(target)));
			},
		});
	}
	return true;
}
