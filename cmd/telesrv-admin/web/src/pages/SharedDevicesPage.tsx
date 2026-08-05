import { ArrowLeft, ChevronDown, ChevronRight, Loader2, RefreshCw, Smartphone } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Avatar } from "../components/Avatar";
import { Alert, Badge, EmptyRow, Metric, PageFrame, SectionHead } from "../components/ui";
import { displayName, displayPhone, displayUsername, formatDate } from "../lib/format";
import type { Navigate } from "../routing";
import type { SharedDeviceGroup } from "../types";

// SharedDevicesPage surfaces authorizations that look like they came from the
// same physical device but belong to different accounts -- a lead worth
// investigating for multi-accounting, not a verdict: device_model and
// system_version are self-reported by the client and easy to spoof, and a
// matching IP alone is common and innocent behind NAT, shared wifi, or
// carrier CGNAT. Treat every group here as "worth a look", not "guilty".
export function SharedDevicesPage({ navigate }: { navigate: Navigate }) {
  const [groups, setGroups] = useState<SharedDeviceGroup[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [offset, setOffset] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function load(next = false) {
    setBusy(true);
    setError("");
    const at = next ? offset : 0;
    const params = new URLSearchParams({ limit: "20", offset: String(at) });
    try {
      const result = await api.sharedDeviceGroups(params);
      const page = result.rows ?? [];
      setGroups((current) => (next ? [...current, ...page] : page));
      setOffset(result.next_offset);
      setHasMore(Boolean(result.has_more));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const totalFlaggedAccounts = groups.reduce((sum, group) => sum + group.AccountCount, 0);

  return (
    <PageFrame
      title={"Shared Devices"}
      eyebrow={"Multi-account signal — device/IP overlap across different accounts"}
      actions={
        <>
          <button className="btn icon-text" type="button" onClick={() => navigate("/accounts")}>
            <ArrowLeft size={15} /> {"Back to accounts"}
          </button>
          <button className="btn" type="button" onClick={() => load(false)} disabled={busy}>
            <RefreshCw size={15} className={busy ? "spin" : ""} /> {"Refresh"}
          </button>
        </>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Device groups on page"} value={String(groups.length)} />
        <Metric label={"Accounts flagged on page"} value={String(totalFlaggedAccounts)} tone="warn" />
      </div>
      <p className="about-text">
        {"Each card below is a device fingerprint (device model + OS + platform + IP) that more than one account has authorized from. "}
        {"device_model/system_version are self-reported by the client, and IP alone can collide innocently -- use this as a lead, not a verdict."}
      </p>

      <div className="stacked-sections">
        {groups.map((group) => (
          <section className="section-block" key={`${group.DeviceModel}|${group.SystemVersion}|${group.Platform}|${group.IP}`}>
            <SectionHead
              title={group.DeviceModel || "Unknown device"}
              text={`${group.Platform || "unknown platform"} ${group.SystemVersion} · ${group.IP} · last active ${formatDate(group.LastActiveAt)}`}
              action={<Badge tone="warn"><Smartphone size={12} /> {`${group.AccountCount} accounts`}</Badge>}
            />
            <div className="table-wrap">
              <table className="data-table">
                <thead>
                  <tr>
                    <th className="avatar-col"></th>
                    <th>{"User ID"}</th>
                    <th>{"Phone"}</th>
                    <th>{"Username"}</th>
                    <th>{"Name"}</th>
                    <th>{"Active from this device"}</th>
                    <th></th>
                  </tr>
                </thead>
                <tbody>
                  {group.Accounts.map((account) => (
                    <tr key={account.UserID}>
                      <td className="avatar-col"><Avatar id={account.UserID} firstName={account.FirstName} lastName={account.LastName} username={account.Username} /></td>
                      <td className="mono">{account.UserID}</td>
                      <td>{displayPhone(account.Phone)}</td>
                      <td>{displayUsername(account.Username) || "-"}</td>
                      <td>{displayName(account) || "-"}</td>
                      <td>{formatDate(account.ActiveAt)}</td>
                      <td><button className="row-link" onClick={() => navigate(`/accounts/${account.UserID}`)}>{"Details"} <ChevronRight size={14} /></button></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </section>
        ))}
        {groups.length === 0 && (
          <div className="table-wrap">
            <table className="data-table">
              <tbody>
                <EmptyRow colSpan={7} />
              </tbody>
            </table>
          </div>
        )}
      </div>

      {hasMore && (
        <div className="toolbar">
          <button className="btn icon-text" type="button" onClick={() => load(true)} disabled={busy}>
            {busy ? <Loader2 size={15} className="spin" /> : <ChevronDown size={15} />} {"Load more"}
          </button>
        </div>
      )}
    </PageFrame>
  );
}
