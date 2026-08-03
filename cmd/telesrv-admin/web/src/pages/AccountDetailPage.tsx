import { ArrowLeft, BadgeCheck, CircleAlert, Sparkles, Star } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { AuthorizationTable } from "../components/AuthorizationTable";
import { Alert, AuditTable, Badge, LoadingSurface, PageFrame, SectionHead, SplitLayout, Summary, UsernameCell } from "../components/ui";
import { ScamFakeActions, ScamFakeBadges } from "../components/flags";
import { ColorAction, EmojiStatusAction, SupportAction, UsernameAction } from "../components/attributes";
import { displayName, displayPhone, displayUsername, formatDate, formatUnix, toInt } from "../lib/format";
import type { Navigate } from "../routing";
import type { AccountDetail } from "../types";

export function AccountDetailPage({ id, navigate }: { id: number; navigate: Navigate }) {
  const [detail, setDetail] = useState<AccountDetail | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [months, setMonths] = useState("1");
  const [starsAmount, setStarsAmount] = useState("1000");
  const [freezeUntil, setFreezeUntil] = useState(() => toDateTimeLocal(new Date(Date.now() + 7 * 86400_000)));
  const [freezeAppealURL, setFreezeAppealURL] = useState("");

  async function load() {
    setBusy(true);
    setError("");
    try {
      const next = await api.account(id);
      setDetail(next);
      if (next.Restriction.Frozen) {
        if (next.Restriction.Until) {
          setFreezeUntil(toDateTimeLocal(new Date(next.Restriction.Until)));
        }
        setFreezeAppealURL(next.Restriction.AppealURL || "");
      }
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
    return <LoadingSurface label={busy ? "Loading account detail" : "Waiting for data"} />;
  }

  const account = detail.Account;
  return (
    <PageFrame
      title={`Account #${account.ID}`}
      eyebrow={"Account Profile"}
      actions={<button className="btn icon-text" onClick={() => navigate("/accounts")}><ArrowLeft size={15} /> {"Back to list"}</button>}
    >
      <SplitLayout
        main={
          <div className="stacked-sections">
            <section className="entity-head">
              <div>
                <div className="entity-title">{displayName(account)}</div>
                <div className="entity-subtitle">{displayUsername(account.Username) || "No username"} · {displayPhone(account.Phone) || "No phone"}</div>
                {account.Collectibles?.length > 0 && (
                  <div className="entity-subtitle">
                    <UsernameCell username="" collectibles={account.Collectibles} />
                  </div>
                )}
              </div>
              <div className="entity-badges">
                {account.PremiumUntil > 0 ? <Badge tone="good">{"Premium"}</Badge> : <Badge>{"Not premium"}</Badge>}
                {detail.Verified ? <Badge tone="good">{"Verified"}</Badge> : <Badge>{"Not verified"}</Badge>}
                <ScamFakeBadges scam={detail.Scam} fake={detail.Fake} />
                {account.Frozen ? <Badge tone="danger">{"Account frozen"}</Badge> : <Badge>{"Account active"}</Badge>}
              </div>
            </section>
            <div className="summary-grid">
              <Summary label={"User ID"} value={String(account.ID)} mono />
              <Summary label={"Last active"} value={formatUnix(detail.LastSeenAt) || "-"} />
              <Summary label={"Premium expires"} value={account.PremiumUntil > 0 ? formatUnix(account.PremiumUntil) : "None"} />
              <Summary label={"Stars balance"} value={`${detail.StarsBalance} / ${detail.StarsGranted ? "initial grant applied" : "initial grant pending"}`} />
              <Summary label={"Updated"} value={formatDate(account.UpdatedAt) || "-"} />
              <Summary label={"Authorized devices"} value={String(detail.Authorizations.length)} />
              <Summary label={"Account flags"} value={`support=${detail.Support} bot=${detail.Bot}`} />
              <Summary label={"Restriction"} value={detail.HasRestriction ? detail.Restriction.Reason || "Restricted" : "None"} />
              <Summary label={"Frozen since"} value={detail.Restriction.Since ? formatDate(detail.Restriction.Since) : "None"} />
              <Summary label={"Appeal deadline"} value={detail.Restriction.Until ? formatDate(detail.Restriction.Until) : "None"} />
              <Summary label={"Appeal URL"} value={detail.Restriction.AppealURL || "None"} />
              <Summary label={"Created"} value={formatDate(account.CreatedAt) || "-"} />
            </div>
            {detail.About && <p className="about-text">{detail.About}</p>}
            <section className="section-block">
              <SectionHead title={"Authorized Devices"} text={`${detail.Authorizations.length} authorizations`} />
              <AuthorizationTable rows={detail.Authorizations} userID={account.ID} onDone={load} />
            </section>
            <section className="section-block">
              <SectionHead title={"Recent Admin Actions"} text={"Last 30 audit rows"} />
              <AuditTable rows={detail.AuditLogs} />
            </section>
          </div>
        }
        side={
          <section className="action-dock">
            <div className="dock-title">{"Account Actions"}</div>
            <label className="duration-field">
              <span>{"Appeal deadline"}</span>
              <input
                aria-label={"Freeze appeal deadline"}
                value={freezeUntil}
                onChange={(event) => setFreezeUntil(event.target.value)}
                type="datetime-local"
              />
            </label>
            <label className="duration-field">
              <span>{"Appeal URL"}</span>
              <input
                aria-label={"Freeze appeal URL"}
                value={freezeAppealURL}
                onChange={(event) => setFreezeAppealURL(event.target.value)}
                type="url"
                placeholder="https://..."
              />
            </label>
            <ActionButton
              label={account.Frozen ? "Update freeze" : "Freeze account"}
              icon={<CircleAlert size={15} />}
              path="/api/actions/set-frozen"
              payload={() => ({
                user_id: account.ID,
                frozen: true,
                freeze_until: new Date(freezeUntil).toISOString(),
                freeze_appeal_url: freezeAppealURL.trim()
              })}
              onDone={load}
            />
            {account.Frozen && (
              <ActionButton
                label={"Unfreeze account"}
                icon={<CircleAlert size={15} />}
                path="/api/actions/set-frozen"
                payload={() => ({ user_id: account.ID, frozen: false })}
                onDone={load}
              />
            )}
            <label className="duration-field">
              <span>{"Premium duration (months)"}</span>
              <input
                aria-label={"Set premium duration in months"}
                value={months}
                onChange={(event) => setMonths(event.target.value)}
                type="number"
                min="1"
                max="120"
              />
            </label>
            <div className="action-stack">
              <ActionButton
                label={"Set premium"}
                icon={<Sparkles size={15} />}
                tone="warn"
                path="/api/actions/grant-premium"
                payload={() => ({ user_id: account.ID, months: toInt(months) })}
                onDone={load}
              />
              <ActionButton
                label={"Clear premium"}
                icon={<Sparkles size={15} />}
                tone="warn"
                path="/api/actions/grant-premium"
                payload={() => ({ user_id: account.ID, months: 0 })}
                onDone={load}
              />
              <label className="duration-field">
                <span>{"Stars to grant"}</span>
                <input
                  aria-label={"Set Stars amount to grant"}
                  value={starsAmount}
                  onChange={(event) => setStarsAmount(event.target.value)}
                  type="number"
                  min="1"
                  max="1000000000"
                />
              </label>
              <ActionButton
                label={"Grant Stars"}
                icon={<Star size={15} />}
                tone="warn"
                path="/api/actions/grant-stars"
                payload={() => ({ user_id: account.ID, amount: toInt(starsAmount) })}
                onDone={load}
              />
              <ActionButton
                label={detail.Verified ? "Clear verified" : "Set verified"}
                icon={<BadgeCheck size={15} />}
                tone="warn"
                path="/api/actions/set-verified"
                payload={() => ({ user_id: account.ID, verified: !detail.Verified })}
                onDone={load}
              />
            </div>
            <ScamFakeActions idKey="user_id" id={account.ID} path="/api/actions/set-account-flags" scam={detail.Scam} fake={detail.Fake} onDone={load} />
            <div className="dock-title">{"Attributes"}</div>
            <SupportAction id={account.ID} support={detail.Support} onDone={load} />
            <UsernameAction idKey="user_id" id={account.ID} path="/api/actions/set-account-username" current={account.Username} onDone={load} />
            <ColorAction idKey="user_id" id={account.ID} path="/api/actions/set-account-color" onDone={load} />
            <EmojiStatusAction idKey="user_id" id={account.ID} path="/api/actions/set-account-emoji-status" onDone={load} />
          </section>
        }
      />
    </PageFrame>
  );
}

function toDateTimeLocal(date: Date): string {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}
