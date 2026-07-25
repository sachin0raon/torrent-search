import { useEffect, useState } from 'react';
import { AnimatePresence, motion } from 'framer-motion';

const SHOW_AFTER_PX = 400;

// Floating bottom-right button, appears once the page has scrolled past a
// threshold; scrolls back to top on click.
export default function ScrollToTopButton() {
  const [visible, setVisible] = useState(false);

  useEffect(() => {
    function onScroll() {
      setVisible(window.scrollY > SHOW_AFTER_PX);
    }
    onScroll();
    window.addEventListener('scroll', onScroll, { passive: true });
    return () => window.removeEventListener('scroll', onScroll);
  }, []);

  function scrollToTop() {
    // matchMedia isn't universally available (and isn't polyfilled in jsdom
    // tests) — feature-detect rather than assume it exists.
    const reduceMotion =
      typeof window.matchMedia === 'function' &&
      window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    window.scrollTo({ top: 0, behavior: reduceMotion ? 'auto' : 'smooth' });
  }

  return (
    <AnimatePresence>
      {visible ? (
        <motion.button
          className="scroll-top-btn"
          onClick={scrollToTop}
          aria-label="Scroll to top"
          title="Scroll to top"
          initial={{ opacity: 0, scale: 0.8, y: 10 }}
          animate={{ opacity: 1, scale: 1, y: 0 }}
          exit={{ opacity: 0, scale: 0.8, y: 10 }}
          transition={{ duration: 0.2 }}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"
            strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 19V5M5 12l7-7 7 7" />
          </svg>
        </motion.button>
      ) : null}
    </AnimatePresence>
  );
}
