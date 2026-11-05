import { useEffect, useRef, useState } from 'react';
import { ChevronDown, KeyRound, LogOut, ShieldCheck, UserRound } from 'lucide-react';
import { useAuth } from '../auth/AuthContext.jsx';

export default function AccountMenu() {
  const { ready, user, openAuth, logout } = useAuth();
  const [open, setOpen] = useState(false);
  const rootRef = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    function onDown(e) {
      if (rootRef.current && !rootRef.current.contains(e.target)) setOpen(false);
    }
    function onKey(e) {
      if (e.key === 'Escape') setOpen(false);
    }
    document.addEventListener('mousedown', onDown);
    window.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onDown);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  // Hold the slot during the initial /me probe so logged-in users do not see
  // the logged-out buttons flash first.
  if (!ready) return <div className="auth-controls" aria-hidden="true" />;

  if (!user) {
    return (
      <div className="auth-controls">
        <button onClick={() => openAuth('login')}>Log in</button>
        <button className="primary" onClick={() => openAuth('register')}>
          Sign up
        </button>
      </div>
    );
  }

  return (
    <div className="auth-controls auth-account" ref={rootRef}>
      <button className="auth-user-btn" onClick={() => setOpen((v) => !v)} aria-expanded={open}>
        <UserRound size={15} />
        <span className="auth-username">{user.username}</span>
        <ChevronDown size={14} />
      </button>
      {open ? (
        <div className="auth-dropdown" role="menu">
          <button onClick={() => { setOpen(false); openAuth('password'); }} role="menuitem">
            <KeyRound size={14} /> Change password
          </button>
          {user.role === 'admin' ? (
            <a href="/admin" role="menuitem" onClick={() => setOpen(false)}>
              <ShieldCheck size={14} /> Admin panel
            </a>
          ) : null}
          <button onClick={() => { setOpen(false); logout(); }} role="menuitem">
            <LogOut size={14} /> Log out
          </button>
        </div>
      ) : null}
    </div>
  );
}
