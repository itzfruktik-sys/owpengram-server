export type Navigate = (href: string) => void;

export type RouteState = {
  href: string;
  path: string;
  search: URLSearchParams;
};

export function currentRoute(): RouteState {
  return {
    href: `${window.location.pathname}${window.location.search}`,
    path: window.location.pathname,
    search: new URLSearchParams(window.location.search)
  };
}

export function routeTitle(pathname: string): string {
  // Third-party verification is tested before the official section and before
  // "/bots": three different prefixes that all read as "verification of a bot".
  if (pathname.startsWith("/bot-verification")) return "Third-party verification";
  if (pathname.startsWith("/verification")) return "Official Verification";
  if (pathname.startsWith("/collectible-usernames")) return "Collectible Usernames";
  if (pathname.startsWith("/account-ratings")) return "Account Rating";
  if (pathname.startsWith("/storage")) return "Storage";
  if (pathname.startsWith("/accounts")) return "Accounts";
  if (pathname.startsWith("/channels")) return "Supergroups and Channels";
  if (pathname.startsWith("/bots")) return "Bots";
  if (pathname.startsWith("/moderation")) return "Reports and Moderation";
  if (pathname.startsWith("/emoji")) return "Emoji";
  if (pathname.startsWith("/messages")) return "Message Audit";
	if (pathname.startsWith("/give-gifts")) return "Give Gifts";
	if (pathname.startsWith("/gifts")) return "Star Gifts";
	if (pathname.startsWith("/stickers")) return "Stickers";
	if (pathname.startsWith("/emoji")) return "Emoji";
  return "Operations Console";
}

export function routeSubtitle(pathname: string): string {
  if (pathname.startsWith("/bot-verification")) return "Console / Third-party verification";
  if (pathname.startsWith("/verification")) return "Console / Verification";
  if (pathname.startsWith("/collectible-usernames")) return "Console / Collectible usernames";
  if (pathname.startsWith("/account-ratings")) return "Console / Account rating";
  if (pathname.startsWith("/storage")) return "Console / Storage";
  if (pathname.startsWith("/accounts")) return "Console / Accounts";
  if (pathname.startsWith("/channels")) return "Console / Channels";
  if (pathname.startsWith("/bots")) return "Console / Bots";
  if (pathname.startsWith("/moderation")) return "Console / Moderation";
  if (pathname.startsWith("/emoji")) return "Console / Emoji";
  if (pathname.startsWith("/messages")) return "Console / Messages";
	if (pathname.startsWith("/give-gifts")) return "Console / Give Gifts";
	if (pathname.startsWith("/gifts")) return "Console / Star Gifts";
	if (pathname.startsWith("/stickers")) return "Console / Stickers";
	if (pathname.startsWith("/emoji")) return "Console / Emoji";
  return "Console / Overview";
}
