import { useEffect, useState } from 'react';
import { useAuth } from './AuthContext.jsx';
import { authApi } from '../api/auth.js';

// Returns a signed player-URL token for one stream session or download hash.
// When no AuthProvider is present (component tests), required is false and no
// request is made: links render without a token, matching the old behavior.
export function useStreamToken(kind, id) {
  const auth = useAuth();
  const required = Boolean(auth.available && auth.user && id);
  const [attempt, setAttempt] = useState(0);
  const [state, setState] = useState({ token: null, loading: false, error: null });

  useEffect(() => {
    if (!required || !id) {
      setState({ token: null, loading: false, error: null });
      return undefined;
    }
    let active = true;
    setState({ token: null, loading: true, error: null });
    authApi
      .streamToken(kind, id)
      .then((data) => {
        if (active) setState({ token: data.token, loading: false, error: null });
      })
      .catch((err) => {
        if (active) {
          setState({ token: null, loading: false, error: err.message || 'Could not prepare link' });
        }
      });
    return () => {
      active = false;
    };
  }, [required, kind, id, attempt]);

  return { ...state, required, reload: () => setAttempt((a) => a + 1) };
}
