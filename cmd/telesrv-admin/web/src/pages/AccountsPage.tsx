import { ChevronLeft, ChevronRight, Loader2, RefreshCw, Search, Smartphone } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Avatar } from "../components/Avatar";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel, UsernameCell } from "../components/ui";
import { ScamFakeBadges } from "../components/flags";
import { displayName, displayPhone, formatDate, formatUnix } from "../lib/format";
import { accountMetrics } from "../lib/metrics";
import type { Navigate } from "../routing";
import type { AccountListResponse, AccountStatsResponse } from "../types";

type Cursor = { beforeID: number; beforeActiveUS: number };
type AccountPageSize = 10 | 20 | 50 | 100;

const zeroCursor: Cursor = { beforeID: 0, beforeActiveUS: 0 };

export function AccountsPage({ navigate }: { navigate: Navigate }) {
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState<AccountPageSize>(50);
  const [data, setData] = useState<AccountListResponse | null>(null);
  const [stats, setStats] = useState<AccountStatsResponse | null>(null);
  // history holds the cursor used to reach every page before the current
  // one, so "Previous" can pop back without re-deriving offsets -- keyset
  // pagination has no notion of "page N" to jump back to otherwise.
  const [history, setHistory] = useState<Cursor[]>([]);
  const [cursor, setCursor] = useState<Cursor>(zeroCursor);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function fetchPage(query: string, at: Cursor) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit: String(limit) });
    if (query.trim()) {
      params.set("q", query.trim());
    }
    if (at.beforeID || at.beforeActiveUS) {
      params.set("before_id", String(at.beforeID));
      params.set("before_active_us", String(at.beforeActiveUS));
    }
    try {
      const result = await api.accounts(params);
      setData(result);
      return result;
    } catch (err) {
      setError(errorMessage(err));
      return null;
    } finally {
      setBusy(false);
    }
  }

  async function loadFresh() {
    setHistory([]);
    setCursor(zeroCursor);
    await fetchPage(q, zeroCursor);
  }

  async function loadNext() {
    if (!data?.has_more) return;
    const at = { beforeID: data.next_before_id, beforeActiveUS: data.next_before_active_us };
    const result = await fetchPage(q, at);
    if (result) {
      setHistory((prev) => [...prev, cursor]);
      setCursor(at);
    }
  }

  async function loadPrev() {
    if (history.length === 0) return;
    const at = history[history.length - 1];
    const result = await fetchPage(q, at);
    if (result) {
      setHistory((prev) => prev.slice(0, -1));
      setCursor(at);
    }
  }

  async function loadStats() {
    try {
      setStats(await api.accountStats());
    } catch {
      // Stats are a header nicety; a failure here shouldn't block the list.
    }
  }

  useEffect(() => {
    void loadFresh();
    void loadStats();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const metrics = accountMetrics(data?.rows ?? []);
  const canGoPrev = history.length > 0 && !busy;
  const canGoNext = Boolean(data?.has_more) && !busy;

  return (
    <PageFrame
      title={"Accounts"}
      eyebrow={data?.listing === false ? "Search results" : "Recently active accounts"}
      actions={
        <>
          <button className="btn icon-text" type="button" onClick={() => navigate("/accounts/shared-devices")}>
            <Smartphone size={15} /> {"Shared devices"}
          </button>
          <button
            className="btn"
            type="button"
            onClick={() => {
              void loadFresh();
              void loadStats();
            }}
            disabled={busy}
          >
            <RefreshCw size={15} /> {"Refresh"}
          </button>
        </>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Total users"} value={stats ? String(stats.total) : "…"} />
        <Metric label={"Online now"} value={stats ? String(stats.online) : "…"} tone="good" />
        <Metric label={"Online device records"} value={String(metrics.devices)} />
      </div>
      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void loadFresh(); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={"User ID / phone / username / email / name"} />
          </label>
          <label className="gift-page-size">
            <span>{"Limit"}</span>
            <select value={String(limit)} onChange={(event) => setLimit(Number(event.target.value) as AccountPageSize)}>
              <option value="10">10</option>
              <option value="20">20</option>
              <option value="50">50</option>
              <option value="100">100</option>
            </select>
          </label>
          <button className="btn primary icon-text" type="submit" disabled={busy}>
            {busy ? <Loader2 size={15} className="spin" /> : <Search size={15} />} {"Search"}
          </button>
          <button className="btn icon-text" type="button" onClick={() => void loadPrev()} disabled={!canGoPrev}>
            <ChevronLeft size={15} /> {"Previous page"}
          </button>
          <button className="btn icon-text" type="button" onClick={() => void loadNext()} disabled={!canGoNext}>
            <ChevronRight size={15} /> {"Next page"}
          </button>
        </form>
      </QueryPanel>
      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th className="avatar-col"></th>
              <th>{"User ID"}</th>
              <th>{"Phone"}</th>
              <th>{"Username"}</th>
              <th>{"Name"}</th>
              <th>{"Login email"}</th>
              <th>{"Device"}</th>
              <th>{"Last active"}</th>
              <th>{"Premium"}</th>
              <th>{"Verified"}</th>
              <th>{"Frozen"}</th>
              <th>{"Updated"}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {data?.rows.map((row) => (
              <tr key={row.ID}>
                <td className="avatar-col">
                  <button className="avatar-link" type="button" onClick={() => navigate(`/accounts/${row.ID}`)} aria-label={`Open account ${row.ID}`}>
                    <Avatar id={row.ID} firstName={row.FirstName} lastName={row.LastName} username={row.Username} />
                  </button>
                </td>
                <td className="mono">{row.ID}</td>
                <td>{displayPhone(row.Phone)}</td>
                <td><UsernameCell username={row.Username} collectibles={row.Collectibles} /></td>
                <td>{displayName(row)}</td>
                <td>{row.LoginEmail || <span className="muted-cell">{"None"}</span>}</td>
                <td>{row.DeviceCount}</td>
                <td>{formatDate(row.LastActiveAt)}</td>
                <td>{row.PremiumUntil > 0 ? <Badge tone="good">{"Premium"} {formatUnix(row.PremiumUntil)}</Badge> : <Badge>{"None"}</Badge>}</td>
                <td>{row.Verified ? <Badge tone="good">{"Verified"}</Badge> : <Badge>{"Not verified"}</Badge>} <ScamFakeBadges scam={row.Scam} fake={row.Fake} /></td>
                <td>{row.Frozen ? <Badge tone="danger">{"Frozen"}</Badge> : <Badge>{"Normal"}</Badge>}</td>
                <td>{formatDate(row.UpdatedAt)}</td>
                <td><button className="row-link" onClick={() => navigate(`/accounts/${row.ID}`)}>{"Details"} <ChevronRight size={14} /></button></td>
              </tr>
            ))}
            {(!data || data.rows.length === 0) && <EmptyRow colSpan={12} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
