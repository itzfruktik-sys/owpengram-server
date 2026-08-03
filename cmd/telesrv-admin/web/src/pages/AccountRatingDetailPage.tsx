import { ArrowLeft, Calculator, RefreshCw, SlidersHorizontal, User } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { Alert, Badge, EmptyRow, LoadingSurface, Metric, PageFrame, SectionHead, SplitLayout, Summary } from "../components/ui";
import { displayUsername, formatDate, formatQuantity, formatSigned, toNumeric } from "../lib/format";
import type { Navigate } from "../routing";
import type { AccountRatingDetail, AccountRatingEventKind, AccountRatingRow } from "../types";
import { LevelBadge, RatingProgress, levelProgress } from "./AccountRatingsPage";

export function AccountRatingDetailPage({ userID, navigate }: { userID: string; navigate: Navigate }) {
  const [detail, setDetail] = useState<AccountRatingDetail | null>(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [adjustment, setAdjustment] = useState("");

  async function load() {
    setBusy(true);
    setError("");
    try {
      setDetail(await api.accountRating(userID));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load();
  }, [userID]);

  if (error && !detail) {
    return <Alert>{error}</Alert>;
  }
  if (!detail) {
    return <LoadingSurface label={busy ? "Loading account rating…" : "Waiting for data"} />;
  }

  const rating = detail.rating;
  const events = detail.events ?? [];
  const pending = toNumeric(rating.PendingStars);
  const progress = levelProgress(rating);
  // user_id / amount are `,string` int64 fields on the backend, so they stay
  // decimal strings and never pass through a float.
  const payloadUserID = rating.UserID || userID;

  return (
    <PageFrame
      title={`Rating of ${displayUsername(rating.Username) || rating.FirstName || rating.UserID}`}
      eyebrow={"Rating / Component breakdown"}
      actions={
        <>
          <button className="btn icon-text" type="button" onClick={() => navigate("/account-ratings")}>
            <ArrowLeft size={15} /> {"Back to list"}
          </button>
          <button className="btn icon-text" type="button" onClick={load} disabled={busy}>
            <RefreshCw size={15} className={busy ? "spin" : ""} /> {"Refresh"}
          </button>
        </>
      }
    >
      {error && <Alert>{error}</Alert>}
      <SplitLayout
        main={
          <div className="stacked-sections">
            <section className="entity-head">
              <div>
                <div className="entity-title">{displayUsername(rating.Username) || rating.FirstName || "Unnamed bot"}</div>
                <div className="entity-subtitle">{"User ID"}: {rating.UserID}</div>
              </div>
              <div className="entity-badges">
                <LevelBadge level={rating.Level} />
                {pending !== 0 && <Badge tone="warn">{`Pending ${formatSigned(rating.PendingStars)}`}</Badge>}
              </div>
            </section>

            <div className="metric-row">
              <Metric label={"Points"} value={formatQuantity(rating.Stars)} mono />
              <Metric label={"Level"} value={String(rating.Level)} tone="good" />
              <Metric
                label={"Next level threshold"}
                value={rating.HasNextLevel ? formatQuantity(rating.NextLevelStars) : "Max level reached"}
                mono={rating.HasNextLevel}
              />
              <Metric
                label={"Points to next level"}
                value={rating.HasNextLevel ? formatQuantity(String(progress.remaining)) : "-"}
                mono
                tone={rating.HasNextLevel && progress.percent >= 80 ? "good" : "neutral"}
              />
            </div>

            <section className="section-block">
              <SectionHead title={"How the rating adds up"} text={"Contribution of every source: stars, activity, moderation penalties and manual corrections."} />
              <Breakdown rating={rating} />
              <div className="summary-grid">
                <Summary label={"Current level threshold"} value={formatQuantity(rating.CurrentLevelStars)} mono />
                <Summary
                  label={"Next level threshold"}
                  value={rating.HasNextLevel ? formatQuantity(rating.NextLevelStars) : "Max level reached"}
                  mono={rating.HasNextLevel}
                />
                <Summary label={"Computed"} value={formatDate(rating.ComputedAt) || "-"} />
                <Summary label={"Updated"} value={formatDate(rating.UpdatedAt) || "-"} />
              </div>
              <div className="progress-wide">
                <RatingProgress row={rating} />
              </div>
            </section>

            {pending !== 0 && (
              <section className="section-block">
                <SectionHead title={"Pending points"} text={"Already earned, but counted towards the rating only on the date below."} />
                <div className="summary-grid">
                  <Summary label={"Pending"} value={formatSigned(rating.PendingStars)} mono />
                  <Summary label={"Applied on"} value={formatDate(rating.PendingDate) || "-"} />
                </div>
              </section>
            )}

            <section className="section-block">
              <SectionHead title={"Rating events"} text={"Every rating change with its source, actor and reason."} />
              <div className="table-wrap">
                <table className="data-table">
                  <thead>
                    <tr>
                      <th>{"ID"}</th>
                      <th>{"Source"}</th>
                      <th>{"Change"}</th>
                      <th>{"Reason"}</th>
                      <th>{"Actor"}</th>
                      <th>{"Time"}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {events.map((row) => (
                      <tr key={row.ID}>
                        <td className="mono">{row.ID}</td>
                        <td><EventKind kind={row.Kind} /></td>
                        <td className="mono">{formatSigned(row.Amount)}</td>
                        <td className="truncate">{row.Reason || "-"}</td>
                        <td>{row.Actor || "-"}</td>
                        <td>{formatDate(row.CreatedAt) || "-"}</td>
                      </tr>
                    ))}
                    {events.length === 0 && <EmptyRow colSpan={6} />}
                  </tbody>
                </table>
              </div>
            </section>
          </div>
        }
        side={
          <section className="action-dock">
            <div className="dock-title">{"Rating operations"}</div>
            <button className="btn icon-text" type="button" onClick={() => navigate(`/accounts/${rating.UserID}`)}>
              <User size={15} /> {"Open account"}
            </button>
            <div className="action-stack">
              <ActionButton
                label={"Recompute"}
                icon={<Calculator size={15} />}
                tone="neutral"
                path="/api/actions/recompute-account-rating"
                payload={() => ({ user_id: payloadUserID })}
                onDone={load}
              />
            </div>
            <p className="bot-create-note">{"Rebuilds the rating from stars, activity, penalties and manual corrections."}</p>
            <div className="dock-title">{"Manual correction"}</div>
            <label className="duration-field">
              <span>{"Value (negative allowed)"}</span>
              <input
                value={adjustment}
                onChange={(event) => setAdjustment(event.target.value)}
                type="number"
                step="1"
                placeholder="-500"
              />
            </label>
            <div className="action-stack">
              <ActionButton
                label={"Apply correction"}
                icon={<SlidersHorizontal size={15} />}
                tone="warn"
                path="/api/actions/adjust-account-rating"
                payload={() => ({
                  user_id: payloadUserID,
                  amount: String(Number.parseInt(adjustment.trim() || "0", 10) || 0)
                })}
                onDone={() => {
                  setAdjustment("");
                  void load();
                }}
              />
            </div>
            <p className="bot-create-note">{"The value is added to the manual component; a negative number lowers the rating."}</p>
          </section>
        }
      />
    </PageFrame>
  );
}

function Breakdown({ rating }: { rating: AccountRatingRow }) {
  // PenaltyComponent is stored as a positive magnitude and subtracted by the
  // scorer, so it is shown (and summed) as a negative contribution.
  const components = [
    { key: "stars", label: "Stars", hint: "Purchased and received stars", value: toNumeric(rating.StarsComponent) },
    { key: "activity", label: "Activity", hint: "Messages, sessions and long-term engagement", value: toNumeric(rating.ActivityComponent) },
    { key: "penalty", label: "Penalties", hint: "Moderation decisions and restrictions", value: -toNumeric(rating.PenaltyComponent) },
    { key: "manual", label: "Manual corrections", hint: "Adjustments made by admins", value: toNumeric(rating.ManualComponent) }
  ];
  const scale = Math.max(1, ...components.map((item) => Math.abs(item.value)));
  // The score is clamped at zero, and a delayed increase sits in PendingStars
  // instead of the score, so both cases are expected rather than drift.
  const sum = Math.max(0, components.reduce((total, item) => total + item.value, 0));
  const total = toNumeric(rating.Stars);
  const pending = toNumeric(rating.PendingStars);

  return (
    <>
      <div className="breakdown-list">
        {components.map((item) => {
          const percent = Math.min(100, (Math.abs(item.value) / scale) * 100);
          const tone = item.value < 0 ? "danger" : item.value > 0 ? "good" : "";
          return (
            <div className="breakdown-row" key={item.key}>
              <div className="breakdown-label">
                <strong>{item.label}</strong>
                <small>{item.hint}</small>
              </div>
              <div className={`progress-bar ${tone}`} role="img" aria-label={String(item.value)}>
                <span style={{ width: `${percent}%` }} />
              </div>
              <div className={`breakdown-value mono ${tone}`}>{formatSigned(String(item.value))}</div>
            </div>
          );
        })}
        <div className="breakdown-row total">
          <div className="breakdown-label"><strong>{"Total rating"}</strong></div>
          <div className="breakdown-value mono">{formatQuantity(rating.Stars)}</div>
        </div>
      </div>
      {pending === 0 && sum !== total && (
        <Alert>{`Components add up to ${formatQuantity(String(sum))} while the stored rating is ${formatQuantity(rating.Stars)}. Recompute to resolve the drift.`}</Alert>
      )}
      {pending !== 0 && <p className="bot-create-note">{`Components already include ${formatSigned(rating.PendingStars)} that reaches the score only on the date below.`}</p>}
    </>
  );
}

const ratingKindLabels: Record<AccountRatingEventKind, string> = {
  stars: "Stars",
  activity: "Activity",
  moderation: "Moderation",
  manual: "Manual",
  recompute: "Recompute"
};

function EventKind({ kind }: { kind: AccountRatingEventKind }) {
  const tone = kind === "moderation" ? "danger" : kind === "manual" ? "warn" : kind === "recompute" ? "neutral" : "good";
  return <Badge tone={tone}>{ratingKindLabels[kind]}</Badge>;
}
