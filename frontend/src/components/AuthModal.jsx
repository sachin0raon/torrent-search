import { useEffect, useState } from 'react';
import { motion } from 'framer-motion';
import { X } from 'lucide-react';
import { useAuth } from '../auth/AuthContext.jsx';
import ErrorBanner from './ErrorBanner.jsx';
import { spring } from '../motion.js';

const TITLES = {
  login: 'Log in',
  register: 'Create account',
  password: 'Change password',
};

export default function AuthModal({ mode: initialMode = 'login', message = '', onClose }) {
  const { login, register, changePassword } = useAuth();
  const [mode, setMode] = useState(initialMode);
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [currentPassword, setCurrentPassword] = useState('');
  const [newPassword, setNewPassword] = useState('');
  const [error, setError] = useState('');
  const [notice, setNotice] = useState(message);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    setMode(initialMode);
    setNotice(message);
    setError('');
  }, [initialMode, message]);

  useEffect(() => {
    function onKey(e) {
      if (e.key === 'Escape') onClose();
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  function switchMode(next) {
    setMode(next);
    setError('');
    setNotice('');
    setPassword('');
    setNewPassword('');
  }

  async function submit(e) {
    e.preventDefault();
    setBusy(true);
    setError('');
    try {
      if (mode === 'login') {
        await login(username, password);
        onClose();
      } else if (mode === 'register') {
        const data = await register(username, password);
        setNotice(data.message || 'Registration submitted for admin approval.');
        setMode('login');
        setPassword('');
      } else {
        await changePassword(currentPassword, newPassword);
        setNotice('Password changed. Other sessions were logged out.');
        setCurrentPassword('');
        setNewPassword('');
      }
    } catch (err) {
      setError(err.message || 'Request failed');
    } finally {
      setBusy(false);
    }
  }

  const isPasswordMode = mode === 'password';

  return (
    <motion.div
      className="modal-backdrop"
      onMouseDown={onClose}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.2 }}
    >
      <motion.div
        className="modal auth-modal"
        onMouseDown={(e) => e.stopPropagation()}
        initial={{ opacity: 0, y: 20, scale: 0.97 }}
        animate={{ opacity: 1, y: 0, scale: 1 }}
        exit={{ opacity: 0, y: 14, scale: 0.97 }}
        transition={spring}
        role="dialog"
        aria-modal="true"
        aria-label={TITLES[mode]}
      >
        <button className="modal-close auth-close" onClick={onClose} aria-label="Close">
          <X size={16} />
        </button>
        <h2>{TITLES[mode]}</h2>

        {mode === 'login' || mode === 'register' ? (
          <div className="auth-tabs" role="tablist" aria-label="Authentication">
            <button
              role="tab"
              aria-selected={mode === 'login'}
              className={mode === 'login' ? 'active' : ''}
              onClick={() => switchMode('login')}
            >
              Log in
            </button>
            <button
              role="tab"
              aria-selected={mode === 'register'}
              className={mode === 'register' ? 'active' : ''}
              onClick={() => switchMode('register')}
            >
              Register
            </button>
          </div>
        ) : null}

        {notice ? <div className="notice auth-notice">{notice}</div> : null}
        {error ? <ErrorBanner message={error} onDismiss={() => setError('')} /> : null}

        <form onSubmit={submit}>
          {!isPasswordMode ? (
            <label className="auth-field">
              <span>Username</span>
              <input
                type="text"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                autoComplete="username"
                autoFocus
                disabled={busy}
                required
              />
            </label>
          ) : null}

          {mode === 'login' || mode === 'register' ? (
            <label className="auth-field">
              <span>Password</span>
              <input
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
                disabled={busy}
                required
                minLength={mode === 'register' ? 8 : undefined}
              />
              {mode === 'register' ? (
                <small>8-128 characters. An admin must approve the account.</small>
              ) : null}
            </label>
          ) : null}

          {isPasswordMode ? (
            <>
              <label className="auth-field">
                <span>Current password</span>
                <input
                  type="password"
                  value={currentPassword}
                  onChange={(e) => setCurrentPassword(e.target.value)}
                  autoComplete="current-password"
                  autoFocus
                  disabled={busy}
                  required
                />
              </label>
              <label className="auth-field">
                <span>New password</span>
                <input
                  type="password"
                  value={newPassword}
                  onChange={(e) => setNewPassword(e.target.value)}
                  autoComplete="new-password"
                  disabled={busy}
                  required
                  minLength={8}
                />
              </label>
            </>
          ) : null}

          <div className="modal-actions auth-actions">
            <button type="button" className="link" onClick={onClose}>
              Cancel
            </button>
            <button type="submit" className="primary" disabled={busy}>
              {busy
                ? 'Working…'
                : mode === 'login'
                  ? 'Log in'
                  : mode === 'register'
                    ? 'Register'
                    : 'Change password'}
            </button>
          </div>
        </form>
      </motion.div>
    </motion.div>
  );
}
