import { AnimatePresence, motion } from 'framer-motion';
import { spring } from '../motion.js';

// Fixed, top-right, auto-dismissing error toast. Auto-dismiss is managed by the
// parent (it owns the message lifetime); this just renders + offers manual close.
export default function Toast({ message, onDismiss }) {
  return (
    <div className="toast-wrap" aria-live="polite">
      <AnimatePresence>
        {message ? (
          <motion.div
            className="toast"
            role="alert"
            initial={{ opacity: 0, x: 28, y: -6, scale: 0.96 }}
            animate={{ opacity: 1, x: 0, y: 0, scale: 1 }}
            exit={{ opacity: 0, x: 28, scale: 0.96 }}
            transition={spring}
          >
            <span className="toast-icon" aria-hidden="true">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6"
                strokeLinecap="round" strokeLinejoin="round">
                <circle cx="12" cy="12" r="9" />
                <path d="M12 8v5M12 16.5v.01" />
              </svg>
            </span>
            <span className="toast-msg">{message}</span>
            <button className="link toast-close" onClick={onDismiss} aria-label="Dismiss">
              ✕
            </button>
          </motion.div>
        ) : null}
      </AnimatePresence>
    </div>
  );
}
