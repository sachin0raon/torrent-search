import { useCallback, useEffect, useMemo, useState } from 'react';
import { Check, KeyRound, RotateCcw, ShieldAlert, Trash2, UserRound, X } from 'lucide-react';
import { useAuth } from '../auth/AuthContext.jsx';
import { adminApi } from '../api/auth.js';
import AccountMenu from './AccountMenu.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import Toast from './Toast.jsx';

const STATUSES = ['all', 'pending', 'active', 'disabled', 'rejected'];

function formatDate(value) {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleDateString();
}

function StatusBadge({ status }) {
  return <span className={`status-badge status-${status}`}>{status}</span>;
}

function ResetPasswordDialog({ user, onCancel, onSubmit }) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      await onSubmit(password);
    } catch (err) {
      setError(err.message || 'Could not reset password');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" onMouseDown={onCancel}>
      <div className="modal" onMouseDown={(e) => e.stopPropagation()} role="dialog" aria-modal="true">
        <h2>Reset password for {user.username}</h2>
        <p className="notice">
          This revokes every active session for the user. The new password is not shown again.
        </p>
        {error ? <ErrorBanner message={error} onDismiss={() => setError('')} /> : null}
        <form onSubmit={submit}>
          <label className="auth-field">
            <span>New password</span>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              minLength={8}
              autoFocus
              disabled={busy}
              required
            />
          </label>
          <div className="modal-actions">
            <button type="button" className="link" onClick={onCancel}>
              Cancel
            </button>
            <button type="submit" className="primary" disabled={busy}>
              {busy ? 'Saving…' : 'Reset password'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default function AdminPage() {
  const { ready, user, openAuth } = useAuth();
  const [users, setUsers] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [toast, setToast] = useState({ message: '', variant: 'info' });
  const [filter, setFilter] = useState('all');
  const [busyId, setBusyId] = useState(null);
  const [confirmDelete, setConfirmDelete] = useState(null);
  const [resetUser, setResetUser] = useState(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const data = await adminApi.listUsers();
      setUsers(data.users || []);
    } catch (err) {
      setError(err.message || 'Could not load users');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (user?.role === 'admin') load();
  }, [user, load]);

  useEffect(() => {
    if (!toast.message) return undefined;
    const timer = setTimeout(() => setToast({ message: '', variant: 'info' }), 4000);
    return () => clearTimeout(timer);
  }, [toast]);

  const pending = useMemo(() => users.filter((u) => u.status === 'pending'), [users]);
  const visible = useMemo(
    () => (filter === 'all' ? users : users.filter((u) => u.status === filter)),
    [users, filter],
  );

  async function run(id, action, message) {
    setBusyId(id);
    try {
      await action();
      await load();
      setToast({ message, variant: 'info' });
    } catch (err) {
      setToast({ message: err.message || 'Action failed', variant: 'error' });
    } finally {
      setBusyId(null);
    }
  }

  async function resetPassword(password) {
    await adminApi.resetPassword(resetUser.id, password);
    setResetUser(null);
    setToast({ message: `Password reset for ${resetUser.username}.`, variant: 'info' });
    await load();
  }

  if (!ready) {
    return <div className="container admin-container"><div className="spinner">Loading account…</div></div>;
  }

  if (!user) {
    return (
      <div className="container admin-container">
        <div className="admin-gate">
          <ShieldAlert size={30} />
          <h1>Admin panel</h1>
          <p>Log in with an admin account to manage users.</p>
          <div className="admin-gate-actions">
            <a href="/" className="link">← Back to search</a>
            <button className="primary" onClick={() => openAuth('login', 'Log in to open the admin panel.')}>
              Log in
            </button>
          </div>
        </div>
      </div>
    );
  }

  if (user.role !== 'admin') {
    return (
      <div className="container admin-container">
        <div className="admin-gate">
          <ShieldAlert size={30} />
          <h1>Not authorized</h1>
          <p>This account does not have admin access.</p>
          <a href="/" className="link">← Back to search</a>
        </div>
      </div>
    );
  }

  return (
    <div className="container admin-container">
      <header className="admin-topbar">
        <div>
          <span className="eyebrow">Administration</span>
          <h1>User management</h1>
        </div>
        <div className="admin-top-actions">
          <a href="/" className="link">← Search</a>
          <AccountMenu />
        </div>
      </header>

      <Toast
        message={toast.message}
        variant={toast.variant}
        onDismiss={() => setToast({ message: '', variant: 'info' })}
      />

      {error ? <ErrorBanner message={error} onDismiss={() => setError('')} /> : null}

      <section className="admin-section">
        <div className="admin-section-head">
          <h2>Pending registrations</h2>
          <span className="admin-count">{pending.length}</span>
        </div>
        {loading && users.length === 0 ? <div className="spinner">Loading users…</div> : null}
        {!loading && pending.length === 0 ? (
          <div className="empty">No pending registrations.</div>
        ) : null}
        <div className="admin-list">
          {pending.map((u) => (
            <div className="admin-user-card" key={u.id}>
              <div className="admin-user-main">
                <UserRound size={16} />
                <div>
                  <div className="admin-username">{u.username}</div>
                  <div className="admin-meta">Registered {formatDate(u.created_at)}</div>
                </div>
              </div>
              <div className="admin-actions">
                <button
                  className="primary"
                  disabled={busyId === u.id}
                  onClick={() => run(u.id, () => adminApi.approve(u.id), `${u.username} approved.`)}
                >
                  <Check size={14} /> Approve
                </button>
                <button
                  disabled={busyId === u.id}
                  onClick={() => run(u.id, () => adminApi.reject(u.id), `${u.username} rejected.`)}
                >
                  <X size={14} /> Reject
                </button>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section className="admin-section">
        <div className="admin-section-head">
          <h2>All users</h2>
          <select value={filter} onChange={(e) => setFilter(e.target.value)} aria-label="Filter users">
            {STATUSES.map((status) => (
              <option key={status} value={status}>
                {status === 'all' ? 'All statuses' : status}
              </option>
            ))}
          </select>
        </div>

        <div className="admin-table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th>User</th>
                <th>Status</th>
                <th>Created</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((u) => (
                <tr key={u.id}>
                  <td className="admin-username">{u.username}</td>
                  <td><StatusBadge status={u.status} /></td>
                  <td className="admin-meta">{formatDate(u.created_at)}</td>
                  <td>
                    <div className="admin-actions">
                      {u.status === 'active' ? (
                        <button
                          disabled={busyId === u.id}
                          onClick={() => run(u.id, () => adminApi.deactivate(u.id), `${u.username} deactivated.`)}
                        >
                          Deactivate
                        </button>
                      ) : null}
                      {u.status === 'disabled' ? (
                        <button
                          disabled={busyId === u.id}
                          onClick={() => run(u.id, () => adminApi.reactivate(u.id), `${u.username} reactivated.`)}
                        >
                          <RotateCcw size={14} /> Reactivate
                        </button>
                      ) : null}
                      {u.status === 'active' || u.status === 'disabled' ? (
                        <button onClick={() => setResetUser(u)}>
                          <KeyRound size={14} /> Reset password
                        </button>
                      ) : null}
                      <button
                        className={confirmDelete === u.id ? 'danger' : ''}
                        disabled={busyId === u.id}
                        onClick={async () => {
                          if (confirmDelete !== u.id) {
                            setConfirmDelete(u.id);
                            return;
                          }
                          setConfirmDelete(null);
                          await run(u.id, () => adminApi.remove(u.id), `${u.username} deleted.`);
                        }}
                      >
                        <Trash2 size={14} />
                        {confirmDelete === u.id ? 'Confirm delete?' : 'Delete'}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!loading && visible.length === 0 ? (
            <div className="empty">No users match this filter.</div>
          ) : null}
        </div>
      </section>

      {resetUser ? (
        <ResetPasswordDialog
          user={resetUser}
          onCancel={() => setResetUser(null)}
          onSubmit={resetPassword}
        />
      ) : null}
    </div>
  );
}
