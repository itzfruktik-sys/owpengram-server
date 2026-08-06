import { ChevronLeft, ChevronRight, Loader2, RefreshCw, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Avatar } from "../components/Avatar";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { ScamFakeBadges } from "../components/flags";
import { channelKind, displayUsername, formatDate } from "../lib/format";
import { channelMetrics } from "../lib/metrics";
import type { Navigate } from "../routing";
import type { ChannelListResponse } from "../types";

type Cursor = { beforeID: number; beforeUpdatedUS: number };
type ChannelPageSize = 10 | 20 | 50 | 100;

const zeroCursor: Cursor = { beforeID: 0, beforeUpdatedUS: 0 };

export function ChannelsPage({ navigate }: { navigate: Navigate }) {
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState<ChannelPageSize>(50);
  const [data, setData] = useState<ChannelListResponse | null>(null);
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
    if (at.beforeID || at.beforeUpdatedUS) {
      params.set("before_id", String(at.beforeID));
      params.set("before_updated_us", String(at.beforeUpdatedUS));
    }
    try {
      const result = await api.channels(params);
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
    const at = { beforeID: data.next_before_id, beforeUpdatedUS: data.next_before_updated_us };
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

  useEffect(() => {
    void loadFresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const metrics = channelMetrics(data?.rows ?? []);
  const canGoPrev = history.length > 0 && !busy;
  const canGoNext = Boolean(data?.has_more) && !busy;

  return (
    <PageFrame
      title={"Supergroups and Channels"}
      eyebrow={data?.listing === false ? "Search results" : "Recently updated"}
      actions={
        <button className="btn" type="button" onClick={() => void loadFresh()} disabled={busy}>
          <RefreshCw size={15} /> {"Refresh"}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Entities on page"} value={String(data?.rows.length ?? 0)} />
        <Metric label={"Supergroups"} value={String(metrics.megagroups)} />
        <Metric label={"Channels"} value={String(metrics.broadcasts)} />
        <Metric label={"Verified"} value={String(metrics.verified)} tone="good" />
      </div>
      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void loadFresh(); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={"Channel ID / username / title"} />
          </label>
          <label className="gift-page-size">
            <span>{"Limit"}</span>
            <select value={String(limit)} onChange={(event) => setLimit(Number(event.target.value) as ChannelPageSize)}>
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
              <th>{"Channel ID"}</th>
              <th>{"Kind"}</th>
              <th>{"Username"}</th>
              <th>{"Title"}</th>
              <th>{"Members"}</th>
              <th>{"Admins"}</th>
              <th>PTS</th>
              <th>{"Verified"}</th>
              <th>{"Updated"}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {data?.rows.map((row) => (
              <tr key={row.ID}>
                <td className="avatar-col">
                  <button className="avatar-link" type="button" onClick={() => navigate(`/channels/${row.ID}`)} aria-label={`Open channel ${row.ID}`}>
                    <Avatar id={row.ID} kind="channel" title={row.Title} />
                  </button>
                </td>
                <td className="mono">{row.ID}</td>
                <td>{channelKind(row)}</td>
                <td>{displayUsername(row.Username)}</td>
                <td>{row.Title}</td>
                <td>{row.ParticipantsCount}</td>
                <td>{row.AdminsCount}</td>
                <td>{row.PTS}</td>
                <td>{row.Verified ? <Badge tone="good">{"Verified"}</Badge> : <Badge>{"Not verified"}</Badge>} <ScamFakeBadges scam={row.Scam} fake={row.Fake} /></td>
                <td>{formatDate(row.UpdatedAt)}</td>
                <td><button className="row-link" onClick={() => navigate(`/channels/${row.ID}`)}>{"Details"} <ChevronRight size={14} /></button></td>
              </tr>
            ))}
            {(!data || data.rows.length === 0) && <EmptyRow colSpan={11} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}
