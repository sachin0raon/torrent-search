import { useState } from 'react';
import { Check, Copy, RefreshCw } from 'lucide-react';

// Copies a magnet link to the clipboard and shows a transient "Copied" state.
// Supports sync `value` string or async `getValue` resolver function.
export default function CopyButton({
  value,
  getValue,
  label = 'Copy magnet',
  className = 'btn-solid',
}) {
  const [copied, setCopied] = useState(false);
  const [loading, setLoading] = useState(false);

  async function performCopy(textToCopy) {
    if (!textToCopy) return false;
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(textToCopy);
      } else {
        const ta = document.createElement('textarea');
        ta.value = textToCopy;
        ta.style.cssText = 'position:fixed;top:0;left:0;opacity:0;pointer-events:none';
        document.body.appendChild(ta);
        ta.focus();
        ta.select();
        document.execCommand('copy');
        document.body.removeChild(ta);
      }
      return true;
    } catch {
      return false;
    }
  }

  async function onCopy() {
    if (loading || copied) return;
    let textToCopy = value;
    if (!textToCopy && getValue) {
      setLoading(true);
      try {
        textToCopy = await getValue();
      } catch {
        textToCopy = null;
      } finally {
        setLoading(false);
      }
    }

    if (textToCopy) {
      const ok = await performCopy(textToCopy);
      if (ok) {
        setCopied(true);
        setTimeout(() => setCopied(false), 1500);
      }
    }
  }

  const isBtnDisabled = loading || (!value && !getValue);

  return (
    <button className={className} onClick={onCopy} disabled={isBtnDisabled}>
      {loading ? (
        <RefreshCw size={14} style={{ animation: 'spin 1s linear infinite' }} />
      ) : copied ? (
        <Check size={14} />
      ) : (
        <Copy size={14} />
      )}
      <span className="btn-label">{loading ? 'Fetching…' : copied ? 'Copied' : label}</span>
    </button>
  );
}
