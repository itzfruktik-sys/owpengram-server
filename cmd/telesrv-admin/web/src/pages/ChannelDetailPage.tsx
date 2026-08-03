import { ArrowLeft, BadgeCheck } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { Alert, AuditTable, Badge, JsonBlock, LoadingSurface, PageFrame, SectionHead, SplitLayout, Summary } from "../components/ui";
import { ScamFakeActions, ScamFakeBadges } from "../components/flags";
import { ChannelSettingsAction, ColorAction, EmojiStatusAction, UsernameAction } from "../components/attributes";
import { channelKind, displayUsername, formatDate, formatUnix } from "../lib/format";
import type { Navigate } from "../routing";
import type { ChannelDetail } from "../types";

export function ChannelDetailPage({ id, navigate }: { id: number; navigate: Navigate }) {
  const [detail, setDetail] = useState<ChannelDetail | null>(null);
  const [error, setError] = useState("");

  async function load() {
    setError("");
    try {
      setDetail(await api.channel(id));
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  useEffect(() => {
    void load();
  }, [id]);

  if (error) {
    return <Alert>{error}</Alert>;
  }
  if (!detail) {
    return <LoadingSurface label={"Loading channel detail"} />;
  }

  const ch = detail.Channel;
  return (
    <PageFrame
      title={`${channelKind(ch)} #${ch.ID}`}
      eyebrow={"Channel Profile"}
      actions={<button className="btn icon-text" onClick={() => navigate("/channels")}><ArrowLeft size={15} /> {"Back to list"}</button>}
    >
      <SplitLayout
        main={
          <div className="stacked-sections">
            <section className="entity-head">
              <div>
                <div className="entity-title">{ch.Title || "-"}</div>
                <div className="entity-subtitle">{displayUsername(ch.Username) || "No username"} · {`Creator ${ch.CreatorUserID}`}</div>
              </div>
              <div className="entity-badges">
                <Badge>{channelKind(ch)}</Badge>
                {ch.Verified ? <Badge tone="good">{"Verified"}</Badge> : <Badge>{"Not verified"}</Badge>}
                <ScamFakeBadges scam={ch.Scam} fake={ch.Fake} />
                {ch.Deleted ? <Badge tone="danger">{"Deleted"}</Badge> : <Badge>{"Valid"}</Badge>}
              </div>
            </section>
            <div className="summary-grid">
              <Summary label={"Channel ID"} value={String(ch.ID)} mono />
              <Summary label="access_hash" value={String(ch.AccessHash)} mono />
              <Summary label={"Members"} value={`${ch.ParticipantsCount} / ${"Admins"} ${ch.AdminsCount}`} />
              <Summary label={"Moderation"} value={`Banned ${ch.BannedCount} / Kicked ${ch.KickedCount}`} />
              <Summary label={"Channel flags"} value={`broadcast=${ch.Broadcast} megagroup=${ch.Megagroup} forum=${ch.Forum}`} />
              <Summary label="top / pinned / PTS" value={`${ch.TopMessageID} / ${ch.PinnedMessageID} / ${ch.PTS}`} />
              <Summary label={"Created"} value={formatUnix(ch.Date) || "-"} />
              <Summary label={"Updated"} value={formatDate(ch.UpdatedAt) || "-"} />
            </div>
            {ch.About && <p className="about-text">{ch.About}</p>}
            <section className="section-block">
              <SectionHead title={"Recent Admin Actions"} text={"Last 30 audit rows"} />
              <AuditTable rows={detail.AuditLogs} />
            </section>
            <section className="section-block">
              <SectionHead title={"Channel Raw Row"} text={"Database read-only snapshot"} />
              <JsonBlock value={detail.ChannelJSON} />
            </section>
          </div>
        }
        side={
          <section className="action-dock">
            <div className="dock-title">{"Channel Actions"}</div>
            <ActionButton
              label={ch.Verified ? "Clear verified" : "Set verified"}
              icon={<BadgeCheck size={15} />}
              tone="warn"
              path="/api/actions/set-channel-verified"
              payload={() => ({ channel_id: ch.ID, verified: !ch.Verified })}
              onDone={load}
            />
            <ScamFakeActions idKey="channel_id" id={ch.ID} path="/api/actions/set-channel-flags" scam={ch.Scam} fake={ch.Fake} onDone={load} />
            <div className="dock-title">{"Settings"}</div>
            <ChannelSettingsAction channel={ch} onDone={load} />
            <div className="dock-title">{"Attributes"}</div>
            <UsernameAction idKey="channel_id" id={ch.ID} path="/api/actions/set-channel-username" current={ch.Username} onDone={load} />
            <ColorAction idKey="channel_id" id={ch.ID} path="/api/actions/set-channel-color" onDone={load} />
            <EmojiStatusAction idKey="channel_id" id={ch.ID} path="/api/actions/set-channel-emoji-status" onDone={load} />
          </section>
        }
      />
    </PageFrame>
  );
}
