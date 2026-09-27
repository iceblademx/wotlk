// Where item/spell links, hover tooltips and icons come from.
//
// - 'wowhead': wowhead.com/wotlk. Works for stock items, but knows nothing about custom items or
//   stat changes made by a private server.
// - 'aowow': an AoWoW instance (the database site most 3.3.5a servers run, e.g. https://db.example.com).
//   It serves the server's own item/spell data, so custom content gets correct tooltips and icons.
export type DatabaseSite = { kind: 'wowhead' } | { kind: 'aowow'; baseUrl: string };

export const DATABASE_SITE: DatabaseSite = { kind: 'wowhead' };

export function databaseSiteUrl(type: 'item' | 'spell' | 'quest' | 'npc' | 'zone', id: number): string | null {
	if (DATABASE_SITE.kind === 'aowow') {
		return `${DATABASE_SITE.baseUrl}/?${type}=${id}`;
	}
	return null; // caller builds the wowhead URL (it needs the language prefix)
}

export function databaseSiteIconUrl(iconLabel: string): string {
	if (DATABASE_SITE.kind === 'aowow') {
		return `${DATABASE_SITE.baseUrl}/static/images/wow/icons/large/${iconLabel}.jpg`;
	}
	return `https://wow.zamimg.com/images/wow/icons/large/${iconLabel}.jpg`;
}

// Loads the hover-tooltip script for the configured site. Both scripts scan the page for links to
// their own domain, so links built with databaseSiteUrl() get tooltips automatically.
export function loadDatabaseSiteTooltips() {
	const script = document.createElement('script');
	if (DATABASE_SITE.kind === 'aowow') {
		(window as any).aowow_tooltips = { colorlinks: true };
		script.src = `${DATABASE_SITE.baseUrl}/static/widgets/power.js`;
	} else {
		(window as any).whTooltips = { colorLinks: true };
		script.src = 'https://wow.zamimg.com/js/tooltips.js';
	}
	document.body.appendChild(script);
}
