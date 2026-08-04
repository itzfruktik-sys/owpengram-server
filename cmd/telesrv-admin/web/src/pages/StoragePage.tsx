import { ChevronDown, Loader2, RefreshCw } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Alert, EmptyRow, Metric, PageFrame } from "../components/ui";
import { displayUsername, formatBytes, formatQuantity } from "../lib/format";
import type { Navigate } from "../routing";
import type { AccountStorageRow, StorageStatsResponse } from "../types";

export function StoragePage({ navigate: _navigate }: { navigate: Navigate }) {
  const [stats, setStats] = useState<StorageStatsResponse | null>(null);
  const [rows, setRows] = useState<AccountStorageRow[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [offset, setOffset] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function loadStats() {
    try {
      setStats(await api.storageStats());
    } catch {
      // Stats are a header nicety; a failure here shouldn't block the list.
    }
  }

  async function loadAccounts(next = false) {
    setBusy(true);
    setError("");
    const at = next ? offset : 0;
    const params = new URLSearchParams({ limit: "50", offset: String(at) });
    try {
      const result = await api.storageAccounts(params);
      const page = result.rows ?? [];
      setRows((current) => (next ? [...current, ...page] : page));
      setOffset(result.next_offset);
      setHasMore(Boolean(result.has_more));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  function refresh() {
    void loadStats();
    void loadAccounts(false);
  }

  useEffect(() => {
    refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Physical is what actually consumes disk/S3 (deduplicated); logical is the
  // sum of what the per-account table below adds up to. They legitimately
  // differ when the same content is shared by more than one document/photo.
  const dedupBytes = stats ? Math.max(0, Number(stats.LogicalBytes) - Number(stats.PhysicalBytes)) : 0;

  return (
    <PageFrame
      title={"Storage"}
      eyebrow={"Media / Storage usage"}
      actions={
        <button className="btn icon-text" type="button" onClick={refresh} disabled={busy}>
          <RefreshCw size={15} className={busy ? "spin" : ""} /> {"Refresh"}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Physical usage (on disk / S3)"} value={stats ? formatBytes(stats.PhysicalBytes) : "-"} />
        <Metric label={"Logical usage (sum per account)"} value={stats ? formatBytes(stats.LogicalBytes) : "-"} />
        <Metric label={"Saved by dedup"} value={formatBytes(String(dedupBytes))} tone={dedupBytes > 0 ? "good" : "neutral"} />
        <Metric label={"Backend"} value={stats?.BackendKind ?? "-"} />
      </div>
      <div className="metric-row">
        <Metric label={"Documents"} value={stats ? formatQuantity(stats.DocumentCount) : "-"} />
        <Metric label={"Photos"} value={stats ? formatQuantity(stats.PhotoCount) : "-"} />
        <Metric label={"Accounts with media"} value={stats ? formatQuantity(stats.AccountCount) : "-"} />
        <Metric
          label={"Unattributed"}
          value={stats ? formatBytes(stats.UnattributedBytes) : "-"}
          tone={stats && Number(stats.UnattributedBytes) > 0 ? "warn" : "neutral"}
        />
      </div>

      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>{"User ID"}</th>
              <th>{"Account"}</th>
              <th>{"Storage used"}</th>
              <th>{"Files"}</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.UserID}>
                <td className="mono">{row.UserID}</td>
                <td>{displayUsername(row.Username) || row.FirstName || "-"}</td>
                <td className="mono">{formatBytes(row.Bytes)}</td>
                <td className="mono">{formatQuantity(row.FileCount)}</td>
              </tr>
            ))}
            {rows.length === 0 && <EmptyRow colSpan={4} />}
          </tbody>
        </table>
      </div>
      {hasMore && (
        <div className="toolbar">
          <button className="btn icon-text" type="button" onClick={() => loadAccounts(true)} disabled={busy}>
            {busy ? <Loader2 size={15} className="spin" /> : <ChevronDown size={15} />} {"Load more"}
          </button>
        </div>
      )}
    </PageFrame>
  );
}
