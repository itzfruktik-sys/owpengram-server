import { ChevronRight, MessageSquareText, ShieldCheck, Users } from "lucide-react";
import type { ReactNode } from "react";
import { AppLink } from "../components/AppLink";
import type { Navigate } from "../routing";

export function Dashboard({ navigate }: { navigate: Navigate }) {
  return (
    <div className="dashboard-layout">
      <div className="command-grid">
        <Launcher icon={<Users />} title={"Accounts"} text={"Account status, premium, verification, sessions."} href="/accounts" navigate={navigate} />
        <Launcher icon={<ShieldCheck />} title={"Supergroups and Channels"} text={"Public entities, member counts, verification state."} href="/channels" navigate={navigate} />
        <Launcher icon={<MessageSquareText />} title={"Message Audit"} text={"Message boxes, updates, outbox state."} href="/messages" navigate={navigate} />
      </div>
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
