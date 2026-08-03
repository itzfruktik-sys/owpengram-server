import { ArrowLeft, BadgeCheck, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { Alert, AuditTable, Badge, LoadingSurface, PageFrame, SectionHead, SplitLayout, Summary } from "../components/ui";
import { ScamFakeActions, ScamFakeBadges } from "../components/flags";
import { ColorAction, EmojiStatusAction, UsernameAction } from "../components/attributes";
import { displayUsername, formatDate } from "../lib/format";
import type { Navigate } from "../routing";
import type { BotDetail } from "../types";

export function BotDetailPage({ id, navigate }: { id: number; navigate: Navigate }) {
  const [detail, setDetail] = useState<BotDetail | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function load() {
    setBusy(true);
    setError("");
    try {
      setDetail(await api.bot(id));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load();
  }, [id]);

  if (error) {
    return <Alert>{error}</Alert>;
  }
  if (!detail) {
    return <LoadingSurface label={busy ? "Loading bot detail" : "Waiting for data"} />;
  }

  const bot = detail.Bot;
  return (
    <PageFrame
      title={`Bot #${bot.ID}`}
      eyebrow={"Bot Profile"}
      actions={<button className="btn icon-text" onClick={() => navigate("/bots")}><ArrowLeft size={15} /> {"Back to list"}</button>}
    >
      <SplitLayout
        main={
          <div className="stacked-sections">
            <section className="entity-head">
              <div>
                <div className="entity-title">{bot.FirstName || "Unnamed bot"}</div>
                <div className="entity-subtitle">{displayUsername(bot.Username) || "No username"}</div>
              </div>
              <div className="entity-badges">
                <Badge tone={bot.System ? "warn" : "neutral"}>{bot.System ? "System" : "User"}</Badge>
                {bot.Verified ? <Badge tone="good">{"Verified"}</Badge> : <Badge>{"Not verified"}</Badge>}
                <ScamFakeBadges scam={bot.Scam} fake={bot.Fake} />
              </div>
            </section>
            <div className="summary-grid">
              <Summary label={"Bot ID"} value={String(bot.ID)} mono />
              <Summary label={"Owner"} value={bot.OwnerUserID > 0 ? `${bot.OwnerUserID} ${displayUsername(detail.OwnerUsername)}`.trim() : "None"} />
              <Summary label={"Type"} value={bot.System ? "System" : "User"} />
              <Summary label={"Updated"} value={formatDate(bot.UpdatedAt) || "-"} />
              <Summary label={"Created"} value={formatDate(bot.CreatedAt) || "-"} />
            </div>
            {detail.About && <p className="about-text">{detail.About}</p>}
            {detail.Description && detail.Description.trim() !== detail.About.trim() && <p className="about-text">{detail.Description}</p>}
            <section className="section-block">
              <SectionHead title={"Recent Admin Actions"} text={"Last 30 audit rows"} />
              <AuditTable rows={detail.AuditLogs} />
            </section>
          </div>
        }
        side={
          <section className="action-dock">
            <div className="dock-title">{"Bot Actions"}</div>
            <div className="action-stack">
              <ActionButton
                label={bot.Verified ? "Clear verified" : "Set verified"}
                icon={<BadgeCheck size={15} />}
                tone="neutral"
                path="/api/actions/set-verified"
                payload={() => ({ user_id: bot.ID, verified: !bot.Verified })}
                onDone={load}
              />
            </div>
            <ScamFakeActions idKey="user_id" id={bot.ID} path="/api/actions/set-account-flags" scam={bot.Scam} fake={bot.Fake} onDone={load} />
            <div className="dock-title">{"Attributes"}</div>
            <UsernameAction idKey="user_id" id={bot.ID} path="/api/actions/set-account-username" current={bot.Username} onDone={load} />
            <ColorAction idKey="user_id" id={bot.ID} path="/api/actions/set-account-color" onDone={load} />
            <EmojiStatusAction idKey="user_id" id={bot.ID} path="/api/actions/set-account-emoji-status" onDone={load} />
            {bot.System ? (
              <p className="bot-create-note">{"System bots are built in and cannot be deleted."}</p>
            ) : (
              <div className="danger-zone">
                <ActionButton
                  label={"Delete bot"}
                  icon={<Trash2 size={15} />}
                  tone="danger"
                  path="/api/actions/delete-bot"
                  payload={() => ({ bot_user_id: bot.ID })}
                  onDone={() => navigate("/bots")}
                />
                <p className="bot-create-note">{"Permanently deletes this user-created bot and invalidates its token. This cannot be undone."}</p>
              </div>
            )}
          </section>
        }
      />
    </PageFrame>
  );
}
