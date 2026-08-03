import { ChevronRight, RefreshCw, ShieldAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel } from "../components/ui";
import { formatDate } from "../lib/format";
import type { Navigate } from "../routing";
import type { ModerationCaseRow } from "../types";

const defaultStatuses = "open,in_review,action_pending,action_failed,appeal_review";
const allStatuses = "open,in_review,action_pending,action_failed,resolved,dismissed,appeal_review";
const statusFilterOptions = [
  { value: defaultStatuses, label: "Active queue" },
  { value: allStatuses, label: "All statuses" },
  { value: "open", label: "Open" },
  { value: "in_review", label: "In review" },
  { value: "action_pending", label: "Action pending" },
  { value: "action_failed", label: "Action failed" },
  { value: "appeal_review", label: "Appeal review" },
  { value: "resolved", label: "Resolved" },
  { value: "dismissed", label: "Dismissed" }
];

export function ModerationCasesPage({ navigate }: { navigate: Navigate }) {
  const [statuses, setStatuses] = useState(defaultStatuses);
  const [assignedTo, setAssignedTo] = useState("");
  const [rows, setRows] = useState<ModerationCaseRow[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function load() {
    setBusy(true);
    setError("");
    try {
      const params = new URLSearchParams({ statuses, limit: "100" });
      if (assignedTo.trim()) params.set("assigned_to", assignedTo.trim());
      setRows((await api.moderationCases(params)).cases);
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const pendingActions = rows.filter((row) => row.Status === "action_pending" || row.Status === "action_failed").length;
  const critical = rows.filter((row) => row.Severity === 4).length;

  return (
    <PageFrame
      title={"Reports and Moderation"}
      eyebrow={"Moderation / Cases"}
      actions={
        <button className="btn icon-text" type="button" onClick={load} disabled={busy}>
          <RefreshCw size={15} className={busy ? "spin" : ""} /> {"Refresh"}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Current queue"} value={String(rows.length)} />
        <Metric label={"Critical cases"} value={String(critical)} tone={critical ? "danger" : "neutral"} />
        <Metric label={"Pending / failed actions"} value={String(pendingActions)} tone={pendingActions ? "warn" : "good"} />
      </div>
      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void load(); }}>
          <label className="field-inline">
            <span>{"Status"}</span>
            <select
              aria-label={"Case status filter"}
              value={statuses}
              onChange={(event) => setStatuses(event.target.value)}
            >
              {statusFilterOptions.map((option) => (
                <option key={option.value} value={option.value}>{option.label}</option>
              ))}
            </select>
          </label>
          <label className="field-inline">
            <span>{"Reviewer"}</span>
            <input
              value={assignedTo}
              onChange={(event) => setAssignedTo(event.target.value)}
              placeholder={"Leave blank for all"}
            />
          </label>
          <button className="btn primary icon-text" type="submit" disabled={busy}>
            <ShieldAlert size={15} /> {"Search"}
          </button>
        </form>
      </QueryPanel>
      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>{"Case"}</th>
              <th>{"Target"}</th>
              <th>{"Status"}</th>
              <th>{"Severity"}</th>
              <th>{"Reports / Reporters"}</th>
              <th>{"Reviewer"}</th>
              <th>{"Latest report"}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.ID}>
                <td className="mono">#{row.ID}</td>
                <td className="mono">{moderationTargetLabel(row.Target.Type, row.Target.ID)}</td>
                <td><CaseStatus status={row.Status} /></td>
                <td><CaseSeverity value={row.Severity} /></td>
                <td>{row.ReportCount} / {row.DistinctReporterCount}</td>
                <td>{row.AssignedTo || "-"}</td>
                <td>{formatDate(row.LastReportAt)}</td>
                <td>
                  <button className="row-link" onClick={() => navigate(`/moderation/${row.ID}`)}>
                    {"Review"} <ChevronRight size={14} />
                  </button>
                </td>
              </tr>
            ))}
            {rows.length === 0 && <EmptyRow colSpan={8} />}
          </tbody>
        </table>
      </div>
    </PageFrame>
  );
}

export function CaseStatus({ status }: { status: string }) {
  const tone = status === "resolved" || status === "dismissed"
    ? "good"
    : status === "action_failed"
      ? "danger"
      : status === "action_pending"
        ? "warn"
        : "neutral";
  return <Badge tone={tone}>{moderationEnumLabel("status", status)}</Badge>;
}

const severityLabels: Record<string, string> = {
  low: "Low",
  medium: "Medium",
  high: "High",
  critical: "Critical"
};

export function CaseSeverity({ value }: { value: number }) {
  const keys = ["", "low", "medium", "high", "critical"];
  const key = keys[value];
  return (
    <Badge tone={value >= 4 ? "danger" : value >= 3 ? "warn" : "neutral"}>
      {key ? severityLabels[key] : value}
    </Badge>
  );
}

const moderationLabels: Record<string, Record<string, string>> = {
  status: {
    open: "Open",
    in_review: "In review",
    action_pending: "Action pending",
    action_failed: "Action failed",
    appeal_review: "Appeal review",
    resolved: "Resolved",
    dismissed: "Dismissed"
  },
  targetType: {
    channel: "Channel",
    chat: "Group",
    user: "Account"
  },
  source: {
    account_peer: "Account / peer",
    antispam_false_positive: "Anti-spam false positive",
    channel_spam: "Channel spam",
    encrypted_spam: "Encrypted-chat spam",
    ephemeral: "Ephemeral media",
    messages: "Messages",
    messages_spam: "Message spam",
    profile_photo: "Profile photo",
    reaction: "Reaction",
    sponsored: "Sponsored message",
    story: "Story"
  },
  reason: {
    child_abuse: "Child abuse",
    copyright: "Copyright",
    fake: "Fake",
    geo_irrelevant: "Location-irrelevant",
    illegal_drugs: "Illegal drugs",
    other: "Other",
    personal_details: "Personal details",
    pornography: "Pornography",
    spam: "Spam",
    violence: "Violence"
  }
};

export function moderationEnumLabel(group: string, value: string): string {
  return moderationLabels[group]?.[value] ?? value;
}

export function moderationTargetLabel(type: string, id: number): string {
  return `${moderationEnumLabel("targetType", type)} #${id}`;
}
