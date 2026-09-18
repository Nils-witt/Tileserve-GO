import { Tab, Tabs as MuiTabs } from '@mui/material';
import { Link, useLocation } from 'react-router-dom';

export interface TabDef {
  to: string;
  label: string;
}

export default function Tabs({ tabs }: { tabs: TabDef[] }) {
  const { pathname } = useLocation();
  const active = tabs.find((t) => pathname.startsWith(t.to))?.to ?? false;

  return (
    <MuiTabs value={active} sx={{ mb: 3, borderBottom: 1, borderColor: 'divider' }}>
      {tabs.map((t) => (
        <Tab key={t.to} value={t.to} label={t.label} component={Link} to={t.to} />
      ))}
    </MuiTabs>
  );
}
