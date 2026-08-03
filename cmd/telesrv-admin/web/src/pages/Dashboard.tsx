import { CheckCircle2, ChevronRight, Clock3, FileJson, KeyRound, MessageSquareText, ShieldCheck, Users } from "lucide-react";
import type { ReactNode } from "react";
import { AppLink } from "../components/AppLink";
import { StatusItem } from "../components/ui";
import type { Navigate } from "../routing";

export function Dashboard({ navigate }: { navigate: Navigate }) {
  return (
    <div className="dashboard-layout">
      <section className="overview-band">
        <div>
          <div className="eyebrow">{"Runtime Overview"}</div>
          <h2>{"Console Overview"}</h2>
        </div>
        <div className="overview-metrics">
          <StatusItem label={"Read path"} value={"PG read-only"} tone="neutral" />
          <StatusItem label={"Write path"} value="Admin API" tone="good" />
          <StatusItem label={"Execution policy"} value={"Dry-run first"} tone="warn" />
        </div>
      </section>
      <div className="command-grid">
        <Launcher icon={<Users />} title={"Accounts"} text={"Account status, premium, verification, sessions."} href="/accounts" navigate={navigate} />
        <Launcher icon={<ShieldCheck />} title={"Supergroups and Channels"} text={"Public entities, member counts, verification state."} href="/channels" navigate={navigate} />
        <Launcher icon={<MessageSquareText />} title={"Message Audit"} text={"Message boxes, updates, outbox state."} href="/messages" navigate={navigate} />
      </div>
      <section className="work-strip">
        <div className="strip-item"><CheckCircle2 size={16} /><span>{"All dangerous actions start with dry-run"}</span></div>
        <div className="strip-item"><KeyRound size={16} /><span>{"Browser never stores internal tokens"}</span></div>
        <div className="strip-item"><Clock3 size={16} /><span>{"Lists use cursor pagination"}</span></div>
        <div className="strip-item"><FileJson size={16} /><span>{"Detail pages retain raw state snapshots"}</span></div>
      </section>
    </div>
  );
}

function Launcher({
  icon,
  title,
  text,
  href,
  navigate
}: {
  icon: ReactNode;
  title: string;
  text: string;
  href: string;
  navigate: Navigate;
}) {
  return (
    <AppLink className="launcher" href={href} navigate={navigate}>
      <span className="launcher-icon">{icon}</span>
      <span className="launcher-copy">
        <strong>{title}</strong>
        <span>{text}</span>
      </span>
      <ChevronRight size={16} />
    </AppLink>
  );
}
