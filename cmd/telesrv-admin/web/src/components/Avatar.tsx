import { useEffect, useState } from "react";

// Mirrors internal/web/server.go's publicAvatarGradients + initials() exactly,
// so admin-console avatars look identical to the public preview cards.
const AVATAR_GRADIENTS: [string, string][] = [
  ["#FF885E", "#FF516A"],
  ["#FFCD6A", "#FFA85C"],
  ["#82B1FF", "#665FFF"],
  ["#A0DE7E", "#54CB68"],
  ["#53EDD6", "#28C9B7"],
  ["#72D5FD", "#2A9EF1"],
  ["#E0A2F3", "#D669ED"]
];

function avatarGradient(id: number): [string, string] {
  const n = Math.abs(id) % AVATAR_GRADIENTS.length;
  return AVATAR_GRADIENTS[n];
}

function firstCodePoint(word: string): string {
  const chars = Array.from(word);
  return chars.length > 0 ? chars[0] : "";
}

function avatarInitials(firstName: string, lastName: string, username: string): string {
  const title = `${firstName} ${lastName}`.trim();
  const words = title.split(/\s+/).filter(Boolean);
  const source = words.length > 0 ? words : (username ? [username] : []);
  if (source.length === 0) return "T";
  let out = firstCodePoint(source[0]);
  if (source.length > 1) {
    out += firstCodePoint(source[source.length - 1]);
  }
  return out.toUpperCase();
}

type AvatarKind = "user" | "channel";

// Avatar renders a user's or channel's current photo, reading through the
// admin console's proxy (/api/accounts/{id}/avatar or /api/channels/{id}/avatar).
// It falls back to a gradient-tinted initials tile — identical to the public
// preview cards — when there's no photo or the fetch fails.
export function Avatar({
  id,
  kind = "user",
  firstName = "",
  lastName = "",
  username = "",
  title = "",
  size = 34,
  refreshKey
}: {
  id: number;
  kind?: AvatarKind;
  firstName?: string;
  lastName?: string;
  username?: string;
  // title is the channel/supergroup name, used for initials when kind="channel".
  title?: string;
  size?: number;
  // refreshKey busts the browser's cached image (Cache-Control: max-age=300)
  // right after an admin-driven avatar change, so the new photo shows up
  // immediately instead of the stale cached one.
  refreshKey?: number | string;
}) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [id, kind, refreshKey]);

  if (failed) {
    const [from, to] = avatarGradient(id);
    return (
      <div
        className="avatar-fallback"
        style={{ width: size, height: size, background: `linear-gradient(135deg, ${from}, ${to})`, fontSize: Math.round(size * 0.42) }}
      >
        {kind === "channel" ? avatarInitials(title, "", username) : avatarInitials(firstName, lastName, username)}
      </div>
    );
  }

  const basePath = kind === "channel" ? `/api/channels/${id}/avatar` : `/api/accounts/${id}/avatar`;
  return (
    <img
      className="avatar-photo-img"
      src={`${basePath}${refreshKey !== undefined ? `?v=${encodeURIComponent(String(refreshKey))}` : ""}`}
      alt=""
      loading="lazy"
      style={{ width: size, height: size }}
      onError={() => setFailed(true)}
    />
  );
}
