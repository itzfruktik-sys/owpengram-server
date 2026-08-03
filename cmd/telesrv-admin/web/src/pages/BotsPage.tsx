import { BadgeCheck, Bot, ChevronRight, Loader2, Plus, RefreshCw, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { ScamFakeBadges } from "../components/flags";
import { displayUsername, formatDate, toInt } from "../lib/format";
import type { Navigate } from "../routing";
import type { BotListResponse } from "../types";

export function BotsPage({ navigate }: { navigate: Navigate }) {
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState("50");
  const [data, setData] = useState<BotListResponse | null>(null);
  const [cursor, setCursor] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const [ownerID, setOwnerID] = useState("");
  const [botName, setBotName] = useState("");
  const [botUsername, setBotUsername] = useState("");

  async function load(next = false) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit });
    if (q.trim()) {
      params.set("q", q.trim());
    } else if (next) {
      params.set("before_id", String(cursor));
    }
    try {
      const result = await api.bots(params);
      setData(result);
      setCursor(result.next_before_id);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load(false);
  }, []);

  const rows = data?.rows ?? [];
  const verified = rows.filter((row) => row.Verified).length;
  const systemCount = rows.filter((row) => row.System).length;

  return (
    <PageFrame
      title={"Bots"}
      eyebrow={data?.listing === false ? "Search results" : "Recently created bots"}
      actions={
        <button className="btn" type="button" onClick={() => load(false)} disabled={busy}>
          <RefreshCw size={15} /> {"Refresh"}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Bots on page"} value={String(rows.length)} />
        <Metric label={"Verified"} value={String(verified)} tone="good" />
        <Metric label={"System"} value={String(systemCount)} />
      </div>

      <section className="section-block">
        <div className="section-head">
          <div>
            <h2>{"Create a system bot"}</h2>
            <p>{"Provision a bot account owned by the given user. The token is shown once after confirmation."}</p>
          </div>
        </div>
        <div className="bot-create-fields">
          <label className="duration-field">
            <span>{"Owner user ID"}</span>
            <input
              value={ownerID}
              onChange={(event) => setOwnerID(event.target.value)}
              type="number"
              min="1"
              placeholder="123456789"
            />
          </label>
          <label className="duration-field">
            <span>{"Display name"}</span>
            <input value={botName} onChange={(event) => setBotName(event.target.value)} placeholder={"e.g. Service Bot"} maxLength={64} />
          </label>
          <label className="duration-field">
            <span>{"Username"}</span>
            <input value={botUsername} onChange={(event) => setBotUsername(event.target.value)} placeholder="my_service_bot" />
          </label>
        </div>
        <div className="bot-create-actions">
          <span className="bot-create-note">{"Username must be 5-32 characters and end with 'bot'."}</span>
          <ActionButton
            label={"Create bot"}
            icon={<Plus size={15} />}
            tone="neutral"
            path="/api/actions/create-bot"
            payload={() => ({
              owner_user_id: toInt(ownerID),
              name: botName.trim(),
              username: botUsername.trim().replace(/^@/, "")
            })}
            onDone={() => load(false)}
          />
        </div>
      </section>

      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void load(false); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={"Bot ID / username"} />
          </label>
          <label className="field-inline">
            <span>{"Limit"}</span>
            <input className="small-input" value={limit} onChange={(event) => setLimit(event.target.value)} type="number" min="1" max="100" />
          </label>
          <button className="btn primary icon-text" type="submit" disabled={busy}>
            {busy ? <Loader2 size={15} className="spin" /> : <Search size={15} />} {"Search"}
          </button>
          {data?.listing && data.has_more && (
            <button className="btn icon-text" type="button" onClick={() => load(true)} disabled={busy}>
              <ChevronRight size={15} /> {"Next page"}
            </button>
          )}
        </form>
      </QueryPanel>

      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>{"Bot ID"}</th>
              <th>{"Username"}</th>
              <th>{"Name"}</th>
              <th>{"Owner"}</th>
              <th>{"Verified"}</th>
              <th>{"Type"}</th>
              <th>{"Created"}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.ID}>
                <td className="mono">{row.ID}</td>
                <td>{displayUsername(row.Username) || "-"}</td>
                <td>{row.FirstName || "-"}</td>
                <td className="mono">{row.OwnerUserID > 0 ? row.OwnerUserID : "-"}</td>
                <td>{row.Verified ? <Badge tone="good"><BadgeCheck size={12} /> {"Verified"}</Badge> : <Badge>{"Not verified"}</Badge>} <ScamFakeBadges scam={row.Scam} fake={row.Fake} /></td>
                <td>{row.System ? <Badge tone="warn">{"System"}</Badge> : <Badge>{"User"}</Badge>}</td>
                <td>{formatDate(row.CreatedAt)}</td>
                <td><button className="row-link" onClick={() => navigate(`/bots/${row.ID}`)}><Bot size={14} /> {"Details"} <ChevronRight size={14} /></button></td>
              </tr>
            ))}
            {rows.length === 0 && <EmptyRow colSpan={8} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
