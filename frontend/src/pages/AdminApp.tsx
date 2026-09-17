import { useState } from "react";
import { useAuth } from "../auth/AuthContext";
import { AdminDataProvider, useAdminData } from "../features/AdminDataContext";
import Tabs, { type TabDef } from "../components/Tabs";
import Footer from "../components/Footer";
import MapsTab from "../features/maps/MapsTab";
import UsersTab from "../features/users/UsersTab";
import GroupsTab from "../features/groups/GroupsTab";
import SyncTab from "../features/sync/SyncTab";
import AuditTab from "../features/audit/AuditTab";

type TabKey = "maps" | "users" | "groups" | "sync" | "audit";

function AdminAppContent() {
  const { username, logout } = useAuth();
  const { isAdmin } = useAdminData();
  const [tab, setTab] = useState<TabKey>("maps");

  const tabs: TabDef[] = [
    { key: "maps", label: "Maps" },
    ...(isAdmin
      ? [
          { key: "users", label: "Users" },
          { key: "groups", label: "Groups" },
          { key: "sync", label: "Sync" },
          { key: "audit", label: "Audit log" },
        ]
      : []),
  ];

  return (
    <div className="wrap">
      <div className="top-bar">
        <h1>tileserve-go</h1>
        <div>
          <span className="muted">{username}</span>{" "}
          <button type="button" className="secondary" onClick={logout}>
            Log out
          </button>
        </div>
      </div>

      <Tabs tabs={tabs} active={tab} onSelect={(key) => setTab(key as TabKey)} />

      {tab === "maps" && <MapsTab />}
      {tab === "users" && isAdmin && <UsersTab />}
      {tab === "groups" && isAdmin && <GroupsTab />}
      {tab === "sync" && isAdmin && <SyncTab />}
      {tab === "audit" && isAdmin && <AuditTab />}

      <Footer />
    </div>
  );
}

export default function AdminApp() {
  return (
    <AdminDataProvider>
      <AdminAppContent />
    </AdminDataProvider>
  );
}
