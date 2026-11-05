import { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { authApi } from '../api/auth.js';
import { setCsrfToken } from './csrf.js';

const noop = () => {};
const AuthContext = createContext({
  available: false,
  ready: true,
  user: null,
  modal: null,
  openAuth: noop,
  closeAuth: noop,
});

export function AuthProvider({ children }) {
  const [user, setUser] = useState(null);
  const [ready, setReady] = useState(false);
  const [modal, setModal] = useState(null); // { mode, message }

  useEffect(() => {
    let active = true;
    authApi
      .me()
      .then((data) => {
        if (!active) return;
        setUser(data.user);
        setCsrfToken(data.csrf_token);
      })
      .catch(() => {})
      .finally(() => active && setReady(true));
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    function onUnauthorized() {
      setUser(null);
      setCsrfToken('');
      setModal({
        mode: 'login',
        message: 'Your session expired. Please log in again.',
      });
    }
    window.addEventListener('auth:unauthorized', onUnauthorized);
    return () => window.removeEventListener('auth:unauthorized', onUnauthorized);
  }, []);

  const value = useMemo(
    () => ({
      available: true,
      ready,
      user,
      modal,
      openAuth: (mode = 'login', message = '') => setModal({ mode, message }),
      closeAuth: () => setModal(null),
      async login(username, password) {
        const data = await authApi.login(username, password);
        setUser(data.user);
        setCsrfToken(data.csrf_token);
        setModal(null);
        return data;
      },
      async register(username, password) {
        return authApi.register(username, password);
      },
      async logout() {
        try {
          await authApi.logout();
        } finally {
          setUser(null);
          setCsrfToken('');
          setModal(null);
        }
      },
      async changePassword(currentPassword, newPassword) {
        const data = await authApi.changePassword(currentPassword, newPassword);
        setUser(data.user);
        setCsrfToken(data.csrf_token);
        return data;
      },
    }),
    [ready, user, modal],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return useContext(AuthContext);
}
