import { AtSign, ChevronDown, ChevronRight, Flame, Loader2, Plus, RefreshCw, Search, Vault } from "lucide-react";
import { useEffect, useState } from "react";
import { api, errorMessage } from "../api";
import { ActionButton } from "../components/ActionButton";
import { ChannelPicker, UserPicker } from "../components/EntityPicker";
import { Alert, Badge, EmptyRow, Metric, PageFrame, QueryPanel, SectionHead } from "../components/ui";
import { currencyExponent, displayUsername, formatCurrency, formatDate, toSmallestUnits } from "../lib/format";
import type { Navigate } from "../routing";
import type {
  AccountRow,
  ChannelRow,
  CollectibleCurrency,
  CollectibleUsernameRow,
  CollectibleUsernameStatus
} from "../types";

type StatusFilter = "all" | CollectibleUsernameStatus;
type OwnerKind = "vault" | "user" | "channel";

export function CollectibleUsernamesPage({ navigate }: { navigate: Navigate }) {
  const [status, setStatus] = useState<StatusFilter>("all");
  const [q, setQ] = useState("");
  const [limit, setLimit] = useState("50");
  const [rows, setRows] = useState<CollectibleUsernameRow[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [cursor, setCursor] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  // Mint form state.
  const [ownerKind, setOwnerKind] = useState<OwnerKind>("vault");
  const [owner, setOwner] = useState<AccountRow | null>(null);
  const [ownerChannel, setOwnerChannel] = useState<ChannelRow | null>(null);
  const [mintUsername, setMintUsername] = useState("");
  const [currency, setCurrency] = useState<CollectibleCurrency>("XTR");
  const [amount, setAmount] = useState("");
  const [cryptoCurrency, setCryptoCurrency] = useState("");
  const [cryptoAmount, setCryptoAmount] = useState("");
  const [url, setUrl] = useState("");
  const [purchaseDate, setPurchaseDate] = useState("");
  const [purchaseTime, setPurchaseTime] = useState("");

  async function load(next = false) {
    setBusy(true);
    setError("");
    const params = new URLSearchParams({ limit });
    if (status !== "all") params.set("status", status);
    if (q.trim()) params.set("q", q.trim().replace(/^@/, ""));
    if (next && cursor) params.set("before_id", cursor);
    try {
      const result = await api.collectibleUsernames(params);
      const page = result.rows ?? [];
      setRows((current) => (next ? [...current, ...page] : page));
      setCursor(result.next_before_id ?? "");
      setHasMore(Boolean(result.has_more));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void load(false);
  }, []);

  const vaultCount = rows.filter((row) => row.Status === "vault").length;
  const ownedCount = rows.filter((row) => row.Status === "owned").length;
  const burnedCount = rows.filter((row) => row.Status === "burned").length;

  // int64 request fields are sent as decimal strings (the backend tags them
  // `,string`); purchase_date is Unix seconds. Optional owner keys are omitted
  // entirely rather than sent empty, because `,string,omitempty` cannot decode "".
  // Both amounts are typed in whole currency units and converted here: the API
  // and fragment.collectibleInfo carry smallest units, so 900 TON has to leave
  // the panel as 900000000000 nanotons or clients render 0.0000009.
  const minorAmount = toSmallestUnits(amount, currency);
  const minorCryptoAmount = cryptoCurrency ? toSmallestUnits(cryptoAmount, cryptoCurrency) : "0";
  const amountInvalid = minorAmount === null;
  const cryptoAmountInvalid = minorCryptoAmount === null;

  function mintPayload(): Record<string, unknown> {
    const payload: Record<string, unknown> = {
      username: mintUsername.trim().replace(/^@/, ""),
      currency,
      amount: minorAmount ?? "0"
    };
    if (ownerKind === "user" && owner) payload.owner_user_id = String(owner.ID);
    if (ownerKind === "channel" && ownerChannel) payload.owner_channel_id = String(ownerChannel.ID);
    // The backend accepts either no crypto leg at all, or TON with a positive
    // nanoton amount — never a currency without an amount.
    if (cryptoCurrency) {
      payload.crypto_currency = cryptoCurrency;
      payload.crypto_amount = minorCryptoAmount ?? "0";
    }
    if (url.trim()) payload.url = url.trim();
    if (purchaseDate) {
      // fragment.collectibleInfo.purchase_date is a unix timestamp, and the date has
      // always been read as UTC here. The time follows the same clock rather than the
      // operator's local one, so adding it cannot silently shift what a date-only
      // entry used to mean; the field label says UTC.
      const parsed = Date.parse(`${purchaseDate}T${purchaseTime || "00:00"}:00Z`);
      if (Number.isFinite(parsed)) payload.purchase_date = Math.floor(parsed / 1000);
    }
    return payload;
  }

  return (
    <PageFrame
      title={"Collectible usernames"}
      eyebrow={"NFT usernames / Registry"}
      actions={
        <button className="btn icon-text" type="button" onClick={() => load(false)} disabled={busy}>
          <RefreshCw size={15} className={busy ? "spin" : ""} /> {"Refresh"}
        </button>
      }
    >
      {error && <Alert>{error}</Alert>}
      <div className="metric-row">
        <Metric label={"Loaded rows"} value={String(rows.length)} />
        <Metric label={"In vault"} value={String(vaultCount)} />
        <Metric label={"Held by owners"} value={String(ownedCount)} tone="good" />
        <Metric label={"Burned"} value={String(burnedCount)} tone={burnedCount ? "danger" : "neutral"} />
      </div>

      <section className="section-block">
        <SectionHead title={"Mint a collectible username"} text={"Creates the asset together with its purchase record. Keep the owner as vault to mint it unassigned."} />
        <div className="toolbar" role="group" aria-label={"Owner type"}>
          <button type="button" className={`btn ${ownerKind === "vault" ? "primary" : ""}`} onClick={() => setOwnerKind("vault")}>
            <Vault size={15} /> {"Vault (no owner)"}
          </button>
          <button type="button" className={`btn ${ownerKind === "user" ? "primary" : ""}`} onClick={() => setOwnerKind("user")}>
            {"User owner"}
          </button>
          <button type="button" className={`btn ${ownerKind === "channel" ? "primary" : ""}`} onClick={() => setOwnerKind("channel")}>
            {"Channel owner"}
          </button>
        </div>
        {ownerKind === "user" && <UserPicker label={"User owner"} value={owner} onChange={setOwner} />}
        {ownerKind === "channel" && <ChannelPicker label={"Channel owner"} value={ownerChannel} onChange={setOwnerChannel} />}
        <div className="bot-create-fields">
          <label className="duration-field">
            <span>{"Username"}</span>
            <input value={mintUsername} onChange={(event) => setMintUsername(event.target.value)} placeholder="durov" />
          </label>
          <label className="duration-field">
            <span>{"Currency"}</span>
            <select value={currency} onChange={(event) => setCurrency(event.target.value as CollectibleCurrency)}>
              <option value="XTR">XTR</option>
              <option value="TON">TON</option>
              <option value="USD">USD</option>
            </select>
          </label>
          <label className="duration-field">
            <span>{`Amount (${currency})`}</span>
            <input value={amount} onChange={(event) => setAmount(event.target.value)} inputMode="decimal" placeholder="1000" />
          </label>
          <label className="duration-field">
            <span>{"Crypto currency"}</span>
            <select value={cryptoCurrency} onChange={(event) => setCryptoCurrency(event.target.value)}>
              <option value="">{"None"}</option>
              <option value="TON">TON</option>
            </select>
          </label>
          {cryptoCurrency !== "" && (
            <label className="duration-field">
              <span>{`Crypto amount (${cryptoCurrency})`}</span>
              <input value={cryptoAmount} onChange={(event) => setCryptoAmount(event.target.value)} inputMode="decimal" placeholder="12.5" />
            </label>
          )}
          <label className="duration-field">
            <span>{"Marketplace URL"}</span>
            <input value={url} onChange={(event) => setUrl(event.target.value)} placeholder="https://fragment.com/username/durov" />
          </label>
          <label className="duration-field">
            <span>{"Purchase date (UTC)"}</span>
            <input value={purchaseDate} onChange={(event) => setPurchaseDate(event.target.value)} type="date" />
          </label>
          <label className="duration-field">
            <span>{"Purchase time (UTC)"}</span>
            <input
              value={purchaseTime}
              onChange={(event) => setPurchaseTime(event.target.value)}
              type="time"
              step={60}
              disabled={!purchaseDate}
            />
          </label>
        </div>
        <p className="bot-create-note">
          {`Amounts are typed in whole ${currency} and stored as the smallest units the API and fragment.collectibleInfo carry, so clients render the price you meant. Up to ${String(currencyExponent(currency))} decimal places. Clients will show: ${formatCurrency(minorAmount ?? "0", currency)}.`}
        </p>
        {amountInvalid && <Alert>{`That is not a valid ${currency} amount: digits only, with at most ${String(currencyExponent(currency))} decimal places.`}</Alert>}
        {cryptoCurrency !== "" && cryptoAmountInvalid && (
          <Alert>{`That is not a valid ${cryptoCurrency} amount: digits only, with at most ${String(currencyExponent(cryptoCurrency))} decimal places.`}</Alert>
        )}
        <div className="bot-create-actions">
          <span className="bot-create-note">{"Username, currency and amount are required; the dry-run checks availability first."}</span>
          <ActionButton
            disabled={amountInvalid || cryptoAmountInvalid}
            label={"Mint username"}
            icon={<Plus size={15} />}
            tone="neutral"
            path="/api/actions/mint-collectible-username"
            payload={mintPayload}
            onDone={() => load(false)}
          />
        </div>
      </section>

      <QueryPanel>
        <form className="toolbar" onSubmit={(event) => { event.preventDefault(); void load(false); }}>
          <label className="searchbox">
            <Search size={15} />
            <input value={q} onChange={(event) => setQ(event.target.value)} placeholder={"Search by username"} />
          </label>
          <label className="field-inline">
            <span>{"Status"}</span>
            <select value={status} onChange={(event) => setStatus(event.target.value as StatusFilter)}>
              <option value="all">{"All statuses"}</option>
              <option value="vault">{"Vault"}</option>
              <option value="owned">{"Owned"}</option>
              <option value="burned">{"Burned"}</option>
            </select>
          </label>
          <label className="field-inline">
            <span>{"Limit"}</span>
            <input className="small-input" value={limit} onChange={(event) => setLimit(event.target.value)} type="number" min="1" max="200" />
          </label>
          <button className="btn primary icon-text" type="submit" disabled={busy}>
            {busy ? <Loader2 size={15} className="spin" /> : <Search size={15} />} {"Search"}
          </button>
        </form>
      </QueryPanel>

      <div className="table-wrap">
        <table className="data-table">
          <thead>
            <tr>
              <th>{"Username"}</th>
              <th>{"Status"}</th>
              <th>{"Owner"}</th>
              <th>{"Price"}</th>
              <th>{"Purchase date (UTC)"}</th>
              <th>{"Transfers"}</th>
              <th>{"Updated"}</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => (
              <tr key={row.ID}>
                <td><strong>{displayUsername(row.Username)}</strong></td>
                <td><UsernameStatus status={row.Status} /></td>
                <td>{ownerLabel(row, "Vault")}</td>
                <td className="mono">{priceLabel(row)}</td>
                <td>{formatDate(row.PurchaseDate) || "-"}</td>
                <td className="mono">{row.TransferCount}</td>
                <td>{formatDate(row.UpdatedAt) || "-"}</td>
                <td>
                  <button className="row-link" type="button" onClick={() => navigate(`/collectible-usernames/${row.ID}`)}>
                    <AtSign size={14} /> {"Details"} <ChevronRight size={14} />
                  </button>
                </td>
              </tr>
            ))}
            {rows.length === 0 && <EmptyRow colSpan={8} />}
          </tbody>
        </table>
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

export function UsernameStatus({ status }: { status: CollectibleUsernameStatus }) {
  if (status === "owned") return <Badge tone="good">{"Owned"}</Badge>;
  if (status === "burned") return <Badge tone="danger"><Flame size={12} /> {"Burned"}</Badge>;
  return <Badge><Vault size={12} /> {"Vault"}</Badge>;
}

export function ownerLabel(row: CollectibleUsernameRow, vaultLabel: string): string {
  if (!row.OwnerPeerType || row.OwnerPeerID === "" || row.OwnerPeerID === "0") return vaultLabel;
  const name = displayUsername(row.OwnerUsername) || row.OwnerName || row.OwnerPeerID;
  return `${name} · ${row.OwnerPeerType}:${row.OwnerPeerID}`;
}

// priceLabel renders what a Telegram client will draw, not the stored integer:
// both legs are smallest units on the wire (see formatCurrency).
export function priceLabel(row: CollectibleUsernameRow): string {
  const base = formatCurrency(row.Amount, row.Currency);
  if (row.CryptoCurrency && row.CryptoAmount && row.CryptoAmount !== "0") {
    return `${base} (${formatCurrency(row.CryptoAmount, row.CryptoCurrency)})`;
  }
  return base;
}
