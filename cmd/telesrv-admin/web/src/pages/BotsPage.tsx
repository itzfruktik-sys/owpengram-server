import { BadgeCheck, Bot, ChevronLeft, ChevronRight, Loader2, Plus, RefreshCw, Search } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Avatar } from "../components/Avatar";
import { CreateBotModal } from "../components/CreateBotModal";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { ScamFakeBadges } from "../components/flags";
import { displayUsername, formatDate } from "../lib/format";
import type { Navigate } from "../routing";
import type { BotListResponse } from "../types";

type Cursor = { beforeID: number };
type BotPageSize = 10 | 20 | 50 | 100;

const zeroCursor: Cursor = { beforeID: 0 };

export function BotsPage({ navigate }: { navigate: Navigate }) {
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState<BotPageSize>(50);
  const [data, setData] = useState<BotListResponse | null>(null);
  // history holds the cursor used to reach every page before the current
  // one, so "Previous" can pop back without re-deriving offsets -- keyset
  // pagination has no notion of "page N" to jump back to otherwise.
  const [history, setHistory] = useState<Cursor[]>([]);
  const [cursor, setCursor] = useState<Cursor>(zeroCursor);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [createModalOpen, setCreateModalOpen] = useState(false);

  async function fetchPage(query: string, at: Cursor) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit: String(limit) });
    if (query.trim()) {
      params.set("q", query.trim());
    }
    if (at.beforeID) {
      params.set("before_id", String(at.beforeID));
    }
    try {
      const result = await api.bots(params);
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
    const at = { beforeID: data.next_before_id };
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

  const rows = data?.rows ?? [];
  const verified = rows.filter((row) => row.Verified).length;
  const systemCount = rows.filter((row) => row.System).length;
  const canGoPrev = history.length > 0 && !busy;
  const canGoNext = Boolean(data?.has_more) && !busy;

  return (
    <PageFrame
      title={"Bots"}
      eyebrow={data?.listing === false ? "Search results" : "Recently created bots"}
      actions={
        <>
          <button className="btn primary icon-text" type="button" onClick={() => setCreateModalOpen(true)}>
            <Plus size={15} /> {"Create bot"}
          </button>
          <button className="btn" type="button" onClick={() => void loadFresh()} disabled={busy}>
            <RefreshCw size={15} /> {"Refresh"}
          </button>
        </>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Bots on page"} value={String(rows.length)} />
        <Metric label={"Verified"} value={String(verified)} tone="good" />
        <Metric label={"System"} value={String(systemCount)} />
      </div>

      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void loadFresh(); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={"Bot ID / username"} />
          </label>
          <label className="gift-page-size">
            <span>{"Limit"}</span>
            <select value={String(limit)} onChange={(event) => setLimit(Number(event.target.value) as BotPageSize)}>
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
                <td className="avatar-col">
                  <button className="avatar-link" type="button" onClick={() => navigate(`/bots/${row.ID}`)} aria-label={`Open bot ${row.ID}`}>
                    <Avatar id={row.ID} firstName={row.FirstName} username={row.Username} />
                  </button>
                </td>
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
            {rows.length === 0 && <EmptyRow colSpan={9} />}
          </tbody>
        </table>
      </div>

      {createModalOpen && (
        <CreateBotModal
          onClose={() => setCreateModalOpen(false)}
          // Deliberately does not close the modal -- the token is only ever
          // shown once, inside ActionButton's own result panel, and closing
          // immediately would yank it away before it can be copied. The
          // operator closes both modals manually once they're done with it.
          onCreated={() => void loadFresh()}
        />
      )}
    </PageFrame>
  );
}
