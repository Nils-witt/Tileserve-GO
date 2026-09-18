import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  TextField,
  Typography,
} from '@mui/material';
import { apiFetch, apiJson, apiPostJSON } from '../../api/client';
import type { ApiKey, GeneratedKeyPair } from '../../api/types';
import Modal from '../../components/Modal';
import ErrorBanner from '../../components/ErrorBanner';
import { fmtDate } from '../../lib/format';
import ScopesModal from './ScopesModal';

export default function ApiKeysModal({
  username,
  onClose,
}: {
  username: string | null;
  onClose: () => void;
}) {
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [pubkey, setPubkey] = useState('');
  const [scopesKey, setScopesKey] = useState<ApiKey | null>(null);

  const load = useCallback(async () => {
    if (!username) return;
    setError(null);
    try {
      setKeys(await apiJson<ApiKey[]>(`/users/${encodeURIComponent(username)}/api-keys`));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [username]);

  useEffect(() => {
    if (!username) return;
    setName('');
    setPubkey('');
    load();
  }, [username, load]);

  const generateKeyPair = async () => {
    setError(null);
    try {
      const kp = await apiPostJSON<GeneratedKeyPair>('/keys/generate', {});
      // The private key is only ever available here, once — a blocking
      // prompt (pre-filled, selectable) is the simplest way to give the
      // admin a chance to copy it before it's gone for good.
      prompt('Save this private key now — it will not be shown again:', kp.privateKeyPem);
      setPubkey(kp.publicKeyPem);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const createKey = async () => {
    if (!username || !name.trim() || !pubkey.trim()) return;
    setError(null);
    try {
      const created = await apiPostJSON<ApiKey>(`/users/${encodeURIComponent(username)}/api-keys`, {
        name: name.trim(),
        publicKeyPem: pubkey.trim(),
      });
      setName('');
      setPubkey('');
      await load();
      // Nothing secret comes back here — the server never sees a private
      // key — but the caller still needs to know which id to use as the
      // JWT `kid` when signing tokens for this key.
      alert('API key registered. Key ID (use as the JWT "kid" when signing tokens): ' + created.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  const revokeKey = async (id: string) => {
    if (!username) return;
    if (!confirm('Revoke this API key? Anything still using it will lose access immediately.'))
      return;
    setError(null);
    try {
      await apiFetch(`/users/${encodeURIComponent(username)}/api-keys/${id}`, { method: 'DELETE' });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <>
      <Modal
        open={!!username}
        title={username ? `API keys — ${username}` : 'API keys'}
        onClose={onClose}
      >
        <ErrorBanner message={error} />
        <TableContainer>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Name</TableCell>
                <TableCell>Created</TableCell>
                <TableCell>Last used</TableCell>
                <TableCell>Scoped</TableCell>
                <TableCell>Actions</TableCell>
              </TableRow>
            </TableHead>
            <TableBody>
              {keys.map((k) => (
                <TableRow key={k.id}>
                  <TableCell>{k.name || '-'}</TableCell>
                  <TableCell>
                    {fmtDate(k.createdAt)}
                    <br />
                    <Typography variant="caption" color="text.secondary">
                      by {k.createdBy}
                    </Typography>
                  </TableCell>
                  <TableCell>{k.lastUsedAt ? fmtDate(k.lastUsedAt) : 'never'}</TableCell>
                  <TableCell>
                    {k.scoped ? (
                      'Yes'
                    ) : (
                      <Typography variant="body2" color="text.secondary" component="span">
                        No
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell>
                    <Stack direction="row" spacing={1}>
                      <Button size="small" onClick={() => setScopesKey(k)}>
                        Scopes
                      </Button>
                      <Button size="small" color="error" onClick={() => revokeKey(k.id)}>
                        Revoke
                      </Button>
                    </Stack>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </TableContainer>
        {keys.length === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
            No API keys yet.
          </Typography>
        )}
        <Stack
          direction="row"
          spacing={2}
          useFlexGap
          sx={{ flexWrap: 'wrap', alignItems: 'flex-start', mt: 2 }}
        >
          <TextField
            label="Name"
            size="small"
            placeholder="e.g. edge-server-01 sync"
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
          <TextField
            label="Public key (PEM)"
            multiline
            minRows={3}
            placeholder="-----BEGIN PUBLIC KEY-----..."
            sx={{ flexBasis: '100%', fontFamily: 'monospace' }}
            slotProps={{ htmlInput: { style: { fontFamily: 'monospace', fontSize: '0.8em' } } }}
            value={pubkey}
            onChange={(e) => setPubkey(e.target.value)}
          />
          <Button onClick={generateKeyPair}>Generate key pair</Button>
          <Button variant="contained" disabled={!name.trim() || !pubkey.trim()} onClick={createKey}>
            Create
          </Button>
        </Stack>
        <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
          The key authenticates as this user (their full permissions apply, narrowed by "Scopes"
          below if any are set). The caller generates its own RSA key pair and signs short-lived
          JWTs with the private half — this server only ever stores the public half. Click "Generate
          key pair" to have this server generate one for you (the private key is shown exactly once,
          copy it immediately), or paste a public key generated elsewhere. Use the resulting key's
          ID as the "Remote API key ID" when registering this server as a sync remote on another
          instance.
        </Typography>
      </Modal>
      <ScopesModal
        username={username}
        apiKey={scopesKey}
        onClose={() => setScopesKey(null)}
        onScopesChanged={load}
      />
    </>
  );
}
