import { AnimatePresence } from 'framer-motion';
import { useAuth } from './AuthContext.jsx';
import AuthModal from '../components/AuthModal.jsx';
import AdminPage from '../components/AdminPage.jsx';
import App from '../App.jsx';

export default function AuthRoot() {
  const { modal, closeAuth } = useAuth();
  const isAdmin = window.location.pathname.startsWith('/admin');

  return (
    <>
      {isAdmin ? <AdminPage /> : <App />}
      <AnimatePresence>
        {modal ? (
          <AuthModal
            key="auth-modal"
            mode={modal.mode}
            message={modal.message}
            onClose={closeAuth}
          />
        ) : null}
      </AnimatePresence>
    </>
  );
}
